# host-metrics-api

Agente local que expõe métricas da máquina (CPU, memória, disco, rede, GPU NVIDIA) via HTTP em JSON.

Roda na própria máquina monitorada. Qualquer consumidor na rede local (dispositivo embarcado, dashboard, script) faz `GET /stats` e recebe um snapshot pronto para exibição: sem cálculo, sem média, sem conversão de unidade do lado de quem lê. O mesmo código gera binários para **Linux** e **Windows** que expõem exatamente o mesmo contrato.

| Item | Valor |
|---|---|
| Módulo Go | `github.com/gmsj/host-metrics-api` |
| Binário | `hostmetrics` |
| Porta padrão | `9900` |
| Variáveis de ambiente | prefixo `HOSTMETRICS_` |
| Dependência de runtime | nenhuma (binário estático) |

## Início rápido

```bash
make build            # binário para a plataforma atual
./hostmetrics         # sobe em 0.0.0.0:9900
curl -s localhost:9900/stats | python3 -m json.tool
```

Ou, sem build: `make run ARGS="--port 9901 --log-level debug"`. Isso é para desenvolvimento; para instalar de verdade veja [docs/instalacao.md](docs/instalacao.md).

## Contrato da API

### `GET /stats`

Objeto **plano**: só valores escalares, sem objeto aninhado, sem array. Resposta real desta máquina (Ubuntu, Ryzen 5 5500, RTX 3070):

```json
{
  "ts": 1789656559,
  "agent_version": "0.1.0",
  "host": "Mainuntu",
  "os": "linux",
  "uptime_s": 19127,
  "cpu_pct": 2.2,
  "cpu_freq_mhz": 4268.6,
  "cpu_temp_c": 37.9,
  "load1": 0.2,
  "load5": 1.1,
  "load15": 1,
  "ram_used_gb": 8.2,
  "ram_total_gb": 31.1,
  "ram_pct": 26.2,
  "swap_used_gb": 0,
  "swap_pct": 0,
  "disk_used_gb": 171.8,
  "disk_total_gb": 241,
  "disk_pct": 71.3,
  "disk_read_mb_s": 0,
  "disk_write_mb_s": 0.2,
  "net_rx_mbps": 0,
  "net_tx_mbps": 0,
  "gpu_present": true,
  "gpu_pct": 10,
  "gpu_temp_c": 50,
  "gpu_vram_used_mb": 610,
  "gpu_vram_total_mb": 8192,
  "gpu_vram_pct": 7.4,
  "gpu_power_w": 19.4,
  "gpu_fan_pct": 0,
  "gpu_clock_mhz": 210
}
```

Regras do contrato:

- **Toda chave está sempre presente.** Métrica indisponível na plataforma, ou que falhou neste tick, vem como `null`. O consumidor escreve um único tratamento, sem branch por sistema operacional.
- **Números vêm arredondados a uma casa decimal.** Go serializa `88.0` como `88`, sem ponto; um parser que decide o tipo pela presença do ponto deve tratar todos os campos numéricos como float.
- **Unidades:** capacidades em base binária (`_gb` = GiB, `_mb` = MiB), que é o que o Gerenciador de Tarefas, `free -h` e `nvidia-smi` mostram. Taxas em base decimal: rede em megabits por segundo (`_mbps`, 10⁶ bit/s), disco em megabytes por segundo (`_mb_s`, 10⁶ byte/s).
- **Antes do primeiro snapshot** (um intervalo após o start, 1 s por padrão) todos os endpoints respondem `503` com `{"error":"no snapshot yet"}`.

### `GET /stats/cores`

```json
{"ts":1789656559,"cores_pct":[9.9,5.1,2,1,2,2,2,2,1,1,1,1]}
```

Uso por core lógico, mesma janela de 1 s. É o único endpoint com array, separado de propósito para não poluir o payload principal. Se a leitura por core falhar, o array vem vazio (`[]`), nunca `null`.

### `GET /healthz`

`200 ok` quando o sampler produziu pelo menos um snapshot e o mais recente tem menos de 3 intervalos de idade. Senão `503` com corpo `no snapshot yet` ou `stale`. Se `/stats` continua respondendo mas `ts` parou de avançar, o sampler travou, e este endpoint é o que denuncia isso.

### Headers

