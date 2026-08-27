# codex-worker-orchestrator

Codex + GLM worker環境の配布元。Codex(Sol High)は判断とorchestrationに専念し、リポジトリ固有の調査・実装・test・reviewを`glm-worker`経由のGLM worker/reviewerへ委譲する。

運用契約の正は配置先の`~/.codex/AGENTS.md`(routing)と`~/.codex/instructions/*.md`(event別contract)、wrapされる側の行動契約は`codex/glm-worker/prompts/*.md`である。決定論的な評価契約はproduction実装・Go test・`glm-worker/scenarios/`が担い、liveな親/model行動だけ`tests/parent-behavior-evals.json`へ分離する。本READMEやlive Eval registryをproduction contractの第二正本にはしない。

## 初回

```sh
git clone https://github.com/shinderuman/codex-worker-orchestrator.git
cd codex-worker-orchestrator
./install.sh
```

ZIPからでも同じで、任意の場所へ展開して`./install.sh`を実行する。

`install.sh`は次だけを行う。

- `codex/`配下の管理対象(`AGENTS.md`・`instructions/`・`rules/`・`glm-worker/prompts/`)を`~/.codex`へ配置。manifestで追跡し、repo側で削除・改名されたfileは次回install時に配置先から削除する
- `config-managed.toml`の管理対象キーだけ`~/.codex/config.toml`へ反映
- `glm-worker`とJSON merge toolをGoでtest/buildしpreflight合格後に配置(preflight失敗時は管理fileを更新しない)
- `IMPLEMENTATION_PLAN.local.md`をGit管理するrepositoryでは、配置前にfinal HEADのplanがfinal HEAD postconditionを満たすことを検証する
- Claudeのmanaged設定だけ`~/.claude/settings.json`へmerge
- Git clone上で実行した場合は`post-merge` hookを有効化

`rules/default.rules`、auth、sessions、SQLite、cache等の既存runtimeには触れない。バックアップは作成しない。install完了後、既に開いているCodexタスクが`AGENTS.md`を再読込する保証はないため、ルール反映には新しいCodexタスクを開始する。

## 2回目以降

```sh
git pull --ff-only
```

初回`install.sh`で設定した`post-merge`から自動的に`install.sh`が実行される。hookを使いたくない場合は`git pull --ff-only && ./install.sh`。

## Claude接続先の端末local override

端末ごとにClaude Codeの接続先・model設定だけをoverrideできる(業務PCでAnthropic Claude Teamへ切替える等)。overrideはGit管理外で、installer共通動作・既存local fileには影響しない。

- path: 既定`${XDG_CONFIG_HOME:-$HOME/.config}/codex-config/claude-settings.local.json`(`CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE`で変更)。repo名とnamespace(`codex-config`)は意図的に異なり、override path・env変数・sidecar・manifestは公開済みのため`codex-config`を恒久維持する
- 形式: Claude settingsの`env`だけを対象としたJSON。string値は追加・上書き、`null`はunset(実際に削除し再流入させない)、空文字は文字列値扱い。top-levelはobject・keyは`env`のみ・env値はstringか`null`のみで、壊れたJSON・未対応形式はinstall・runnerともfail closedになる
- 運用: overrideを追加・変更・削除したら`install.sh`を再実行する。installerは`~/.claude/`直下のGit管理外sidecar `.codex-config-claude-env-state.json`へ適用前baseline(schema version 1)を記録し、毎回「前回stateの全所有keyを元値へ復元→managed defaultsをmerge→今回のbaselineをsnapshot→patch適用」の順で実行する。これによりoverrideから外したkeyやoverride削除後の再installで各keyは確実に元へ戻る。stateは0600・atomic writeで認証情報には触れない
- sidecarだけを単体削除してはならない。baselineが失われ現在値が新baselineへ固定され、override解除で元値へ復元できなくなる。復旧はsettings.jsonとsidecarを整合した既知状態へ一緒に戻すか、対象keyを手動で正しいbaselineへ戻す
- glm-worker起動時にも同じoverrideを読みchild envへset/deleteを再適用する(stateは読まない)。override不要な端末では本fileを作成しない

## 構成

```text
codex-worker-orchestrator/
├── AGENTS.md                 # このリポジトリの作業bootstrap規則
├── install.sh
├── codex/
│   ├── AGENTS.md             # ~/.codex/AGENTS.mdへ配置(Sol High routing)
│   ├── config-managed.toml
│   ├── instructions/         # 親Codex向けevent別contract + worker/規則
│   ├── rules/
│   │   └── glm-worker.rules
│   └── glm-worker/
│       └── prompts/          # WORKER.md / REVIEWER.md (system prompt)
├── glm-worker/
│   ├── cmd/                  # CLI entrypoint (glm-worker, commentlint)
│   ├── internal/             # app / config / packet / runner / state / workflow
│   └── scenarios/            # deterministic workflow scenario corpus
├── claude/
│   └── settings-managed.json
├── tools/
│   └── merge-json/
├── tests/
│   ├── install_smoke.sh
│   └── parent-behavior-evals.json # explicit許可時だけ実行するlive Eval入力
└── .githooks/
    └── post-merge
```

