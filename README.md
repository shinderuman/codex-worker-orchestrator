# codex-worker-orchestrator

Codex(Sol High)を判断・orchestrationへ集中させ、repository調査・実装・test・reviewを`glm-worker`経由のGLM worker/reviewerへ委譲するための環境。

運用契約の正は配置先`~/.codex/AGENTS.md`と`~/.codex/instructions/`、worker/reviewer行動契約は`codex/glm-worker/prompts/`、決定論的な挙動はproduction実装と対応testで保持する。READMEはcontractの第二正本にしない。

## 必要command

- Go 1.25.4 / lint解析用Go 1.22.12
- git / rsync
- `golangci-lint` 2.7.0 / `shellcheck` 0.11.0 / `shfmt` 3.13.1
- Claude Code CLI(runtimeで必要)

固定versionの正は`quality-tools.yml`。

## Install

```sh
git clone https://github.com/shinderuman/codex-worker-orchestrator.git
cd codex-worker-orchestrator
./install-quality-tools.sh
export PATH="$HOME/.local/bin:$PATH"
./install.sh
```

`install.sh`は固定tool versionとruntime commandを検証し、`glm-worker` / `glm-parent-action` / `glm-codex-context` / `commentlint` / `harnesslint`をbuildして`~/.local/bin`へ配置する。`merge-json` / `plancheck`はinstall中だけbuild-dirから使う。`codex/`のmanaged fileを`~/.codex`へ同期し、managed Codex configとClaude settingsを既存設定へmergeし、Git cloneでは`.githooks/post-merge`を有効化する。repository test/lintはinstallerでは実行しない。`git pull --ff-only`後はpost-merge hookから再installされる。

managed Claude settingsはZ.ai Anthropic互換endpointを使い、Claude Codeの`opus` / `sonnet` aliasをGLM-5.3、`haiku` aliasをGLM-4.7へ割り当てる。具体値は`claude/settings-managed.json`を正とし、認証情報は管理しない。

端末local overrideは既定`${XDG_CONFIG_HOME:-$HOME/.config}/codex-config/claude-settings.local.json`（`CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE`で変更可）。top-level `env`だけを受け、stringはset/overwrite、`null`はunset。適用前baselineは`~/.claude/.codex-config-claude-env-state.json`へ保持する。

主なruntime override:

- `GLM_WORKER_HOME`=`~/.glm-worker`、`GLM_WORKER_PROMPT_DIR`=`~/.codex/glm-worker/prompts`
- `CODEX_CONFIG_DIR`=`~/.codex`、`CLAUDE_CONFIG_DIR`=`~/.claude`
- `GLM_WORKER_CLAUDE_BIN`=`claude`、`GLM_WORKER_CODEX_BIN`=`codex`
- model alias: worker=`opus`、reviewer=`haiku`、high-risk reviewer=`sonnet`
- effort: normal=`high`、escalated=`max`、auto-fix rounds=`2`
- `GLM_WORKER_TELEMETRY_CONTENT=true`、`GLM_WORKER_REPO_SEARCH=true`
- `GLM_WORKER_ENV_ALLOWLIST`: Claude childへ追加で渡す環境変数名
- installer配置先は`GLM_WORKER_BIN_DIR` / `CODEX_CONFIG_DIR` / `CLAUDE_SETTINGS_FILE`でも変更可能

## 構成

```text
codex-worker-orchestrator/
├── AGENTS.md
├── install.sh / install-quality-tools.sh / quality-tools.yml
├── commentlint / harnesslint / .golangci.yml
├── .github/workflows/quality.yml
├── codex/
│   ├── AGENTS.md / config-managed.toml
│   ├── instructions/ / rules/
│   └── glm-worker/prompts/
├── glm-worker/
│   ├── go.mod
│   ├── cmd/{glm-worker,glm-parent-action,glm-codex-context,commentlint,harnesslint,merge-json,plancheck}/
│   └── internal/
├── claude/settings-managed.json
├── tests/{install_smoke.sh,parent-behavior-evals.json}
└── .githooks/post-merge
```

Go commandは薄い`cmd/<name>/main.go`、実装責務は`internal/`へ置く。Go moduleは`glm-worker/go.mod`だけをcanonicalとする。