Todas as respostas, incluindo erros, trazem `Access-Control-Allow-Origin: *` (um dashboard em browser em qualquer origem consegue ler) e `Cache-Control: no-store` (nada no caminho guarda um snapshot velho). Métodos diferentes de `GET` recebem `405`.

## Dicionário de campos

Cada métrica tem uma **natureza**. Ela define se faz sentido tirar média e explica por que dois campos podem discordar na tela sem nenhum estar errado.

| Chave | Descrição | Natureza | Windows | Linux |
|---|---|---|---|---|
| `ts` | Unix timestamp da coleta. Se parar de avançar, o sampler travou | pontual | ✅ | ✅ |
| `agent_version` | Versão do binário (tag git, ou SHA curto) | estático | ✅ | ✅ |
| `host` | Hostname. Muda entre os dois boots da mesma máquina | estático | ✅ | ✅ |
| `os` | `"windows"` ou `"linux"`: em qual boot a máquina está | estático | ✅ | ✅ |
| `uptime_s` | Segundos desde o boot | acumulado | ✅ | ✅ |
| `cpu_pct` | Uso agregado de CPU, 0 a 100 | **janela de 1 s** | ✅ | ✅ |
| `cpu_freq_mhz` | ⚠️ Clock **máximo (nominal)** do processador, nas duas plataformas. Não é o clock corrente. Lido uma vez no start | estático | ⚠️ | ⚠️ |
| `cpu_temp_c` | Temperatura do die/package via hwmon (`k10temp` na AMD, `coretemp` na Intel). `null` no Windows | pontual | ❌ | ✅ |
| `load1` / `load5` / `load15` | Média de carga de 1/5/15 min. ⚠️ **Já é média**: reage em escala de minuto e vai divergir de `cpu_pct`, ambos corretos. No Windows é uma emulação com warm-up (veja limitações) | média | ⚠️ | ✅ |
| `ram_used_gb` / `ram_total_gb` / `ram_pct` | Memória física em GiB. `used = total − available`, mesma fórmula nos dois sistemas. `total` é a memória que o kernel enxerga, um pouco menor que a instalada | pontual | ✅ | ✅ |
| `swap_used_gb` / `swap_pct` | Linux: swap. Windows: uso do page file (equivalente prático). Sem swap: `0` e `0` | pontual | ⚠️ | ✅ |
| `disk_used_gb` / `disk_total_gb` / `disk_pct` | Espaço do volume monitorado (`--disk`), GiB. `pct = used / total` | pontual | ✅ | ✅ |
| `disk_read_mb_s` / `disk_write_mb_s` | Throughput de I/O somado dos **discos físicos** (Linux) ou dos volumes lógicos (Windows), em MB/s | **janela de 1 s** | ✅ | ✅ |
| `net_rx_mbps` / `net_tx_mbps` | Throughput da interface monitorada (`--net-iface`), em Mbit/s | **janela de 1 s** | ✅ | ✅ |
| `gpu_present` | `false` quando `nvidia-smi` não existe ou falha repetidamente. Quando `false`, todos os `gpu_*` são `null` | estado | ✅ | ✅ |
| `gpu_pct` | Uso do núcleo gráfico. ⚠️ Pré-agregado numa janela interna do driver, não na nossa | janela do driver | ✅ | ✅ |
| `gpu_temp_c` | Temperatura do die da GPU. A única temperatura simétrica entre os dois sistemas | pontual | ✅ | ✅ |
| `gpu_vram_used_mb` / `_total_mb` / `_pct` | VRAM alocada, MiB | pontual | ✅ | ✅ |
| `gpu_power_w` | Consumo instantâneo em watts. Pode ser `null` em placas que não reportam | pontual | ✅ | ✅ |
| `gpu_fan_pct` | Rotação do fan em % do máximo. `0` com fan parado em idle; `null` se a placa não reporta | pontual | ✅ | ✅ |
| `gpu_clock_mhz` | Clock gráfico corrente | pontual | ✅ | ✅ |

Legenda: ✅ disponível e confiável · ⚠️ disponível com ressalva · ❌ sempre `null` nessa plataforma

## Configuração

Flag de linha de comando com equivalente em variável de ambiente. Precedência: flag > variável > padrão.

