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
- `glm-worker` / `glm-parent-action` / `glm-codex-context` / `commentlint` / `harnesslint`を`glm-worker` moduleからbuildして配置し、`merge-json` / `plancheck`はinstall中だけbuild-dirから実行
- `codex/`のmanaged fileを`~/.codex`へ同期し、前回manifestにのみ残るfileを削除
- managed Codex configを既存`~/.codex/config.toml`へ反映
- managed Claude settingsを既存`~/.claude/settings.json`へmerge
- Git clone上では`.githooks/post-merge`を有効化

installerはrepository test suiteやlintを実行せず、Implementation Planの状態機械も持たない。`git pull --ff-only`後はpost-merge hookから再installされる。

既定のmanaged Claude settingsはZ.aiのAnthropic互換endpointを使い、Claude Codeの`opus` / `sonnet` aliasをGLM-5.3、`haiku` aliasをGLM-4.7へ割り当てる。具体値は`claude/settings-managed.json`を正とし、認証情報はこのrepositoryで管理しない。

## Claude settings local override

端末local overrideは既定で`${XDG_CONFIG_HOME:-$HOME/.config}/codex-config/claude-settings.local.json`。`CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE`で変更できる。

形式はtop-level `env` objectだけ。stringはset/overwrite、`null`はunset。installerは`~/.claude/.codex-config-claude-env-state.json`へ適用前baselineを保持し、前回overrideを復元してからmanaged defaultsと今回overrideを適用する。sidecarだけを単体削除しない。

## 主なruntime設定

通常はmanaged defaultsのまま使用する。必要な場合は次の環境変数で配置先・runner・model routing等を上書きできる。

- `GLM_WORKER_HOME`（既定`~/.glm-worker`）: state、worktree、search cache、bundle exportの基点
- `GLM_WORKER_PROMPT_DIR`（既定`~/.codex/glm-worker/prompts`）: worker/reviewer prompt配置先
- `CODEX_CONFIG_DIR`（既定`~/.codex`）、`CLAUDE_CONFIG_DIR`（既定`~/.claude`）
- `GLM_WORKER_CLAUDE_BIN`（既定`claude`）、`GLM_WORKER_CODEX_BIN`（既定`codex`）
- `GLM_WORKER_WORKER_MODEL`（既定`opus`）、`GLM_WORKER_REVIEWER_MODEL`（既定`haiku`）、`GLM_WORKER_HIGH_RISK_REVIEWER_MODEL`（既定`sonnet`）
- `GLM_WORKER_EFFORT`（既定`high`）、`GLM_WORKER_ESCALATED_EFFORT`（既定`max`）、`GLM_WORKER_MAX_AUTO_FIX_ROUNDS`（既定`2`）
- `GLM_WORKER_TELEMETRY_CONTENT`（既定`true`）: prompt/system prompt/response本文をtelemetryへ保持するか
- `GLM_WORKER_REPO_SEARCH`（既定`true`）: worker/reviewerのBM25 repo-search注入とCLI searchをまとめて切り替える
- `GLM_WORKER_ENV_ALLOWLIST`: Claude child processへ追加で渡す環境変数名のcomma-separated list

installer自身の配置先は`GLM_WORKER_BIN_DIR`、`CODEX_CONFIG_DIR`、`CLAUDE_SETTINGS_FILE`でも上書きできる。詳細な意味契約はproduction configと`codex/instructions/`を正とする。

## 構成

