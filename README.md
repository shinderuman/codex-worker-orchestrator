# codex-worker-orchestrator

Codex(Sol High)を判断・orchestrationへ集中させ、repository調査・実装・test・reviewを`glm-worker`経由のGLM worker/reviewerへ委譲するための環境。

運用契約の正は配置先`~/.codex/AGENTS.md`と`~/.codex/instructions/`、worker/reviewer行動契約は`codex/glm-worker/prompts/`、決定論的な挙動はproduction実装と対応するtestで保持する。READMEはcontractの第二正本にしない。

## 必要command

- Go 1.25.4 toolchain
- lint解析用Go 1.22.12 toolchain
- git
- rsync
- `golangci-lint` 2.7.0
- `shellcheck` 0.11.0
- `shfmt` 3.13.1
- Claude Code CLI(runtimeで必要)

## Install

```sh
git clone https://github.com/shinderuman/codex-worker-orchestrator.git
cd codex-worker-orchestrator
./install-quality-tools.sh
export PATH="$HOME/.local/bin:$PATH"
./install.sh
```

`install.sh`がquality toolの不足またはversion不一致を報告した場合も、`./install-quality-tools.sh`で`quality-tools.yml`の全固定versionを導入する。Go toolchainはGoのtoolchain管理、実行fileは`~/.local/bin`を使用する。別配置先は`QUALITY_TOOLS_BIN_DIR`で指定し、そのdirectoryを`PATH`へ追加する。

`install.sh`は次だけを行う。

- runtimeに必要なcommandの存在確認
- `quality-tools.yml`に固定した実行用Go・lint解析用Go・lint tool versionの検証
- `glm-worker` / `commentlint` / `harnesslint` / `merge-json`を`glm-worker` moduleからbuildして配置
- `codex/`のmanaged fileを`~/.codex`へ同期し、前回manifestにのみ残るfileを削除
- managed Codex configを既存`~/.codex/config.toml`へ反映
- managed Claude settingsを既存`~/.claude/settings.json`へmerge
- Git clone上では`.githooks/post-merge`を有効化

installerはrepository test suiteやlintを実行せず、Implementation Planの状態機械も持たない。`git pull --ff-only`後はpost-merge hookから再installされる。

## Claude settings local override

端末local overrideは既定で`${XDG_CONFIG_HOME:-$HOME/.config}/codex-config/claude-settings.local.json`。`CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE`で変更できる。

形式はtop-level `env` objectだけ。stringはset/overwrite、`null`はunset。installerは`~/.claude/.codex-config-claude-env-state.json`へ適用前baselineを保持し、前回overrideを復元してからmanaged defaultsと今回overrideを適用する。sidecarだけを単体削除しない。

## 構成

```text
codex-worker-orchestrator/
├── AGENTS.md
├── install.sh
├── commentlint
├── harnesslint
├── .golangci.yml
├── codex/
│   ├── AGENTS.md
│   ├── config-managed.toml
│   ├── instructions/
│   ├── rules/
│   └── glm-worker/prompts/
├── glm-worker/
│   ├── go.mod
│   ├── cmd/
│   │   ├── glm-worker/
│   │   ├── commentlint/
│   │   ├── harnesslint/
│   │   └── merge-json/
│   └── internal/
├── claude/settings-managed.json
├── tests/
│   ├── install_smoke.sh
│   └── parent-behavior-evals.json
└── .githooks/post-merge
```

Go commandは薄い`cmd/<name>/main.go`とし、実装責務は`internal/`へ置く。Go moduleは`glm-worker/go.mod`だけをcanonicalとする。

## Quality gate

`harnesslint`はこのrepository専用のmachine quality gate。GoだけでなくShell/smoke、Markdown/prompt/instruction、structured config、quality-gate wiringを検査する。

```sh
./harnesslint
./harnesslint --fix
```

`harnesslint`は標準Go lint群、`shellcheck`、`shfmt`、`commentlint`とrepository固有ruleを集約する。主な固有ruleは、prose contract pin、test-only production/state-machine、scenario self-test、追加Go module、thin wrapper、smoke scope逸脱、quality bypass、runtime Markdown肥大、stale authority、quality config弱体化、quality wiring欠落を対象とする。

CI、root wrapper、`glm-worker`内部quality gate、installerは`quality-tools.yml`を同じversion authorityとして使う。version不一致はlint結果として扱わずpreflightで拒否する。

このrepositoryを`glm-worker`自身で変更する場合、worker終了後・reviewer開始前にwrapperがcheck-only quality gateを必ず通す。不合格ならreviewerへ進まずworker修正へ戻る。GLM task中の`.golangci.yml`、harnesslint/commentlint実装・entrypoint・wrapper変更は`quality-surface-dirty`で拒否する。gate wiring自体の削除も`quality-wiring`で拒否する。Linterのpolicy変更が必要なら通常task内で自己変更せず、具体的なfalse positive/negativeと最小再現を親へ報告する。

他repositoryではこのrepository固有`harnesslint`を適用せず、従来のcomment policyだけを維持する。

## Test

通常のGo検証:

```sh
cd glm-worker
go test ./...
go vet ./...
go build ./...
```

Codex sandboxではUnix socket bindを必要とするfull suiteだけ固定quality-gate入口を使う。

```sh
glm-worker --quality-gate go-test
glm-worker --quality-gate go-test-race
```

installer/managed-file behaviorを変更した場合だけoffline install smokeを実行する。

```sh
glm-worker --install-smoke --role worker
```

install smokeはtemp home/repositoryへinstallを2回行い、managed file、local設定保持、binary配置、idempotenceを確認する。provider credentialや実GLM/Z.ai接続は使わない。provider/isolation behaviorを変更した場合のlive integration確認とは分離する。

`tests/parent-behavior-evals.json`は決定論的testで証明できないliveな親/model行動の入力registryだけを保持し、production contractを複製しない。

## CLI

主要command:

```sh
glm-worker "<task>"
glm-worker --decision-stdin <bytes> [--sha256 <sha256>]
glm-worker --fix-stdin <bytes> [--sha256 <sha256>] [--origin <origin>]
glm-worker --accept
glm-worker --resume
glm-worker --stop
glm-worker --isolate
glm-worker --status
glm-worker --watch [--verbose]
glm-worker --timeline [task-id]
glm-worker --convergence [task-id]
glm-worker --stats
glm-worker --reset
glm-worker --eval-ab <run-dir>
glm-worker --call-outliers
glm-worker --model-routing
glm-worker --test-impact
glm-worker --codex-limit
glm-worker --check-wake-coalesce <parent-thread-id> <resume-at-rfc3339>
```

詳細な呼出条件・packet契約・auto-resume・stop/isolate・feasibility gate等は`codex/instructions/`が持つ。

## State

repositoryごとのstateは`$GLM_WORKER_HOME/sessions/<repo SHA-256>/`。task/session/checkpoint/telemetry/artifactはrepo単位に分離し、同一repoの同時実行だけlockする。異なるrepoは並列利用できる。

## Self-protection

このrepository自身のcritical production/config/instruction/prompt/quality surface変更は`glm-worker/internal/workflow/selfprotection.go`でHIGHへ固定する。workerのLOW自己申告やreviewerのPASSだけでは完結しない。test・docs・観測fileは内容に応じ通常review対象だが、quality policy surfaceは別途machine gateで保護する。

## License

MIT License。詳細は`LICENSE`。