| Flag | Variável | Padrão | Descrição |
|---|---|---|---|
| `--port` | `HOSTMETRICS_PORT` | `9900` | Porta HTTP |
| `--bind` | `HOSTMETRICS_BIND` | `0.0.0.0` | Endereço de bind (IPv4). Use `::` para IPv4 e IPv6 |
| `--interval` | `HOSTMETRICS_INTERVAL` | `1s` | Período do sampler. Mínimo `500ms` |
| `--disk` | `HOSTMETRICS_DISK` | auto | Volume monitorado. Padrão: `/` no Linux, `%SystemDrive%\` no Windows |
| `--net-iface` | `HOSTMETRICS_NET_IFACE` | auto | Interface de rede. Padrão: a de maior tráfego acumulado no start, excluindo loopback. A escolha aparece no log |
| `--log-level` | `HOSTMETRICS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

A porta 9900 fica deliberadamente longe de 8085, usada por ferramentas de monitoramento de hardware que podem coexistir na máquina.

## Build

```bash
make build-linux     # dist/hostmetrics-linux-amd64
make build-windows   # dist/hostmetrics-windows-amd64.exe
make build-all       # os dois
```

Os dois alvos rodam a partir de uma máquina Linux, sem toolchain extra: `CGO_ENABLED=0` mais `GOOS`/`GOARCH` bastam. O binário é estático, com cerca de 7 MB. A versão é injetada via `-ldflags "-X main.version=..."` a partir de `git describe --tags --always --dirty`; sobrescreva com `make build VERSION=0.1.0`.

## Instalar e rodar na máquina monitorada

Passo a passo completo em [docs/instalacao.md](docs/instalacao.md): teste rápido, instalação permanente (systemd no Ubuntu, tarefa agendada no Windows), configuração, firewall, atualização e desinstalação.
## Limitações conhecidas

- **`cpu_temp_c` é sempre `null` no Windows.** A única fonte sem driver em ring-0 é a classe WMI `MSAcpi_ThermalZoneTemperature`, que exige elevação e, na maioria dos desktops modernos, devolve a temperatura de uma zona térmica ACPI da placa (ou nada), nunca a do core. Um número plausível e errado é pior que `null`, então não há heurística.
- **`load*` no Windows é emulação.** O kernel NT não tem load average. O gopsutil amostra o tamanho da fila de processador a cada 5 s e aplica média exponencial. Espere valores próximos de zero no primeiro minuto após o start (warm-up), e não compare com o Linux.
- **`cpu_freq_mhz` é o clock máximo, não o corrente**, nas duas plataformas. No Windows vem de `Win32_Processor.MaxClockSpeed`; no Linux o gopsutil v4 lê `cpuinfo_max_freq` de propósito para bater com o Windows. Para clock corrente seria preciso ler `scaling_cur_freq` do sysfs, o que reintroduziria a assimetria.
- **`swap_*` no Windows é uso do page file**, não swap no sentido Unix. É o equivalente prático e o número é real.
- **Um único volume e uma única interface.** `disk_*` cobre só o volume de `--disk`; `net_*` só a interface escolhida no start. Se a máquina troca de Wi‑Fi para cabo com o agente rodando, reinicie ou fixe `--net-iface`.
- **Uma única GPU.** Se `nvidia-smi` listar mais de uma, o agente usa a primeira e avisa uma vez no log.
- **Reset de contador.** Quando um contador cumulativo volta a zero (interface recriada, driver recarregado), a taxa daquele tick vem `null` em vez de um número negativo enorme. O tick seguinte já tem baseline nova.
- **Primeiro snapshot leva um intervalo.** O sampler faz uma leitura de aquecimento no start e só publica no primeiro tick. Até lá, `503`.

## Segurança

O agente faz bind em `0.0.0.0` sem autenticação. É intencional para rede doméstica confiável, mas significa que **qualquer dispositivo da rede local vê hostname, uso de recursos e modelo de GPU desta máquina**. Mantenha a porta 9900 fechada no firewall de borda (roteador) e, se quiser restringir dentro da rede, limite a regra de firewall local à sub-rede do consumidor. O payload não inclui lista de processos, caminhos de arquivo, usuários ou endereços IP.

## Arquitetura