```text
codex-worker-orchestrator/
├── AGENTS.md
├── install.sh
├── install-quality-tools.sh
├── quality-tools.yml
├── commentlint
├── harnesslint
├── .golangci.yml
├── .github/workflows/quality.yml
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
│   │   ├── glm-parent-action/
│   │   ├── glm-codex-context/
│   │   ├── commentlint/
│   │   ├── harnesslint/
│   │   ├── merge-json/
│   │   └── plancheck/
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

CI、root wrapper、`glm-worker`内部quality gate、installerは`quality-tools.yml`を同じversion authorityとして使う。version不一致はlint結果として扱わずpreflightで拒否する。`.github/workflows/quality.yml`はpull request、main push、manual `workflow_dispatch`で`./harnesslint`と`cd glm-worker && go test ./...`を実行する。

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

長時間validationはrun ID付きでstateへ記録され、`glm-worker --quality-gate status|watch|result <validation-run-id>`から再観測できる。

installer/managed-file behaviorを変更した場合だけoffline install smokeを実行する。

```sh
glm-worker --install-smoke --role worker
```

install smokeはtemp home/repositoryへinstallを2回行い、managed file、local設定保持、binary配置、idempotenceを確認する。provider credentialや実GLM/Z.ai接続は使わない。provider/isolation behaviorを変更した場合のlive integration確認とは分離する。

`tests/parent-behavior-evals.json`は決定論的testで証明できないliveな親/model行動の入力registryだけを保持し、production contractを複製しない。

## Target repository Codex context

`glm-worker`を使うtarget repositoryだけCodex Desktopの固定contextを軽量化する場合は、target repositoryで次を実行する。

```sh
glm-codex-context enable
glm-codex-context status
glm-codex-context disable
```

各commandは末尾にrepository pathを明示することもできる。`enable`はtarget repositoryの`.codex/config.toml`へtool-owned local profileを作成し、Skills catalog自動注入、Plugins/recommended-plugin、Apps instructions、collaboration-mode instructionsを無効化する。permissions/environment contextは変更しない。fileはrepositoryの`.git/info/exclude`だけで除外し、tracked `.gitignore`は変更しないため他のcloneやrepositoryへ設定を伝播しない。

既存の`.codex/config.toml`がtool-owned内容と一致しない場合は上書きせずfail closedする。`disable`もtool-owned内容だけを削除する。設定変更後は新しいCodex threadを開始する。通常はCodex Desktop自体の再起動を前提としない。別repositoryでは通常のCodex設定がそのまま使われる。

## CLI

親Codexの通常lifecycle操作は`glm-parent-action`を使う。Plan管理repositoryの新規task開始はcurrent ACTIVE taskを固定要求で起動する1操作、semantic payloadを持つdecision/fixとexecution milestone start/revisionはrepository内のbounded stagingを使う。

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

`prepare decision|fix|start-milestones|revise-milestones`のJSONが返す`path`は`.glm-worker-parent-actions/`配下だけで、通常の親Codex運用ではそのplaceholderを`apply_patch`でsemantic payloadへ置換する。実actionはpathを受け取らずcrypto-random tokenだけを受け取る。wrapperがpayloadをmemoryへ取り込みstaging fileを削除してから既存`glm-worker`入口へ委譲するため、sandbox外processへ任意local pathを読ませない。UTF-8 byte長・SHA-256・stdin framingもwrapperが処理する。

execution milestoneは1つのsemantic ACTIVE taskが大きい場合だけのruntime execution authorityで、task requirement自体を分割・複製しない。`no-go`はcurrent canonical parent action planがterminal observation no-goを許可している場合だけ成立する。詳細は`codex/instructions/task-request-boundary.md`と関連lifecycle instructionを正とする。

`finalize-check`は既存のblocking quality gateとcanonical `--handoff`を連続実行し、validationがcurrent snapshotへ対応することを確認して、validation・handoff・read-only local Git summaryを1件のJSONで返す。`status:"ready_for_parent_decision"`はsemantic acceptanceではなく親判断へ進める証拠であり、`status:"blocked"`は`failure.stage`/`reason`の境界へ親判断を戻す。accept/fix、commit message生成、commit、fetch、push、divergence修復は行わず、`git.remote_state`も`not_checked`として明示する。

低レベルstdin transportやinspection/report、recovery/debug commandは`glm-worker`を直接使う。`glm-worker`は成功時にmachine-readable JSONをstdoutへ1件返し、`--watch`だけJSON Lines streamを返す。失敗時はstdoutを空にしてstructured error JSONをstderrへ返す。

```sh
glm-worker "<task>"
glm-worker --execution-milestones-stdin <bytes> [--sha256 <sha256>]
glm-worker --execution-milestones-revise-stdin <bytes> [--sha256 <sha256>]
glm-worker --decision-stdin <bytes> [--sha256 <sha256>]
glm-worker --fix-stdin <bytes> [--sha256 <sha256>] [--origin <origin>] [--accepted-scope current-diff] [--approval-only]
glm-worker --accept
glm-worker --resume
glm-worker --stop
glm-worker --isolate
glm-worker --status
glm-worker --handoff
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
glm-worker --repo-search <query>
glm-worker --repo-search-eval
glm-worker --check-wake-coalesce <parent-thread-id> <resume-at-rfc3339>
glm-worker --verify-auto-resume <automation-key> <resume-at-rfc3339> <thread-id>
glm-worker --install-smoke [--role worker|reviewer|fix|parent]
glm-worker --quality-gate <go-test|go-test-race>
glm-worker --quality-gate <status|watch|result> <validation-run-id>
glm-worker --rotate-instruction-baseline
glm-worker bundle [task-id]
glm-worker --help
```

`glm-worker --repo-search <query>`はcurrent repositoryを既存BM25 coreでread-only検索し機械可読JSONを返す。repo-search feature全体は`GLM_WORKER_REPO_SEARCH`環境変数(既定enabled)で切り替わり、disabled時はworker/reviewerのsearch注入とCLI検索を実行しない。

`glm-worker --repo-search-eval`は保存済みtask eventとtask statsだけを読むread-only評価reportを返す。worker/reviewer各search routeのquery category、hit/miss/fallback/skip、result count、durationをraw query/result本文なしで集計し、event logとtask statsの加法整合をcross-checkする。同じtask stats集計はfixed eval-ab基盤(`glm_usage`解決)経由でA/B reportの`repo_search` blockへ接続し、実benchmark runは実行しない。

`--rotate-instruction-baseline`はactive taskがSol decision待ちのときだけ使える限定的なrecovery command。auto-resume検証、wake coalesce、quality-gate recovery等も親orchestration向けのspecialized commandであり、通常操作の手順は`codex/instructions/`を正とする。

## Lifecycle / parent action

lifecycle actionの合法性はstateのcanonical parent action planが決め、appとworkflowの両方が同じadmissionを使う。未解決の親actionやresume stateがある間は、新規taskを別taskとして開始できない。

主な境界は次のとおり。

- `waiting-decision`: decisionが必要。観測taskでcanonical planが許す場合だけ`no-go`も選べる
- `waiting-sol-review`: parent reviewを`accept`または必要時`fix`で解消する
- `complete`で未解決PASS reviewがある場合: `accept`で親outcomeを確定する
- `rate-limited` / `provider-unavailable` / `interrupted`: 保存済みcheckpointを`resume`する
- `guard-recoverable`: guardを修復してから`resume`する

`glm-worker --handoff`はcurrent task/status、canonical required/allowed action、resume kind、parent review、Git baseline/current snapshot、latest material model call、current snapshotへ対応するvalidation evidenceを1件のJSONへまとめる。lifecycle stateが矛盾している場合は`consistent:false`としてfail closed情報を返す。`glm-parent-action finalize-check`はこのhandoffをvalidationと組み合わせる親向け入口である。

`glm-worker --status`はrepo lock、task status/liveness、worker/reviewer session、parent wait state、current phase/model、rate limit/provider状態、resume可否、isolation等をread-only JSONで返す。同じrepoだけをlock単位とし、別repoのworkerは並列利用できる。

`glm-worker --stop`はrunning ownerのrepo-local Unix socket endpointへ停止要求を送り、owner側の停止・checkpoint保存ackを待つ安全停止入口。正常なuser interruptionは`interrupted`とresume checkpointを残し、再開は同じcheckoutで`--resume`を使う。`--isolate`はこのuser interruption状態だけを対象に、元taskを保持したまま別task用のgit worktree/branchを作る。詳細な保持・統合手順は`codex/instructions/glm-stop-isolate.md`を正とする。

## Evidence bundle

currentまたはretained taskの診断・dogfood evidenceを1つのZIPへ収集する場合は次を使う。

```sh
glm-worker bundle [task-id]
```

task ID省略時はcurrent taskがあればそれを、current taskがなければretained stats上の最新taskを対象にする。ZIPは既定で`$GLM_WORKER_HOME/exports/<repo SHA-256>/<task-id>.zip`へatomicに配置され、command outputは`archive_path`とtask/evidence coverage summaryを返す。

bundleにはtask telemetry/event/round/lifecycle/authority/artifact、関連Claude transcript、取得可能なparent Codex evidence、current taskではrepository authority/status/task diff等を収集する。`manifest.json`はtask/statusとevidence coverageを、`collection.json`は各entryの取得・readability情報を、`analysis-index.json`は親session window、validation、retry等の解析用indexを保持する。current/in-flight/未完了taskはcoverageが`open`、evidence欠損・readability anomalyは`partial`、完了済みretained taskで必要evidenceが揃う場合は`closed`となる。bundleは診断evidenceでありrepository contractの代替ではない。

## State

repositoryごとの主stateは`$GLM_WORKER_HOME/sessions/<repo SHA-256>/`。task/session/checkpoint/telemetry/artifactはrepo単位に分離し、同一repoの同時実行だけlockする。異なるrepoは並列利用できる。

既定`GLM_WORKER_HOME=~/.glm-worker`では、隔離worktreeは`worktrees/`、repo-search cacheは`search/`、bundleは`exports/<repo SHA-256>/`へ分離する。state repository keyは解決済みrepository root pathのSHA-256であるため、別pathのworktreeは別state/lock/sessionを持つ。

## Self-protection

このrepository自身のcritical production/config/instruction/prompt/quality surface変更は`glm-worker/internal/workflow/selfprotection.go`でHIGHへ固定する。workerのLOW自己申告やreviewerのPASSだけでは完結しない。test・docs・観測fileは内容に応じ通常review対象だが、quality policy surfaceは別途machine gateで保護する。

## License

MIT License。詳細は`LICENSE`。