`cmd/glm-worker`は薄いentrypointとし、外部公開しない実装は`internal`配下へ置く。package間の依存は`app`から各機能へ向け、状態永続化とworkflowを分離する。

## glm-worker CLI

```sh
glm-worker "<新規タスク>"
glm-worker --decision-stdin <判断本文のUTF-8 byte数> [--sha256 <sha256 hex>]
glm-worker --fix-stdin <指示本文のUTF-8 byte数> [--sha256 <sha256 hex>] [--origin codex-review|glm-reviewer|user-amendment|external-review|metadata-repair]
glm-worker --accept
glm-worker --resume
glm-worker --stop
glm-worker --isolate
glm-worker --status
glm-worker --watch "[--verbose]"
glm-worker --timeline "[task ID]"
glm-worker --convergence "[task ID]"
glm-worker --stats
glm-worker --reset
glm-worker --eval-ab "<A/B run dir>"
glm-worker --call-outliers
glm-worker --codex-limit
glm-worker --check-wake-coalesce "<親実装task thread ID>" "<GLM auto-resume時刻 RFC3339>"
```

要点だけ記す。個別commandの呼び出し手順は配置先`~/.codex/instructions/`(glm-execution・glm-packets・glm-auto-resume等)が契約を持ち、機械的な出力契約の正はproduction実装と対応するGo test/scenarioである。

- `--decision-stdin`は`NEEDS_SOL_DECISION`停止taskの継続、`--fix-stdin`は`NEEDS_SOL_REVIEW`後だけの差戻し。本文はshell quotingを通さないstdin modeで渡し、glm-workerがstderrへ出すREADY control event(`{"type":"control","event":"stdin_ready"}`)確認後に1回だけ書く。読み取り不足・`--sha256`不一致・event未観測はstate変更・model呼出前にfail closedする。廃止済みargv埋込み`--decision`/`--fix`はusage errorになる
- `--origin`は差戻し元申告(有限集合。未申告は`unknown`計上で`codex-review`へ推定しない)。`--accept`はterminal packet採用の観測記録専用
- `--resume`は5h上限・provider一時障害・`--stop`停止で中断した同一phase・session・checkpointの再開。中断taskのstate・session・working treeは破棄せず、`--reset`なしの新規task投入はfail closedする
- `--stop`は単一目的local control endpoint経由の安全停止、`--isolate`は停止中の元taskを保持したまま割り込みtask用のgit worktree隔離checkoutを作る。手動kill・同一checkoutへの重ねtaskは使わない
- `--status`・`--watch`(event logのread-only JSONL stream)・`--timeline`・`--convergence`(round log・telemetry由来のreview/fix収束)・`--call-outliers`(全task横断のworker呼出分布・outlier)・`--eval-ab`(A/B run dir検証)・`--codex-limit`(Codex CLI rate-limit読取)・`--verify-auto-resume`・`--check-wake-coalesce`(既存Codex 5h wake実体照合によるGLM wake重複排除判定)はすべてAI呼出・repo lock・state書換をしない参照専用command
- `--stats`は完了済みと現在taskの集計(model alias別呼出・token・risk floor・snapshot mismatch・probe・parent outcome等)。`--reset`は現在統計をarchiveして実行状態を消去する

停止・再開の分類と機械契約(5h上限signal、provider一時障害のtransient分類とprobe、`--stop`保持基準、`--isolate`統合条件)の正はproduction実装と対応するGo test/scenario corpusである。error時はstderrへerror JSON 1行とnon-zero exitで、種別は`rate_limited`・`provider_unavailable`・`interrupted`・`worker_error`等の`kind`で識別できる。

主な環境変数:

| 変数 | 既定値 | 用途 |
|---|---|---|
| `GLM_WORKER_HOME` | `~/.glm-worker` | task・session・statsの保存先 |
| `GLM_WORKER_PROMPT_DIR` | `~/.codex/glm-worker/prompts` | worker/reviewer prompt |
| `GLM_WORKER_CLAUDE_BIN` | `claude` | Claude Code実行ファイル |
| `GLM_WORKER_CODEX_BIN` | `codex` | Codex CLI実行ファイル(`--codex-limit`) |
| `GLM_WORKER_WORKER_MODEL` | `opus` | worker model alias |
| `GLM_WORKER_REVIEWER_MODEL` | `haiku` | 通常reviewer model alias |
| `GLM_WORKER_HIGH_RISK_REVIEWER_MODEL` | `sonnet` | 高リスク・Sol判断後・修正後reviewer model alias |
| `GLM_WORKER_EFFORT` | `high` | 通常実行effort |
| `GLM_WORKER_ESCALATED_EFFORT` | `max` | Sol判断後・明示fixのeffort |
| `GLM_WORKER_MAX_AUTO_FIX_ROUNDS` | `2` | 自動修正の上限回数 |
| `GLM_WORKER_TELEMETRY_CONTENT` | `true` | 呼出ログへprompt/response本文を保存するか |