```
host-metrics-api/
├── cmd/hostmetrics/main.go     # composition root: flags, wiring, sinais, shutdown
├── internal/
│   ├── config/                 # flags + env, validação
│   ├── model/stats.go          # structs do payload + tags JSON (o contrato)
│   ├── collector/
│   │   ├── collector.go        # interfaces System e GPU, tipos de amostra
│   │   ├── system.go           # gopsutil (comum às duas plataformas)
│   │   ├── disk_linux.go       # filtro de discos físicos; disk_windows.go, disk_other.go
│   │   ├── temp_linux.go       # hwmon; temp_other.go devolve nil
│   │   ├── gpu.go              # parser do nvidia-smi + máquina de estados de presença
│   │   └── gpu_windows.go      # exec sem janela de console; gpu_other.go
│   ├── sampler/                # ticker, deltas, unidades, snapshot atômico
│   └── httpapi/                # handlers, headers, /healthz
├── deploy/hostmetrics.service  # unit do systemd
├── docs/instalacao.md          # guia de instalação e uso (Ubuntu e Windows)
├── Makefile
└── .golangci.yml
```

O desenho central: **nenhuma coleta acontece no handler HTTP**. Uma goroutine com `time.Ticker` monta um `Snapshot` completo a cada intervalo e publica via `atomic.Pointer`. O handler apenas serializa o snapshot atual (cerca de 0,3 ms medidos). Motivos:

1. `cpu.Percent(0)` mede o delta desde a chamada anterior, e essa baseline é estado global dentro do gopsutil. Dois clientes coletando sob demanda roubariam a baseline um do outro.
2. Contadores de rede e disco são cumulativos; taxa exige guardar a leitura anterior. Esse estado tem um único dono.
3. `nvidia-smi` é um spawn de processo de 100 a 300 ms que não pode estar no caminho da request. O sampler o roda em paralelo com a coleta de sistema.

Falha de um coletor individual vira `null` naquele campo e um `warn` no log (uma vez, não a cada tick); os outros campos seguem.

## Desenvolvimento

```bash
make help     # lista os alvos
make test     # go test -race ./...
make cover    # relatório HTML em coverage.html
make lint     # go vet (Linux e Windows) + golangci-lint
make fmt      # gofumpt
make tidy     # go mod tidy
```

`make lint` roda `GOOS=windows go vet` porque os arquivos com `//go:build windows` nunca são compilados num `go build` feito no Linux; sem isso um erro neles só apareceria no `make build-windows`.

Os testes não tocam hardware: o parser do `nvidia-smi` é uma função pura, o sampler recebe coletores falsos, e os handlers usam `httptest`. Testar o gopsutil em si seria testar a biblioteca dos outros.

## Guia de Go para quem nunca usou

Para quem vem de Python/AWS/dados. Assume experiência sólida em programação, nenhuma em Go.

### Instalação

**Não instale pelo `apt`**: o repositório do Ubuntu fica várias releases atrás. Use o tarball oficial:

```bash
# versão estável atual em https://go.dev/dl/
curl -LO https://go.dev/dl/goX.Y.Z.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf goX.Y.Z.linux-amd64.tar.gz

echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.bashrc   # binários de `go install` (golangci-lint, gofumpt)
source ~/.bashrc
go version
```

No Windows, o instalador `.msi` do site oficial configura o PATH. Este projeto exige Go 1.24 ou mais novo (é o mínimo do gopsutil v4).

Ferramentas de desenvolvimento, opcionais mas usadas pelo Makefile:

