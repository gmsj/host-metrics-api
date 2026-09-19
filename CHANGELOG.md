# Changelog

Mudanças relevantes deste projeto, por versão. O formato segue o
[Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) e as versões seguem
[SemVer](https://semver.org/lang/pt-BR/).

## [Unreleased]

## [0.1.0] - 2026-09-18

Primeira versão pública.

### Added

- Endpoints `GET /stats`, `GET /stats/cores` e `GET /healthz` com o contrato
  JSON plano documentado no README.
- Coleta de CPU, memória, swap, disco, rede, uptime e temperatura de CPU
  (Linux) via gopsutil.
- Coleta de GPU NVIDIA via `nvidia-smi`, com detecção de presença, backoff e
  loop de leitura independente do tick de sistema.
- Binários estáticos para Linux e Windows (amd64), unit do systemd e guia de
  instalação para os dois sistemas.
- CI (testes, vet, lint, cross-build, govulncheck) e release automática via
  GoReleaser a cada tag `v*`.

[Unreleased]: https://github.com/gmsj/host-metrics-api/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/gmsj/host-metrics-api/releases/tag/v0.1.0
