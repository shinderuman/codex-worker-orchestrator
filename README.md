# codex-worker-orchestrator

Codexをsemantic judgmentとorchestrationへ集中させ、repository調査・実装・test・reviewを`glm-worker`経由でGLMへ委譲するためのrepository-owned harnessです。installerはruntime binaryとCodex / Claude向けmanaged configurationを配置します。

## Prerequisites

- Git、Go、curl、tar、rsyncなどinstallerが使用するUnix tool
- Codex
- Claude Code（`glm-worker`のruntimeで使用）

repository-owned quality toolのcurrent versionは[`quality-tools.yml`](quality-tools.yml)を正とします。provider credentialはこのrepositoryでは管理しません。

## Setup

```sh
git clone https://github.com/shinderuman/codex-worker-orchestrator.git
cd codex-worker-orchestrator
./install-quality-tools.sh
./install.sh
```

runtime binaryは既定で`$HOME/.local/bin`へ配置されます。別directoryを使う場合は`GLM_WORKER_BIN_DIR`を指定してください。

## Start

セットアップ後は、対象repositoryのrootでCodexを開始して通常の作業要求を渡します。このrepository自身で作業する場合は、rootの[`AGENTS.md`](AGENTS.md)がproject instructionのbootstrapです。

current project stateを確認する入口は次です。

```sh
glm-worker --project-state
```

CLIのcurrent command / usageは`glm-worker --help`を参照してください。

## Development rules

repositoryでの作業規則は[`AGENTS.md`](AGENTS.md)と[`IMPLEMENTATION_RULES.md`](IMPLEMENTATION_RULES.md)を参照してください。task schedule、individual task requirement、runtime state、schema、internal architectureはREADMEへ複製せず、それぞれのcanonical repository sourceとlive commandを正とします。

## License

[MIT License](LICENSE)