## Quality gate / Test

`harnesslint`はこのrepository専用のmachine quality gateで、Go、Shell/smoke、Markdown/prompt/instruction、structured config、quality wiringを検査する。

```sh
./harnesslint
./harnesslint --fix

cd glm-worker
go test ./...
go vet ./...
go build ./...
```

CI・root wrapper・`glm-worker`内部gate・installerは`quality-tools.yml`を共通version authorityとして使い、不一致はpreflightで拒否する。`.github/workflows/quality.yml`はpull request、main push、manual `workflow_dispatch`で`./harnesslint`とfull Go test suiteを実行する。

Codex sandboxでUnix socket bindを必要とするfull suiteは固定入口を使う。

```sh
glm-worker --quality-gate go-test
glm-worker --quality-gate go-test-race
```

validationはrun ID付きでstateへ記録され、`glm-worker --quality-gate status|watch|result <validation-run-id>`で再観測できる。installer/managed-file behavior変更時だけ`glm-worker --install-smoke --role worker`を使う。smokeはtemp home/repoへoffline installを2回行い、managed file、local設定保持、binary、idempotenceを検証し、provider credentialや実GLM/Z.ai接続は使わない。

このrepository自身を`glm-worker`で変更するとworker終了後・reviewer前にcheck-only gateを必ず通す。quality policy surfaceの自己変更はmachine gateでfail closedする。他repositoryへこのrepository固有`harnesslint`は適用しない。

`tests/parent-behavior-evals.json`は決定論的testで証明できないliveな親/model行動の入力registryだけを保持する。

## Target repository Codex context

target repositoryだけCodex Desktopの固定contextを軽量化する場合:

```sh
glm-codex-context enable [repository]
glm-codex-context status [repository]
glm-codex-context disable [repository]
```

`enable`は`.codex/config.toml`へtool-owned local profileを作り、Skills catalog自動注入、Plugins/recommended-plugin、Apps instructions、collaboration-mode instructionsを無効化する。permissions/environment contextは変えず、`.git/info/exclude`だけで除外する。既存fileがtool-owned内容と一致しなければ上書きせずfail closedし、`disable`もtool-owned内容だけを削除する。変更後は新しいCodex threadを開始する。

## CLI

親Codexの通常lifecycle操作は`glm-parent-action`を使う。

```sh
glm-parent-action start
glm-parent-action prepare start-milestones
glm-parent-action start-milestones <token>
glm-parent-action prepare revise-milestones
glm-parent-action revise-milestones <token>
glm-parent-action prepare decision
glm-parent-action decision <token>
glm-parent-action no-go
glm-parent-action prepare fix
glm-parent-action fix <token> [--origin <origin>] [--accepted-scope current-diff] [--approval-only]
glm-parent-action accept
glm-parent-action resume
glm-parent-action finalize-check <go-test|go-test-race>
```

Plan管理repoの`start`はcurrent ACTIVE taskを固定要求で起動する。decision/fixとexecution milestone start/revisionは`.glm-worker-parent-actions/`内のtoken-bound stagingを使い、実actionはpathではなくcrypto-random tokenだけを受ける。wrapperはpayloadをmemoryへ取り込みstaging fileを削除後、UTF-8 byte長・SHA-256・stdin framingを処理して`glm-worker`へ渡す。

execution milestoneは大きい1つのsemantic ACTIVE taskを2〜8 unitへ区切るruntime authorityで、task requirement自体を分割しない。`no-go`はcanonical parent action planがterminal observation no-goを許す場合だけ成立する。詳細は`codex/instructions/task-request-boundary.md`等を正とする。

`finalize-check`はblocking quality gateとcanonical `--handoff`を連続実行し、current snapshotに対応するvalidation・handoff・read-only local Git summaryをJSONで返す。accept/fix、commit、fetch/pushやdivergence修復は行わない。

低レベルtransport、inspection/report、recovery/debugは`glm-worker`を直接使う。全commandの現在一覧は`glm-worker --help`がJSONで返す。主要surface:

```sh
glm-worker "<task>"
glm-worker --decision-stdin <bytes> [--sha256 <sha256>]
glm-worker --fix-stdin <bytes> [--sha256 <sha256>] [--origin <origin>] [--accepted-scope current-diff] [--approval-only]
glm-worker --accept | --resume | --stop | --isolate | --reset
glm-worker --status | --handoff | --watch [--verbose]
glm-worker --timeline [task-id] | --convergence [task-id] | --stats
glm-worker --repo-search <query> | --repo-search-eval
glm-worker --eval-ab <run-dir> | --call-outliers | --model-routing | --test-impact | --codex-limit
glm-worker bundle [task-id]
```

execution-milestone stdin、auto-resume verification/wake coalesce、install smoke、quality-gate recovery、instruction-baseline rotation等のspecialized surfaceも`--help`へ含まれる。通常親workflowでは対応する`glm-parent-action`/`codex/instructions/`を使う。

`glm-worker`は成功時stdoutへmachine-readable JSON 1件を返し、`--watch`だけJSON Lines stream。失敗時stdoutを空にしてstructured error JSONをstderrへ返す。

`--reset`はcurrent task statsをarchiveしてtask/session/checkpoint stateを明示的に破棄するrecovery操作で、model callは行わない。

`--repo-search`はcurrent repoをBM25 coreでread-only検索する。`GLM_WORKER_REPO_SEARCH=false`ではworker/reviewer search注入とCLI searchをまとめて無効化する。`--repo-search-eval`は保存済みtask event/statsだけからquery category、outcome、result count、durationと整合性を集計し、実benchmarkは走らせない。

## Lifecycle / parent action

合法な親actionはstateのcanonical parent action planが決め、app/workflowが同じadmissionを使う。未解決action/resume stateがある間は新規taskを開始できない。

- `waiting-decision` → decision（観測taskでplanが許す場合だけ`no-go`）
- `waiting-sol-review` → `accept`または`fix`
- `complete` + unresolved PASS review → `accept`
- `rate-limited` / `provider-unavailable` / `interrupted` → saved checkpointを`resume`
- `guard-recoverable` → guard修復後`resume`

`--handoff`はtask/status、required/allowed action、resume kind、parent review、Git baseline/current snapshot、latest material call、current validation evidenceをJSONへまとめる。lifecycle矛盾時は`consistent:false`。`--status`はrepo lock、task liveness/session/phase/model、parent wait、rate limit/provider、resume/isolation等をread-only JSONで返す。

`--stop`はrepo-local Unix socketへ停止要求しowner ackを待つ安全停止入口。user interruptionは`interrupted`とresume checkpointを残し、同じcheckoutで`--resume`する。`--isolate`はこの状態だけを対象に元taskを保持した別task用git worktree/branchを作る。詳細は`codex/instructions/glm-stop-isolate.md`。

## Evidence bundle / State

```sh
glm-worker bundle [task-id]
```

task ID省略時はcurrent task、なければretained stats上の最新taskを対象にする。ZIPは既定`$GLM_WORKER_HOME/exports/<repo SHA-256>/<task-id>.zip`へatomic配置し、outputは`archive_path`とcoverage summaryを返す。task telemetry/event/round/lifecycle/authority/artifact、関連Claude transcript、取得可能なparent Codex evidence、current taskではrepository authority/status/task diff等を収集する。`manifest.json`、`collection.json`、`analysis-index.json`がcoverage/readability/analysis metadataを持つ。coverageはcurrent/in-flight/未完了で`open`、欠損/readability anomalyで`partial`、完了retained evidenceが揃えば`closed`。

主stateは`$GLM_WORKER_HOME/sessions/<repo SHA-256>/`。task/session/checkpoint/telemetry/artifactはrepo単位で、同じrepoだけをlockする。既定`~/.glm-worker`ではisolation worktree=`worktrees/`、repo-search cache=`search/`、bundle=`exports/`。repo keyは解決済みroot pathのSHA-256なので別path worktreeは別state/lock/sessionを持つ。

## Self-protection

このrepository自身のcritical production/config/instruction/prompt/quality surface変更は`glm-worker/internal/workflow/selfprotection.go`でHIGHへ固定する。workerのLOW自己申告やreviewer PASSだけでは完結しない。test/docs/観測fileは内容に応じ通常review対象、quality policy surfaceは別途machine gateで保護する。

## License

MIT License。詳細は`LICENSE`。
