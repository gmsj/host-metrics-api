# Guia de instalação e uso

Este guia é para **colocar o agente rodando na máquina monitorada**, nos dois boots. Para desenvolvimento (build, testes, lint) veja o [README](../README.md).

Os binários estão na página de [Releases](https://github.com/gmsj/host-metrics-api/releases/latest) do projeto:

| Sistema | Arquivo |
|---|---|
| Ubuntu | `hostmetrics-linux-amd64` |
| Windows | `hostmetrics-windows-amd64.exe` |

Cada release traz um `checksums.txt`; confira o download com `sha256sum -c checksums.txt --ignore-missing` no Linux ou `Get-FileHash` no PowerShell. Quem prefere compilar roda `make build-all` no Ubuntu e encontra os mesmos arquivos em `dist/`; os comandos abaixo assumem a pasta `dist/`, ajuste o caminho se baixou da release.

O binário é autocontido. Nada precisa ser instalado antes dele. Para a GPU, basta o driver NVIDIA, que já traz o `nvidia-smi`.

---

## Ubuntu

### 1. Teste rápido, sem instalar

Serve para confirmar que o binário sobe e que os valores fazem sentido. Não é assim que ele vai rodar no dia a dia.

```bash
./dist/hostmetrics-linux-amd64
```

Em outro terminal:

```bash
curl -s localhost:9900/stats | python3 -m json.tool
curl -s localhost:9900/healthz
```

`Ctrl+C` encerra. O log vai para o terminal e mostra qual disco e interface foram escolhidos.

### 2. Instalar

O jeito correto é o binário em `/usr/local/bin` e o agente como serviço do systemd, que sobe no boot e reinicia sozinho se cair.

Os comandos usam `sudo` porque escrevem em diretórios do sistema e registram um serviço do sistema. O agente em si **não** roda como root: a unit usa `DynamicUser=yes`, e o systemd cria um usuário sem privilégios a cada execução.

Na pasta do repositório (ou de onde estiverem o binário e a unit):

```bash
# 1. binário no PATH, com o nome curto
sudo install -m 755 dist/hostmetrics-linux-amd64 /usr/local/bin/hostmetrics

# 2. unit do systemd
sudo install -m 644 deploy/hostmetrics.service /etc/systemd/system/hostmetrics.service

# 3. recarregar units, habilitar no boot e subir agora
sudo systemctl daemon-reload
sudo systemctl enable --now hostmetrics
```

`install` é o `cp` com permissão definida na mesma operação: `755` deixa o binário executável, `644` deixa a unit legível.

Conferir:

```bash
systemctl status hostmetrics
journalctl -u hostmetrics -f        # log ao vivo
curl -s localhost:9900/healthz      # ok
```

Depois disso, o comando `hostmetrics` também existe no PATH para uso manual, mas o serviço já está com a porta ocupada. Para experimentar flags à mão, use outra porta: `hostmetrics --port 9901`.

### 3. Configurar

A configuração do serviço são as linhas `Environment=` da unit. Não edite o arquivo em `/etc/systemd/system` diretamente; crie um drop-in, que sobrevive a reinstalação:

```bash
sudo systemctl edit hostmetrics
```

No editor que abre, entre os comentários:

```ini
[Service]
Environment=HOSTMETRICS_PORT=9900
Environment=HOSTMETRICS_NET_IFACE=enp7s0
Environment=HOSTMETRICS_LOG_LEVEL=debug
```

Salve e reinicie: `sudo systemctl restart hostmetrics`. As variáveis disponíveis estão na tabela de configuração do README.

### 4. Firewall

Se o `ufw` estiver ativo, libere a porta só para a rede local:

```bash
sudo ufw allow from 192.168.0.0/16 to any port 9900 proto tcp comment hostmetrics
```

Ajuste a faixa para a sua sub-rede. Nunca exponha a porta no roteador.

### 5. Atualizar

```bash
sudo install -m 755 dist/hostmetrics-linux-amd64 /usr/local/bin/hostmetrics
sudo systemctl restart hostmetrics
curl -s localhost:9900/stats | grep agent_version
```

Se a unit em `deploy/` também mudou, copie de novo e rode `sudo systemctl daemon-reload` antes do `restart`.

### 6. Desinstalar

```bash
sudo systemctl disable --now hostmetrics
sudo rm -f /etc/systemd/system/hostmetrics.service /usr/local/bin/hostmetrics
sudo rm -rf /etc/systemd/system/hostmetrics.service.d   # drop-in de configuração, se existir
sudo systemctl daemon-reload
```

Remova também a regra do firewall, se criou: `sudo ufw delete allow from 192.168.0.0/16 to any port 9900 proto tcp`.

---

## Windows

### 1. Teste rápido, sem instalar

Copie `hostmetrics-windows-amd64.exe` para qualquer pasta e **dê dois cliques**. Abre uma janela de console com o log; enquanto ela estiver aberta, o agente está rodando. Fechar a janela encerra o agente.

O SmartScreen pode avisar que o executável não é assinado. "Mais informações" e "Executar assim mesmo" resolve; é o custo de não pagar por um certificado de assinatura.

Teste no navegador: `http://localhost:9900/stats`. Ou no PowerShell:

```powershell
Invoke-RestMethod http://localhost:9900/stats
```

Para testar flags, rode pelo PowerShell na pasta do exe:

```powershell
.\hostmetrics-windows-amd64.exe --port 9901 --log-level debug
```

### 2. Instalar

O jeito correto é uma **tarefa agendada** que sobe no boot, como SYSTEM, sem janela e sem precisar de usuário logado. O Windows não aceita este exe direto como serviço (`sc create`): o Gerenciador de Serviços exige que o processo fale o protocolo dele, e este é um programa de console comum.

PowerShell **como administrador**, a partir da pasta do repositório ou de onde estiver o exe:

```powershell
# 1. binário em Program Files, com o nome curto
New-Item -ItemType Directory -Force "C:\Program Files\hostmetrics" | Out-Null
Copy-Item .\dist\hostmetrics-windows-amd64.exe "C:\Program Files\hostmetrics\hostmetrics.exe" -Force

# 2. tarefa agendada: no boot, como SYSTEM, sem limite de tempo, reinicia se cair
$action   = New-ScheduledTaskAction -Execute "C:\Program Files\hostmetrics\hostmetrics.exe"
$trigger  = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit 0 `
              -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) `
              -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName hostmetrics -Action $action -Trigger $trigger `
  -Settings $settings -User "NT AUTHORITY\SYSTEM" -RunLevel Highest -Force

