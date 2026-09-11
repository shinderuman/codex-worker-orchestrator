# codex-worker-orchestrator

Codexをsemantic judgmentとorchestrationへ集中させ、調査・実装・test・reviewを`glm-worker`経由でGLMへ安全に委譲するための環境です。

READMEはcurrent runtime stateや実装inventoryの第二正本ではありません。変更し得る値・command surface・state/schemaの正は、下記のcanonical sourceまたはlive commandから取得します。

## Authority

- repository作業規則: `AGENTS.md`、`IMPLEMENTATION_RULES.md`
- repository内の現在のtask schedule: `IMPLEMENTATION_PLAN.local.md`
- 個別task requirement: `IMPLEMENTATION_TASKS/*.md`
- project/tool-scoped Codex親規則の正本: `codex/AGENTS.md`（repository root `AGENTS.md`または`glm-codex-context`のproject-scoped `developer_instructions` bootstrapから参照）
- on-demand Codex契約: `codex/instructions/`
- worker/reviewer契約: `codex/glm-worker/prompts/`
- deterministic behavior: `glm-worker/internal/`のproduction codeと対応test
- tool versionとrepository-owned quality tool配置契約: `quality-tools.yml`
- managed Codex/Claude設定値: `codex/config-managed.toml`、`claude/settings-managed.json`
- ordinary completion evidence: Git、CI、bundle / telemetry

`IMPLEMENTATION_HISTORY.md`は通常の完了ledgerではなく、将来taskが明示参照するcross-taskの採否・Go/No-Go decisionだけを保持します。

## Setup

必要なtool versionとrepository-owned quality toolの配置契約は`quality-tools.yml`を参照してください。provider credentialはrepositoryで管理しません。

```sh
git clone https://github.com/shinderuman/codex-worker-orchestrator.git
cd codex-worker-orchestrator
./install-quality-tools.sh
./install.sh
```

quality toolはgenericなuser-global executable名を所有せず、repository専用のnamespaced pathへ配置されます。`QUALITY_TOOLS_BIN_DIR`を明示した場合も、そのdirectory内のnamespaced executableだけをrepository-ownedとして扱います。

installerの実装・配置対象・managed config merge・override境界は`install.sh`、`glm-worker/internal/install*`、対応testを正とします。runtimeへ影響する変更のinstalled/source一致やsmoke条件は`IMPLEMENTATION_RULES.md`と該当instructionを参照してください。

## Runtime discovery

`glm-worker`の現在のcommand一覧・usageは実binaryから取得します。

```sh
glm-worker --help
glm-worker --project-state
glm-worker --status
glm-worker --handoff
```

`--project-state`はPlan schedule/dependencyとcompletion条件、`--status`/`--handoff`はruntime lifecycle/state/evidenceをread-only projectionとして返します。具体的なfield、state transition、recovery、analysis/eval schemaはREADMEへ複製せず、production code、CLI出力、`codex/instructions/`を正とします。

親Codexの通常lifecycle操作は`glm-parent-action`を入口にします。利用可能なactionとargument contractは`glm-worker/internal/parentactioncmd/`および`codex/instructions/`を参照してください。

## Source map

```text
codex-worker-orchestrator/
├── AGENTS.md / IMPLEMENTATION_RULES.md
├── IMPLEMENTATION_PLAN.local.md / IMPLEMENTATION_TASKS/
├── quality-tools.yml / install-quality-tools.sh / install.sh
├── codex/                 # project/tool-scoped Codex contracts and managed artifacts
├── claude/                # managed Claude settings
├── glm-worker/
│   ├── cmd/               # thin binary entrypoints
│   └── internal/          # runtime, workflow, state, analysis, validation
├── tests/                 # cross-boundary fixtures/smoke
└── .github/workflows/     # repository CI
```

構成の完全なcurrent一覧はGit treeを正とし、この図は責務locatorだけを示します。

## Validation

repository標準validation入口:

```sh
./harnesslint

cd glm-worker
go test ./...
go vet ./...
go build ./...
```

固定tool version、quality threshold、CI wiringは`quality-tools.yml`、`.golangci.yml`、`.github/workflows/quality.yml`を正とします。sandbox等で通常のfull test実行に追加能力が必要な場合の入口は`glm-worker --help`と`codex/instructions/quality-gate-capability.md`から確認します。

## State and evidence

`glm-worker`のnamespaced runtime state、task event、telemetry、bundle/analysis artifactはproduction implementationがschema authorityです。現在のtask ID、HEAD、branch、dirty state、rate-limit/provider state、validation evidence、bundle schema versionや集計値をREADMEへsnapshotとして保存しません。

現在値はGitとlive projectionから取得し、過去のordinary completionはGit/CI/bundleから回収します。外部providerやCodex/Claude側で独立に変わる仕様・version・quota等もREADMEへcurrent valueを固定せず、実行時または該当taskのfeasibility確認でlive authorityを参照します。

## License

MIT License。詳細は`LICENSE`。
