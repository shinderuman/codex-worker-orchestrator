# codex-worker-orchestrator

Codexが要求・設計・最終採否を判断し、GLMがリポジトリ調査・実装・テスト・独立レビューを担当するためのツールです。`glm-worker`が実行状態と検証証拠を管理します。

## Prerequisites

- Git、Go、curl、tar、rsyncなどinstallerが使用するUnix tool
- Codex
- Claude Code（`glm-worker`のruntimeで使用）
- Z.aiのAPI認証情報（Claude Codeから利用できるよう設定）

repository-owned quality toolのcurrent versionは[`quality-tools.yml`](quality-tools.yml)を正とします。provider credentialはこのrepositoryでは管理しません。

## Setup

```sh
git clone https://github.com/shinderuman/codex-worker-orchestrator.git
cd codex-worker-orchestrator
./install-quality-tools.sh
./install.sh
```

CLIは既定で`$HOME/.local/bin`へ配置されます。シェルの`PATH`にこのディレクトリを追加してください。インストールはCodex・Claude Codeの管理設定と、このリポジトリのGit hooksも配置します。

認証方法、配置先の変更、撤去時の注意は[セットアップの補足](docs/setup.md)を参照してください。

## Uninstall

```sh
./uninstall.sh
```

本ツールが管理するCLI・設定・hooksを撤去します。ユーザーの設定や認証情報、実行記録、別途導入したquality toolsは保持します。配置先を変更した場合は、インストール時と同じ指定で実行してください。

## Start

セットアップ後は、作業対象リポジトリのrootでCodexを開始し、通常どおり作業を依頼します。要求の整理やGLMへの委譲はCodexが行います。

このリポジトリ自身で作業する場合は[`AGENTS.md`](AGENTS.md)を入口に、親Codexが依頼を専用Taskへ登録します。前のTaskがすべて完了した状態でも新しい依頼を開始でき、登録だけのコミットは不要です。

current project stateを確認する入口は次です。

```sh
glm-worker --project-state
```

CLIのcurrent command / usageは`glm-worker --help`を参照してください。

## Development rules

このリポジトリでの作業規則は[`AGENTS.md`](AGENTS.md)と[`IMPLEMENTATION_RULES.md`](IMPLEMENTATION_RULES.md)を参照してください。

ローカルLinux ARM64でのGo診断は[専用の実行入口](docs/linux-go-tests.md)を参照してください。正規Quality Gateの証拠とは区別します。

Darwin 専用テストの宣言要件は [正規の規則](IMPLEMENTATION_RULES.md#task-file必須構造) を参照してください。

## License

[MIT License](LICENSE)