# 3. subir agora, sem esperar o próximo boot
Start-ScheduledTask -TaskName hostmetrics
```

Conferir:

```powershell
Get-ScheduledTask hostmetrics | Select-Object State
Invoke-RestMethod http://localhost:9900/healthz
```

A tarefa também aparece no Agendador de Tarefas (`taskschd.msc`), na biblioteca raiz, com o nome `hostmetrics`.

### 3. Configurar

Duas formas. Por flags, na própria tarefa:

```powershell
$action = New-ScheduledTaskAction -Execute "C:\Program Files\hostmetrics\hostmetrics.exe" `
            -Argument "--port 9900 --net-iface Ethernet --log-level debug"
Set-ScheduledTask -TaskName hostmetrics -Action $action
Stop-ScheduledTask -TaskName hostmetrics; Start-ScheduledTask -TaskName hostmetrics
```

Ou por variáveis de ambiente **do sistema** (as de usuário não valem para uma tarefa rodando como SYSTEM):

```powershell
[Environment]::SetEnvironmentVariable("HOSTMETRICS_PORT", "9900", "Machine")
```

Os nomes de interface no Windows são os do painel de rede: `Ethernet`, `Wi-Fi`. `Get-NetAdapter` lista.

### 4. Log

Rodando como tarefa não há console, então o log some. Para ver o que o agente está dizendo, pare a tarefa e rode o exe à mão no PowerShell (passo 1). Se precisar de log persistente, redirecione na tarefa usando `cmd` como executável:

```powershell
$action = New-ScheduledTaskAction -Execute "cmd.exe" `
  -Argument '/c ""C:\Program Files\hostmetrics\hostmetrics.exe" >> "C:\ProgramData\hostmetrics\hostmetrics.log" 2>&1"'
```

### 5. Firewall

O Firewall do Windows bloqueia conexões de entrada por padrão. Libere a porta só no perfil de rede privada:

```powershell
New-NetFirewallRule -DisplayName hostmetrics -Direction Inbound -Protocol TCP `
  -LocalPort 9900 -Action Allow -Profile Private
```

Se a rede da casa estiver marcada como "Pública" no Windows, a regra não vale; mude o perfil da rede ou inclua `Public` no `-Profile`.

### 6. Atualizar

```powershell
Stop-ScheduledTask -TaskName hostmetrics
Copy-Item .\dist\hostmetrics-windows-amd64.exe "C:\Program Files\hostmetrics\hostmetrics.exe" -Force
Start-ScheduledTask -TaskName hostmetrics
```

### 7. Desinstalar

```powershell
Stop-ScheduledTask -TaskName hostmetrics
Unregister-ScheduledTask -TaskName hostmetrics -Confirm:$false
Remove-NetFirewallRule -DisplayName hostmetrics
Remove-Item -Recurse "C:\Program Files\hostmetrics"
```

### Alternativa: serviço de verdade com NSSM

Se você prefere ver o agente em `services.msc`, um wrapper como o [NSSM](https://nssm.cc/) faz a ponte com o Gerenciador de Serviços:

```powershell
nssm install hostmetrics "C:\Program Files\hostmetrics\hostmetrics.exe"
nssm set hostmetrics AppEnvironmentExtra HOSTMETRICS_PORT=9900
nssm set hostmetrics AppStdout C:\ProgramData\hostmetrics\hostmetrics.log
nssm set hostmetrics AppStderr C:\ProgramData\hostmetrics\hostmetrics.log
nssm start hostmetrics
```

Serviço nativo, sem wrapper, é possível com a biblioteca `golang.org/x/sys/windows/svc`, que já é dependência transitiva do projeto. Ficou fora da v0.1 por escopo.

---

## Depois de instalado nos dois boots

Do consumidor, o teste é o mesmo independente de qual sistema está de pé:

```bash
curl -s http://IP-DA-MAQUINA:9900/stats
```

O campo `os` diz em qual boot a máquina está. Connection refused ou timeout significa máquina desligada, reiniciando, ou trocando de boot: isso é informação, não erro do agente.
