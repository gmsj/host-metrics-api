# host-metrics-api

[![CI](https://github.com/gmsj/host-metrics-api/actions/workflows/ci.yml/badge.svg)](https://github.com/gmsj/host-metrics-api/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/gmsj/host-metrics-api)](https://github.com/gmsj/host-metrics-api/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> **English:** a small, dependency-free agent that exposes host metrics (CPU, memory, disk, network, NVIDIA GPU) as flat JSON over HTTP, with identical output on Linux and Windows. Single static binary, one endpoint per consumer need, no collection on the request path. Documentation is in Brazilian Portuguese; the code, comments and logs are in English. Binaries are on the [Releases](https://github.com/gmsj/host-metrics-api/releases) page.

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

Objeto **plano**: só valores escalares, sem objeto aninhado, sem array. Resposta real de um desktop Linux com uma GPU NVIDIA:

```json
{
  "ts": 1789656559,
  "agent_version": "0.1.0",
  "host": "desktop",
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
- **Os campos `gpu_*` podem estar até um intervalo atrás de `ts`.** O `nvidia-smi` roda num loop próprio e o snapshot leva a última leitura pronta. Se essa leitura ficar com mais de 3 intervalos de idade (`nvidia-smi` travado ou lentíssimo), os números vêm `null` e `gpu_present` mantém o último estado conhecido.

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
| `gpu_present` | `false` quando `nvidia-smi` não existe ou falha 5 vezes seguidas. Quando `false`, todos os `gpu_*` são `null`. `true` com todos os `gpu_*` em `null` significa que a placa existe mas a leitura falhou ou está velha demais | estado | ✅ | ✅ |
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

Binários prontos para Linux e Windows (amd64) estão na página de [Releases](https://github.com/gmsj/host-metrics-api/releases), com `checksums.txt`. Eles são gerados pelo GoReleaser a cada tag `v*` (veja [.goreleaser.yaml](.goreleaser.yaml) e o workflow de release).

Para compilar localmente:

```bash
make build-linux     # dist/hostmetrics-linux-amd64
make build-windows   # dist/hostmetrics-windows-amd64.exe
make build-all       # os dois
```

Os dois alvos rodam a partir de uma máquina Linux, sem toolchain extra: `CGO_ENABLED=0` mais `GOOS`/`GOARCH` bastam. O binário é estático, com cerca de 7 MB. A versão é injetada via `-ldflags "-X main.version=..."` a partir de `git describe --tags --always --dirty`, sem o `v` inicial, para bater com o que a release reporta; sobrescreva com `make build VERSION=0.1.0`.

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
- **`gpu_*` não é do mesmo instante que o resto.** A leitura de GPU acontece em loop próprio, com o mesmo intervalo, e o snapshot usa a mais recente disponível. Em condições normais a diferença é menor que um intervalo; num `nvidia-smi` lento (comum no Windows quando o driver acorda a placa) o sistema continua atualizado e só a GPU envelhece.

## Segurança

O agente faz bind em `0.0.0.0` sem autenticação. É intencional para rede doméstica confiável, mas significa que **qualquer dispositivo da rede local vê hostname, uso de recursos e modelo de GPU desta máquina**. Mantenha a porta 9900 fechada no firewall de borda (roteador) e, se quiser restringir dentro da rede, limite a regra de firewall local à sub-rede do consumidor. O payload não inclui lista de processos, caminhos de arquivo, usuários ou endereços IP. Detalhes e como reportar uma vulnerabilidade em [SECURITY.md](SECURITY.md).

## Arquitetura

```
host-metrics-api/
├── .github/workflows/          # ci.yml (testes, vet, lint, cross-build, govulncheck) e release.yml
├── .goreleaser.yaml            # binários, checksums e changelog da release
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
├── CHANGELOG.md · CONTRIBUTING.md · SECURITY.md · LICENSE (MIT)
├── Makefile
└── .golangci.yml
```

O desenho central: **nenhuma coleta acontece no handler HTTP**. Uma goroutine com `time.Ticker` monta um `Snapshot` completo a cada intervalo e publica via `atomic.Pointer`. O handler apenas serializa o snapshot atual (cerca de 0,3 ms medidos). Motivos:

1. `cpu.Percent(0)` mede o delta desde a chamada anterior, e essa baseline é estado global dentro do gopsutil. Dois clientes coletando sob demanda roubariam a baseline um do outro.
2. Contadores de rede e disco são cumulativos; taxa exige guardar a leitura anterior. Esse estado tem um único dono.
3. `nvidia-smi` é um spawn de processo de 100 a 300 ms (segundos, às vezes, no Windows) que não pode estar no caminho da request nem no caminho do tick. Ele roda numa segunda goroutine com ticker próprio e publica a última leitura noutro `atomic.Pointer`; o tick de sistema só lê o que estiver lá. Um `nvidia-smi` lento ou travado não atrasa CPU, RAM e rede, e não faz o `/healthz` acusar `stale` num agente saudável.

Falha de um coletor individual vira `null` naquele campo e um `warn` no log (uma vez por episódio, com uma linha `info` na recuperação); os outros campos seguem. Falha causada pelo próprio encerramento (Ctrl+C ou SIGTERM também chegam ao `nvidia-smi` filho) não é logada.

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

Os testes não tocam hardware: o parser do `nvidia-smi`, a escolha do sensor de CPU, o filtro de discos físicos e a escolha da interface de rede são funções puras alimentadas com dados de máquinas reais; o sampler recebe coletores falsos; os handlers usam `httptest`. Testar o gopsutil em si seria testar a biblioteca dos outros. O CI roda tudo isso no Ubuntu e a suíte também no Windows, onde os arquivos com `//go:build windows` compilam de verdade.

Regras para contribuir em [CONTRIBUTING.md](CONTRIBUTING.md).
