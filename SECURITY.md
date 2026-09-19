# Segurança

## Modelo de exposição

O agente faz bind em `0.0.0.0` **sem autenticação** e responde a qualquer
origem (`Access-Control-Allow-Origin: *`). Isso é intencional para uma rede
doméstica confiável e significa que qualquer dispositivo com acesso à porta
(9900 por padrão) vê hostname, uso de recursos e modelo de GPU da máquina.

O payload não inclui lista de processos, caminhos de arquivo, usuários,
endereços IP nem qualquer dado que permita agir sobre a máquina. Todos os
endpoints são somente leitura; não existe rota que altere estado.

Recomendações:

- Nunca exponha a porta no roteador ou em IP público.
- Restrinja o firewall local à sub-rede do consumidor (o guia de instalação
  mostra como no `ufw` e no Firewall do Windows).
- Se só um consumidor local precisa ler, use `--bind 127.0.0.1`.

## Reportando uma vulnerabilidade

Abra um [security advisory privado](https://github.com/gmsj/host-metrics-api/security/advisories/new)
no GitHub em vez de uma issue pública. Descreva o impacto e como reproduzir.
Vulnerabilidades em dependências são verificadas a cada push pelo
`govulncheck` no CI.
