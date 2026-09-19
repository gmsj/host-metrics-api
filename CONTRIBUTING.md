# Contribuindo

Issues e pull requests são bem-vindos, em português ou inglês.

## Antes de abrir um PR

```bash
make test    # go test -race ./...
make lint    # go vet (Linux e Windows) + golangci-lint
make fmt     # gofumpt
```

O CI roda exatamente isso, mais o cross-build dos dois binários e o
`govulncheck`. Um PR que não passa localmente não vai passar lá.

## Regras do projeto

- **O contrato JSON é a parte estável.** Adicionar um campo é uma mudança
  minor; renomear ou remover é breaking e precisa de discussão em issue antes.
  O teste `TestStatsShapeAndHeaders` lista as chaves esperadas e o README
  documenta cada uma: os dois mudam junto com o código.
- **Sem cgo.** O cross-compile depende de `CGO_ENABLED=0`. Dependências que
  chamam C (NVML, por exemplo) não entram.
- **Coletores não retornam erro.** Métrica que falhou vira `nil` no sample e
  uma linha de log na primeira falha; veja o comentário do pacote `collector`.
- **Testes não tocam hardware.** Lógica de seleção e parsing fica em funções
  puras testáveis; a leitura do sistema fica numa casca fina sem teste.

## Commits

[Conventional Commits](https://www.conventionalcommits.org/pt-br/): `feat:`,
`fix:`, `docs:`, `ci:`, `test:`, `chore:`. O changelog da release é gerado a
partir deles, então o assunto do commit é o que o usuário vai ler.