```bash
go install mvdan.cc/gofumpt@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

### Comandos do dia a dia

| Comando | O que faz | Análogo em Python |
|---|---|---|
| `go mod init github.com/gmsj/host-metrics-api` | Cria o módulo e o `go.mod` | `poetry init` |
| `go get github.com/shirou/gopsutil/v4` | Adiciona dependência | `pip install` + lockfile |
| `go mod tidy` | Adiciona o que falta, remove o que sobra | limpeza do lockfile |
| `go build ./...` | Compila tudo | — |
| `go run ./cmd/hostmetrics` | Compila e executa | `python main.py` |
| `go test ./...` | Roda todos os testes | `pytest` |
| `go test -race ./...` | Testes com detector de data race | sem equivalente |
| `go fmt ./...` | Formata | `black` |
| `go vet ./...` | Análise estática básica | parte de `ruff`/`mypy` |
| `go doc net/http.Server` | Doc de um símbolo no terminal | `help()` |

### Conceitos que pegam quem vem de Python, com exemplos deste projeto

**Não há `venv`.** O `go.mod` declara o módulo e as dependências; o `go.sum` é o lockfile com hashes. O código das dependências fica num cache global versionado (`~/go/pkg/mod`), compartilhado por todos os projetos. Nada é "ativado".

**`go build` produz um binário estático.** Não há runtime para instalar na máquina de destino. `dist/hostmetrics-linux-amd64` roda em qualquer Linux x86-64.

**Cross-compile é nativo.** `GOOS` e `GOARCH` são variáveis de ambiente. Funciona enquanto `CGO_ENABLED=0`, ou seja, enquanto nenhuma dependência chama C. É por isso que bindings NVML para a GPU estão fora: eles exigem cgo e destruiriam isso.

**Erros são valores, não exceções.** Uma função que pode falhar devolve o erro como último retorno, e o chamador decide na hora. O padrão abaixo aparece dezenas de vezes e é intencional:

```go
usage, err := disk.UsageWithContext(ctx, c.diskPath)
if err != nil {
    c.faults.fail("disk_usage", err)   // loga e deixa o campo nil
} else {
    s.DiskTotalB, s.DiskUsedB = ptr(usage.Total), ptr(usage.Used)
}
```

Não há `try/except` que pega tudo lá em cima; cada erro é tratado onde acontece. `errors.Is` e `errors.As` fazem o papel de `isinstance` em exceções (veja `runNvidiaSMI` em `gpu.go`).

**`defer` é o context manager.** Empilha uma chamada para rodar quando a função retornar, aconteça o que acontecer:

```go
ctx, cancel := context.WithTimeout(ctx, nvidiaSMITimeout)
defer cancel()   // roda ao sair da função, como o __exit__ do `with`
```

**Ponteiros são só "pode ser nulo" aqui.** Em `model.Stats`, `*float64` significa "float ou `null` no JSON". `encoding/json` serializa ponteiro nil como `null` e o valor apontado caso contrário. O helper `ptr(v)` faz o que em Python seria trivial: pega o endereço de um valor.

**Maiúscula exporta.** `Stats` é visível fora do pacote, `faultTracker` não. Não é convenção como `_private`; é regra da linguagem. `internal/` é outra regra: pacotes ali só podem ser importados de dentro deste módulo.

**Interfaces são implícitas.** `collector.System` é uma interface com um método `Collect`. `SystemCollector` a satisfaz sem declarar nada, e o `fakeSystem` dos testes também. É duck typing verificado em compilação. O sampler recebe a interface, então é testável sem hardware.

**Goroutines e `context`.** Uma goroutine é uma função rodando concorrentemente (`go smp.Run(ctx)`). Este projeto usa poucas: uma para o sampler, uma para o servidor HTTP, e duas curtas por tick para coletar sistema e GPU em paralelo (`sync.WaitGroup` espera as duas). `context.Context` é o mecanismo de cancelamento: em `main.go`, `signal.NotifyContext` cancela o contexto quando chega SIGINT/SIGTERM, e tudo que recebeu esse `ctx` (o loop do sampler, o timeout do `nvidia-smi`) encerra.

**`atomic.Pointer[T]` é o padrão central.** O sampler monta um `*model.Snapshot` novo a cada tick e faz `Store`; cada handler HTTP faz `Load`. Funciona sem mutex porque o snapshot **nunca é modificado depois de publicado**: quem lê tem um ponteiro para um valor imutável, e o próximo tick cria outro valor em vez de mexer neste. Leitura lock-free para qualquer número de clientes, escrita por um único dono. Compare com Python, onde você usaria um `threading.Lock` em volta de um dict compartilhado.

**Build tags** escolhem arquivos por plataforma em compilação. `//go:build windows` na primeira linha de `gpu_windows.go` faz esse arquivo existir só no build Windows; `gpu_other.go` tem `//go:build !windows`. Os dois definem a mesma função `nvidiaSMICommand`, e o compilador vê exatamente uma. É o único lugar onde há código específico de plataforma além do stub de temperatura.

**`ServeMux` com método na rota.** Desde Go 1.22, `mux.HandleFunc("GET /stats", ...)` registra método e caminho juntos; outros métodos recebem `405` do próprio roteador, sem middleware.