model routing(worker=opus、通常reviewer=haiku、高リスク系reviewer=sonnet、effort high/max、auto-compact window 500K)の契約は配置先`~/.codex/AGENTS.md`と`claude/settings-managed.json`が正である。sessionはリポジトリ永久ではなくタスク単位で、同一タスク内のdecision・fix・resumeではsessionを維持する。

リポジトリごとの状態は`$GLM_WORKER_HOME/sessions/<repo SHA-256>/`へ保存する。`task.status`を正規状態とし、`task-stats.json`は観測用mirror。呼出単位の詳細は`telemetry/<task ID>.jsonl`へ`0600`で保存し、本文保存は`GLM_WORKER_TELEMETRY_CONTENT=false`で無効化できる。stats・telemetryの破損や書き込み失敗はwarningでworkflowを継続する。packetへ収まらない成果物はtask別artifact dirへ保存し、packetでは絶対パスだけを受け渡す。

## 複数リポジトリの並列利用

異なるrepositoryでglm-workerを同時利用できることを通常contractとする。全体を直列化するglobal lock・daemon・socket・schedulerは持たず、state dir・repo lock・session・telemetry・event log・artifact・repo-search cacheはすべて`GLM_WORKER_HOME`配下のrepo hash単位へ分離される。同一repoの2本目の起動だけflock拒否され、他repoの起動・実行はblockしない。`--verify-auto-resume`・`--check-wake-coalesce`・`--codex-limit`のCodex config dir・app-server accessは読み取り専用。provider quotaはaccount単位のupstream管理であり、同一quotaを2 repoが消費すること自体はbugではない。このcontractは`glm-worker/internal/app/multirepo_process_test.go`・`multirepo_reposearch_test.go`・`multirepo_pty_test.go`で固定する。

## 自己保護 (self-protection)

glm-workerはこの配布repo自身を作業対象にした変更について、wrapper側のcritical surface判定で実効riskをHIGHへ固定し、workerのLOW自己申告やreviewerのPASSだけでは完結させない。判定の唯一の正は`glm-worker/internal/workflow/selfprotection.go`であり、`internal/`・`cmd/`配下のproduction Go、installer適用経路、管理settings、依存manifest、scenario corpus、`codex/instructions/`・`codex/rules/`・`codex/glm-worker/prompts/`・両`AGENTS.md`がHIGH、test file・検証harness・docs(`README.md`等)・観測専用fileは通常reviewのまま、という意味分類をunit testが全tracked fileへ強制する。行動固定はscenario corpus(`orchestrator-critical-low-self-declare`等)による。

## 開発時の検証

```sh
cd glm-worker
go test ./...
go test -race ./...
go vet ./...
go build -o /dev/null ./cmd/glm-worker
go run ./cmd/glm-worker --install-smoke --role worker
```

決定論的に検証できる契約はGo test・`glm-worker/scenarios/`を正とする。親Codex/modelの実行行動そのものを必要とするlive Evalだけ`tests/parent-behavior-evals.json`へ保持し、registryの`run_policy`どおりユーザーの明示許可なしには実行しない。registryは未実行状態と評価入力を保持するだけで、instruction contractを複製する正本にはしない。

full install smokeは`tests/install_smoke.sh`を直接実行せず、`glm-worker --install-smoke`単一入口で取得・確認する(未配置環境では上記の`go run`相当)。実行・再利用の契約は`codex/instructions/install-smoke-evidence.md`を参照する。install smokeのscenario分類とreal/contract実行境界は`tests/install-smoke-coverage.md`を参照する。

Codex sandbox内ではUnix socket bindが拒否されるため、Unix socketを必要とする`glm-worker` moduleのfull suiteは`glm-worker` module rootから`glm-worker --quality-gate go-test|go-test-race`として実行し、既存のglm-worker prefix ruleによって最初から昇格境界へ一度だけdispatchする。この入口は固定argvだけを受理して余分なargvをfail closedで拒否する。sandbox内で成立する他moduleのGo suiteやvet/build/lint等のgateは直接sandbox実行のまま昇格しない。capabilityと実行境界の契約は`codex/instructions/quality-gate-capability.md`を参照する。

## ライセンス

本リポジトリはMIT Licenseの下で配布する。詳細は[LICENSE](LICENSE)を参照。
