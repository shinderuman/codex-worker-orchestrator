# codex-worker-orchestrator

Codex + GLM worker環境の配布元。

## 初回

```sh
git clone https://github.com/shinderuman/codex-worker-orchestrator.git
cd codex-worker-orchestrator
./install.sh
```

ZIPからでも同じで、任意の場所へ展開して`./install.sh`を実行する。

`install.sh`は次だけを行う。

- `codex/`配下の管理対象を`~/.codex`へ配置
- `config-managed.toml`の管理対象キーだけ`~/.codex/config.toml`へ反映
- `glm-worker`をGoでtest/buildし`~/.local/bin/glm-worker`へ配置
- Claudeのmanaged設定だけ`~/.claude/settings.json`へmerge
- Git clone上で実行した場合は`post-merge` hookを有効化

`rules/default.rules`、auth、sessions、SQLite、cache等の既存runtimeには触れない。
一方、過去に`install.sh`が配置した管理ファイルはmanifestで追跡し、リポジトリ側で削除・改名された場合は次回install時に旧ファイルを削除する。
バックアップは作成しない。

管理ファイルを変更する前に、`glm-worker`とJSON merge toolのtest/buildをpreflightとして実行する。preflight失敗時は管理ファイルを更新しない。
`IMPLEMENTATION_PLAN.local.md`をGit管理するrepositoryでは、preflight前にfinal HEADのplanがfinal HEAD postcondition(ACTIVE/NEXT/task fileのHEAD tree存在・Git境界・過渡表現なし)を満たしていることを検証し、失敗時は配置を行わない。
install完了後、既に開いているCodexタスクが`AGENTS.md`を再読込する保証はない。ルール反映を保証するには新しいCodexタスクを開始する。

## 2回目以降

```sh
git pull --ff-only
```

初回`install.sh`で設定した`post-merge`から、自動的に`install.sh`が実行される。

hookを使いたくない場合は:

```sh
git pull --ff-only
./install.sh
```

## Claude接続先の端末local override

端末ごとにClaude Codeの接続先・model設定だけをoverrideできる。同一repo/install.sh/Go sourceのままで、業務PCなどでAnthropic Claude Teamへ切替える用途を想定する。overrideはGit管理外で、installer共通動作・既存local fileには影響しない。

### 配置場所と形式

既定のpath:

```text
${XDG_CONFIG_HOME:-$HOME/.config}/codex-config/claude-settings.local.json
```

`CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE`でpathを変更できる。

GitHub repo名(`codex-worker-orchestrator`)とlocal設定namespace(`codex-config`)は意図的に異なる。repo名はrename可能だが、override path・env変数・sidecar・manifestは公開済みで各端末の既存状態を指すため、rename対象外として`codex-config`を恒久維持する。

形式はClaude settingsの`env`だけを対象としたJSON:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": null,
    "ANTHROPIC_DEFAULT_OPUS_MODEL": null,
    "ANTHROPIC_DEFAULT_SONNET_MODEL": null,
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": null
  }
}
```

- string値は追加・上書き
- `null`はunset(targetのenvから実際に削除し、親processや`~/.claude/settings.json`から再流入させない)
- 空文字は文字列値扱いでありunsetではない
- top-levelはobjectのみ、top-level keyは`env`のみ、env値はstringか`null`のみ。`null`(top-level)・`{"env":null}`・空object・`{"env":{}}`以外の壊れたJSON・未対応形式はinstall・runnerともfail closedになる。空objectと`{"env":{}}`は有効な空patch

### 業務PCでのClaude Team切替え例

Z.ai向けの`ANTHROPIC_BASE_URL`・`ANTHROPIC_DEFAULT_*_MODEL`を`null`で削除する。認証はClaude Code自身のOAuth等を使い、本機構はOAuth等の認証情報を読み出し・コピー・上書きしない。glm-workerのmodel alias(`opus`/`haiku`/`sonnet`)はそのままで、`ANTHROPIC_DEFAULT_*_MODEL`の削除によって実モデルへ解決される。

### 運用

overrideを追加・変更・削除したときは、必ず`install.sh`を再実行する。install.shはsettings.jsonと同じdirectory(`~/.claude/`)のGit管理外sidecar `.codex-config-claude-env-state.json` に、overrideが所有する各env keyの適用前baseline(schema version 1)を記録する。各installは、前回stateの全所有keyを元値または不存在へ復元→managed defaultsをdeep merge→今回overrideの全keyのbaselineをsnapshot→set/null patchを適用、の順に実行する。これによりoverrideから外したkeyやoverrideファイル削除後の再installで、managed Z.ai keyはdefault・override追加keyは不存在・上書きやnull削除した既存local keyは元値へ確実に戻る。overrideなしは現行mergeと同じ結果になる。stateは0600・atomic writeでOAuth等の認証情報には触れず、repoやinstaller manifest対象外。壊れたoverride・state・未対応形式はsettings/stateを書き換える前にfail closedする。override適用中にsidecarだけを単体削除してはならない。baselineが失われ、現在のsettings.json値が新たなbaselineとして固定され、以降のoverride解除で元値へ復元できなくなる。復旧にはsettings.jsonとsidecarを整合した既知状態へ一緒に復元するか、overrideが所有する各keyを手動で正しいbaselineへ戻すことが必要である。glm-worker起動時にも同じoverrideを読みchild envへset/deleteを再適用するが、stateは読まない。趣味PCなどoverride不要な端末では本ファイルを作成せず、既定のZ.ai接続を維持する。

## 構成

```text
codex-worker-orchestrator/
├── AGENTS.md                 # このリポジトリの作業bootstrap規則
├── install.sh
├── codex/
│   ├── AGENTS.md
│   ├── config-managed.toml
│   ├── instructions/
│   ├── rules/
│   │   └── glm-worker.rules
│   └── glm-worker/
│       └── prompts/
├── glm-worker/
│   ├── cmd/
│   │   └── glm-worker/       # CLI entrypoint
│   └── internal/
│       ├── app/              # 引数解析・ロック・出力
│       ├── config/           # 環境変数・リポジトリ設定
│       ├── packet/           # PACKET解析・検証
│       ├── runner/           # Claude Codeプロセス実行
│       ├── state/            # task・session・stats・resume状態
│       └── workflow/         # worker/reviewer状態機械
├── claude/
│   └── settings-managed.json
├── tools/
│   └── merge-json/
├── tests/
│   └── install_smoke.sh
└── .githooks/
    └── post-merge
```

`cmd/glm-worker`は薄いentrypointとし、外部公開しない実装は`internal`配下へ置く。
package間の依存は`app`から各機能へ向け、状態永続化とworkflowを分離する。


## glm-worker CLI

```sh
glm-worker "<新規タスク>"
glm-worker --decision-stdin <判断本文のUTF-8 byte数> [--sha256 <sha256 hex>]
glm-worker --fix-stdin <指示本文のUTF-8 byte数> [--sha256 <sha256 hex>] [--origin codex-review|glm-reviewer|user-amendment|external-review|metadata-repair]
glm-worker --accept
glm-worker --resume
glm-worker --status
glm-worker --watch "[--verbose]"
glm-worker --timeline "[task ID]"
glm-worker --convergence "[task ID]"
glm-worker --stats
glm-worker --reset
glm-worker --eval-ab "<A/B run dir>"
```

- `--decision-stdin`は`NEEDS_SOL_DECISION`で停止した同一タスクを継続する。
- `--decision-stdin`・`--fix-stdin`は本文をstdinから宣言byte数だけ読み取り、同じ継続・差戻し経路へ渡す。stdinがTTY/PTYのときはglm-worker自身が読み取り前にraw/noecho相当へ切り替え読み取り後に元のterminal stateへ復元するため、caller側の`stty`等の事前設定は不要。切り替え成功直後にREADY control event行(`{"type":"control","event":"stdin_ready"}`)をstderrへ1回だけ出すため、callerはこのevent行を確認した直後に本文を1回だけ書く。event未観測・event行の重複・process先行終了では本文を送らずfail closedとする。control event行はtransport controlであり受理対象のtask本文へ含めない。stdinがpipe/fileのときはtermiosへ触れずeventも出さない。読み取り不足・`--sha256`不一致はstate変更・model呼出前にfail closedする。長文decision/fix本文の伝達はshell quotingを通さないこの経路を使い、廃止したargv埋込み`--decision`/`--fix`はusage errorへfail closedする。
- `--fix-stdin`は`NEEDS_SOL_REVIEW`後だけ利用できる。`--origin`は差戻し元の申告で、有限集合`codex-review`(親Codexがterminal packet受領後の最終reviewで新たに検出した差戻し)・`glm-reviewer`(GLM reviewerのterminal resultに既に記載された指摘を親が差し戻す場合)・`user-amendment`(user修正要求)・`external-review`(repo外review)・`metadata-repair`(parent管理metadata修復)のどれかを`--fix-stdin`に先行または対で渡す。集合外値はusage errorへfail closedし、未申告の`--fix-stdin`単独は新規検出かreviewer既記載か確定できない場合だけが該当し、推定せず`unknown` originとして計上して`codex-review`へ倒さない。
- `--accept`はparent Codexがterminal packet(PASS・NEEDS_SOL_REVIEW等)を採用したときの観測記録専用commandで、model呼出・stateの実行状態変更・Git操作を行わない。対応するopportunityがopenしているときだけ`accepted` outcomeを1回だけ確定し、未open・再実行はno-op、`NEEDS_SOL_DECISION`待ちへの適用はfail closedする。
- `--resume`はZ.ai 5時間上限またはprovider一時障害で停止した同一phase・session・checkpointを再開する。
- `--status`と`--stats`は参照専用、`--reset`は現在の統計をarchiveして実行状態を消去する。
- `--watch`は現在taskの受動event log(`events/<task ID>.jsonl`)を保存済み内容のread・tailだけで表示する参照専用command。provider/workerへの問い合わせ・AI呼出・repo lock・state書換を行わない。event logの保存済みJSONL行をそのまま流した後追記をfollowするJSONL streamで、開始時に`watch_start` control eventを出す。follow対象taskのauthoritative `task.status`が`active`を離れた時点(`waiting-decision`・`waiting-sol-review`・`complete`・`rate-limited`・`provider-unavailable`)・別taskへの切替時に残eventを流して`watch_exit` control event(`task_id`・`status`、task切替時は`new_task_id`付き)を出力しexit 0する。event log file不在時だけ`event_log_status` control event(`{"type":"event_log_status","status":"removed"}`、初回open時は`watch_start`の`event_log_status: empty`)で正常終了する。permission等のfile不在以外のI/O失敗は正常状態へ偽装せず、stderrのprocess error JSON(`kind:"internal"`)とnon-zero exitで失敗する。`--verbose`を併用したときだけlive tool状態を型付き`live` eventへ流す。task全体の経過時間・最後のmodel activityからの経過・現在実行中tool名とその経過・Bashなら実行command・tool入力のdescription/purpose・background task待機状態・直前に完了した長時間toolの種類と所要時間・直近のtool errorを、表示中のevent logとrunnerがlive受信stream eventから組み立てた瞬間snapshot(`events/<task ID>.live.json`)から定期的に再表示する。command等の長い本文はこの表示時だけtruncateし、event logのschema・retention・本文非保存方針は変更しない。Claude Code内部session JSONLは参照しない。
- `--timeline [task-id]`は保存済みevent logとtelemetryだけからtask/call単位のtimeline・tool種別別集計・session agingを表示する参照専用command。AI呼出・repo lock・state書換を行わない。task ID省略時は現在task、明示指定時はretention内の旧taskも読める。
- `--convergence [task-id]`は保存済みround log・telemetry・event logだけからreview/fix convergenceをround単位で表示する参照専用command。AI呼出・repo lock・state書換を行わない。task ID省略時は現在task、明示指定時はretention内の旧taskも読める。
- `--eval-ab`はCodex Direct対orchestratedのA/B比較run dir(spec.json・direct.json・orchestrated.json)を検証して比較結果を表示する参照専用commandで、AI呼出は行わない。orchestrated記録のGLM usageは当該taskのstats履歴から解決するため、orchestrated run側またはそのstate履歴を持つcheckoutで実行する。

reviewer呼出しの前後でGit状態を3軸(HEAD・index・worktree/untracked)のdigestで固定・検証する。worker終了時とreviewer開始前、5h上限・provider障害からのresume前、そして各reviewer model callが正常終了した直後かつPASS/FIX_REQUIRED/NEEDS_SOL_REVIEW等を採用する前に、保存snapshotと現在状態を同じ3軸で比較する。reviewerがEdit/Write禁止でもBash・formatter・test・generator等でrepositoryを変更していた場合はreview結果を採用せず、rollbackも黙認もせず`NEEDS_SOL_REVIEW`/`HIGH`へfail closedする。追加のmodel呼出・reviewer層の変更は行わない。

`targets: ["PACKET"]`の報告再出力(report-only)workerはEdit/Write等を禁じたReadOnly capabilityでdispatchし、開始直前に保存した3軸snapshotを基準として終了後の同一性を確認するまで通常reviewへ進まない。基準snapshotの取得・保存に失敗した場合はreport-only workerを実行せず、実行前後で1軸でも変化した場合・終了後照合に必要な基準snapshotが読めない場合はreviewerを呼ばず、いずれも変更をrollbackも黙認もせず`NEEDS_SOL_REVIEW`/`HIGH`へfail closedする。5h上限・provider一時障害・resumeを跨いでも同じ開始前snapshotを基準に使い、resume時に新baselineを取り直して変化を隠さない。通常のimplementation auto-fix・explicit fix・decision経路のsnapshot semanticsは変更しない。report-only判定はcheckpointの`report_only` fieldだけを信用し、version 4以降のwriterは`report_only`をfalseでも明示保存する。resume checkpointは現version(version 4)だけを受理し、旧version checkpoint(`report_only`欠落のv3以下を含む)のupgrade・phase等からの推定は行わず、routing前にresume不能として明示終了する。v4も`report_only` keyの明示存在とbool型を検証し、key欠落・非bool値をzero value falseの通常auto-fixとして受理せず同じくfail closedする。fail closedが清除するのはresume checkpointだけで、開始前snapshot・comparison・telemetry・worker session等の診断証拠は`--fix-stdin` recoveryでの調査のために残す。

主な環境変数:

| 変数 | 既定値 | 用途 |
|---|---|---|
| `GLM_WORKER_HOME` | `~/.glm-worker` | task・session・statsの保存先 |
| `GLM_WORKER_PROMPT_DIR` | `~/.codex/glm-worker/prompts` | worker/reviewer prompt |
| `GLM_WORKER_CLAUDE_BIN` | `claude` | Claude Code実行ファイル |
| `GLM_WORKER_WORKER_MODEL` | `opus` | worker model alias |
| `GLM_WORKER_REVIEWER_MODEL` | `haiku` | 通常reviewer model alias |
| `GLM_WORKER_HIGH_RISK_REVIEWER_MODEL` | `sonnet` | 高リスク・Sol判断後・修正後reviewer model alias |
| `GLM_WORKER_EFFORT` | `high` | 通常実行effort |
| `GLM_WORKER_ESCALATED_EFFORT` | `max` | Sol判断後・明示fixのeffort |
| `GLM_WORKER_MAX_AUTO_FIX_ROUNDS` | `2` | 自動修正の上限回数 |
| `GLM_WORKER_TELEMETRY_CONTENT` | `true` | 呼出ログへsystem/dynamic promptと最終response本文を保存するか |

リポジトリごとの状態は`$GLM_WORKER_HOME/sessions/<repo SHA-256>/`へ保存する。
`task.status`を正規状態とし、`task-stats.json`は観測用mirrorとして扱う。
呼出単位の詳細は`telemetry/<task ID>.jsonl`へ`0600`で保存する。stats・telemetryの破損や書き込み失敗はwarningを出してworkflowを継続し、明示的な`--stats`だけはstats読み込みエラーを返す。


## 複数リポジトリの並列利用と共有資源

異なるrepositoryでglm-workerを同時に利用できることを通常contractとする
(`Codex A → PTY A → glm-worker A`と`Codex B → PTY B → glm-worker B`の並列)。全体を
直列化するglobal lock・daemon・socket・scheduler・queue・coordinatorは持たない。

repository分離の設計:

- state dir・repo lockは`GLM_WORKER_HOME/sessions/<repo SHA-256>/`単位
- session・checkpoint・telemetry・event log・artifactはすべて当該repoのstate dir配下
- model子processのcwdは当該repo root
- repo-search cacheは`GLM_WORKER_HOME/search/<repo canonical path SHA-256>/`単位
- task ID・session IDはrepo間で混入しない
- 同一repoの2本目の起動だけflock拒否され、他repoの起動・実行はblockしない

共有資源の分類(concrete collision evidenceがない限り直列化を追加しない):

| 資源 | 分類 | 根拠 |
|---|---|---|
| `GLM_WORKER_HOME` | repo/task namespace済み | `sessions/`・`search/`配下はrepo hash単位で分離。dir自体は共有するがglm-workerが書く内容は全てnamespace内 |
| prompt dir(`GLM_WORKER_PROMPT_DIR`) | read-only shared | runnerが`WORKER.md`/`REVIEWER.md`を読むだけ。glm-workerは書かない |
| Claude config dir(`CLAUDE_CONFIG_DIR`) | read-only shared(glm-workerから)。配下のsession state書込みはupstream管理 | glm-workerはsettings.jsonのenv allowlist読取とexclude path解決だけ。config dir配下へのsession書込みはClaude CLI自身がsession ID単位で行う |
| Claude settings override(`CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE`) | read-only shared | env set/deleteのpatchを読むだけ。書き込みは`install.sh`/`tools/merge-json`側 |
| Codex automation TOML/SQLite(`CODEX_CONFIG_DIR`) | read-only shared | `--verify-auto-resume`がTOML読取とSQLite読取(`sqlite3` CLIのSELECT)だけ行う。automationの作成・更新は親Codexの責務 |
| provider/Z.ai quota | upstream管理・repo stateとは分離 | account単位の上限。同一provider quotaを2 repoが消費すること自体はbugではなく、rate-limit判定・停止・resumeは各repoのstateだけへ反映される |
| temp dir(TMPDIR) | per-process | 実行ごとに`os.MkdirTemp`の一意dir(`glm-worker-*`)を作り、終了時に削除 |
| install済みglm-worker binary | read-only shared | 全repoが同じbinaryを実行する。実行中の上書きはinstallerの適用契約(`install.sh`)が管理 |

このcontractは`glm-worker/internal/app/multirepo_process_test.go`(独立2 Git repositoryでの
実binary並列実行・lock意味・state/session/checkpoint/telemetry/event log非混入・reset/resume/
rate-limit recovery/status非干渉。実claudeの代わりの動作固定stubで追加AI callなし)、
`multirepo_reposearch_test.go`(共有cache rootでのrepo-search cache分離を独立processで)、
`multirepo_pty_test.go`(2実PTYのmode・payload非干渉)で固定する。


## 自己保護 (self-protection)

glm-workerはこの配布repo自身を作業対象にした変更について、wrapper側のcritical surface判定で実効riskをHIGHへ固定し、workerのLOW自己申告やreviewerのPASSだけでは完結させずSol確認へ昇格する。判定はfile種別ではなく「委譲・model routing・prompt/instruction・PACKET・session/resume・provider recovery/autoresume・権限/隔離・managed settings/installer適用意味を変更できるproduction surfaceか」の意味で行う。

HIGH対象:

- `glm-worker/internal/`配下のproduction `.go`(package既知・未知を問わず既定。将来のinternal package追加もfail-openしない。観測専用の`state/stats.go`・`state/telemetry.go`のみ除外)
- `glm-worker/cmd/`配下のproduction `.go`(CLI entrypoint。現状薄くてもCLI routing・app/workflow gate呼出を直接変更できる境界)
- installer適用経路: `install.sh`、`.githooks/post-merge`、`tools/merge-json/`のmerge engine
- 管理settings内容: `claude/settings-managed.json`(model routing・provider接続)、`codex/config-managed.toml`(実行envelope)
- `glm-worker/go.mod`・`tools/merge-json/go.mod`(production binaryの依存境界)
- `glm-worker/scenarios/`、`codex/instructions/`、`codex/rules/`、`codex/glm-worker/prompts/`、`codex/AGENTS.md`、root `AGENTS.md`

非対象(通常のLOW reviewのまま): test file(`*_test.go`)、`tests/`・`glm-worker/scripts/`配下の検証harness、`README.md`・`EVAL.md`等docs、観測専用file。docs/testだけ・観測値だけの変更をHIGHにしない。

判定契約の唯一の正は`glm-worker/internal/workflow/selfprotection.go`であり、production判定とtest/scenario corpusは同じ契約を参照する。repoの全tracked fileがcritical・非対象いずれかの分類を持つことをunit testが強制し、将来fileを分類なしで追加するとtestが失敗して意味判断を求める。行動固定はscenario corpus(`orchestrator-critical-low-self-declare`・`repo-agents-root-change-escalates-self-protection-high`・`install-merge-path-escalates-self-protection-high`・`managed-settings-content-escalates-self-protection-high`・`autoresume-verifier-escalates-self-protection-high`・`future-internal-package-escalates-self-protection-high`・`cmd-entrypoint-escalates-self-protection-high`・`test-and-docs-only-stay-low-risk`)による。


## Z.ai 5時間上限からの再開

次のZ.ai実エラーを5時間上限として判定する。

```text
API Error: Request rejected (429) · [1308][Usage limit reached for 5 hour. Your limit will reset at YYYY-MM-DD HH:MM:SS][...]
```

genericな429だけでは5時間上限と判定しない。

停止時:

停止時はstderrへerror JSON 1行を出してexit 1する(`--status`の`rate_limited`・`resume_available`でも同じ状態を読める)。

```json
{"error":{"kind":"rate_limited","message":"Z.ai Coding Plan 5h limit reached; task is stopped and resumable","detail":{"limit":"ZAI_GLM_CODING_PLAN_5H","phase":"...","task_id":"...","repo_root":"...","reset_at_cst":"YYYY-MM-DD HH:MM:SS","reset_at_rfc3339":"YYYY-MM-DDTHH:MM:SS+08:00","auto_resume_available":true,"auto_resume_at_rfc3339":"YYYY-MM-DDTHH:MM:SS+08:00","auto_resume_key":"glm-worker-resume-...","resume_available":true}}}
```

枠回復後:

```sh
glm-worker --resume
```

同じworker/reviewer sessionと保存済みphaseから再開する。

Codex appでthread heartbeat automationを利用できる場合は、reset時刻の2分後に現在のCodexタスクを自動でwakeする。
wake時は同じローカルcheckoutでtask IDと`rate-limited`状態を照合してから`glm-worker --resume`を実行する。
別worktree、reset済みtask、task IDが変わった状態では再開しない。再度rate limitになった場合は同じautomationを新しい時刻へ更新する。
automation時刻はRFC3339のoffsetを保持してUTCへ変換する。heartbeat schedulerは`TZID`を`next_run_at`計算へ反映しないため、`DTSTART;TZID=Asia/Tokyo`は使わず、UTCの壁時計値を1回限りの`DTSTART`へ設定する。既存automationは同一IDへ直接updateする。新規作成はDTSTART付き即時createがCodex appに拒否されるため、DTSTARTなし・PAUSED・常にfuture occurrenceを持つplaceholderを作成して成功応答から正確なIDを得て、同一IDを目的の絶対時刻DTSTARTと`COUNT=1`へupdateしてACTIVE化する二段階作成とする。update失敗時はplaceholderをbest-effort削除し、最終verify失敗もfail closedとする。toolの成功応答だけで完了扱いせず、SQLiteの`automations.next_run_at`またはCodex app上の次回実行時刻が意図したJST時刻と一致することを確認する。


## provider一時障害からの回復

Z.ai 5時間上限とは別に、応答本文中の`502`/`503`/`504`/`529`と明確な一時network障害(connection refused/reset、i/o timeout、dial tcp失敗等)だけを一時provider障害として分類する。分類は共通入口で排他的に行い、Z.ai 5h上限signal(429/1308/Usage limit reached)を先に判定したうえでtransient信号を見る。よってauth(401/403)・invalid request(400)・session破損・不明errorは従来どおり`WORKER_ERROR`、genericな429はZ.ai 5h固有signalがなければ非transientの`WORKER_ERROR`、5h上限signalのみ`RATE_LIMITED`で、いずれもここへは入らない。providerの公開status page等の外部情報は回復判定の根拠に使わず、補助情報に留める。

一時障害時は元taskのrole/phase/model/session/checkpoint/Git snapshotを保持したまま同一glm-worker process内で上限付きbackoffを行う。各待機後にrepo・working tree・元依頼を読ませずtoolを許可せずsessionを作成・保存せずreasoningさせない最小疎通probeを同一endpoint・対象modelへ1回だけ送り(`--safe-mode`・setting sources空・empty MCP・env隔離を維持)、成功時だけ保存済み本taskを同一sessionで1回resumeする。probeはexit 0かつ結果JSONが正常・`is_error=false`・応答本文がtrim後固定sentinel `GLM_WORKER_PROBE_OK`と完全一致・model usageが出力tokenを含むときだけ成功と認める。この固定疎通確認の契約を通らない応答は正常probeと認めずsaved taskのresumeへ進めないが、semantic invalidだけで即fatalにはせず、502/503/504/529や明確な一時network errorと同じ通常のprobe失敗として既存backoff/retryを継続する。ただし応答本文中の明示的なauth/config信号(401 Unauthorized/403 Forbidden/400 Bad Requestの組合せ、HTTP/status/API error文脈付きの同status、authentication failed/required・invalid api key・invalid model等の明示表現)はsemantic invalidと区別し、5h・transient信号より後の優先度で既存fatal経路へ本task再開も追加probeもせず`WORKER_ERROR`へfail closedする。裸のstatus数字や一般語だけではfatal判定せず、503等のtransient信号との混在応答はtransientとして扱う。probe上限4回・hard deadline約3時間の先に到達した側で`provider-unavailable`へ保存し、応答契約違反が継続していた場合はclassification値`probe-contract`で区別する。短周期pollingやCodex heartbeatによる途中wake、新task/sessionでの再実行は行わない。

deadline/回数上限に到達すると、`WORKER_ERROR`や`RATE_LIMITED`とは独立した`provider-unavailable`の再開可能task状態とcheckpointを保存する(5h上限のような自動wakeは設定しない)。backoff・resume・probe応答のいずれかでZ.ai 5h上限signalを検出した場合は5h優先でrate-limited checkpoint/statusへ移行し、provider-unavailable状態と矛盾する保存を行わない。

停止時はstderrへerror JSON 1行を出してexit 1する(`--status`の`provider_unavailable`・`resume_available`でも同じ状態を読める)。

```json
{"error":{"kind":"provider_unavailable","message":"provider stayed unavailable after probe budget; task is stopped and resumable","detail":{"phase":"...","classification":"http-503","probes":4,"elapsed_ms":10800000,"task_id":"...","repo_root":"...","resume_available":true}}}
```

回復後:

```sh
glm-worker --resume
```

同じtask/session/checkpointから再試行する。


## GLM実行の軽量化

- worker: `opus` alias → `glm-5.3`
- 通常reviewer: `haiku` alias → `glm-4.7`
- `"risk":"HIGH"`、Sol判断後、自動修正後、明示fix後のreviewer: `sonnet` alias → `glm-5.3`
- reviewerは4.7と5.3を直列実行せず、worker packetと自動修正履歴から一方だけを選ぶ。
- reviewerはAgent/subagentへ委譲せず、選択されたreviewerモデル自身で確認する。
- 選択したmodel aliasはresume checkpointへ保存し、5時間上限後も同じモデルで再開する。
- resume checkpointはversion 4でmodelを必須とし、`report_only`をfalseでも明示保存する。v4読込は`report_only` keyの存在とbool型を検証し、key欠落・非bool値はfail closedとする。旧versionの自動移行やroleからのmodel推定は行わない。
- 通常worker/reviewer/自動fix: effort `high`
- Sol判断後の継続とSolからの明示fix: effort `max`
- auto-compact window: 500K
- Claude Code sessionはリポジトリ永久ではなくタスク単位。新規タスク開始時にworker/reviewer session IDを更新する。
- 同一タスク内の`--decision-stdin`、自動fix、Z.ai 5h limit後の`--resume`ではsessionを維持する。
- `--fix-stdin`は`NEEDS_SOL_REVIEW`後だけ使用できる。`PASS`後の追加依頼は新規タスクとして開始し、worker/reviewer sessionを更新する。
- worker/reviewer呼出にはrole別のtyped JSON schemaを`--json-schema`として渡し、結果はresult eventの`structured_output`だけを権威として受理する。status・risk enum・必須field・型はschemaが強制し、STATUS別必須field・RISK整合性・artifact実在などの意味契約はwrapperの意味検証が強制する。結果は6 KiB・1 field 1536 bytes以内。意味検証不合格時は同じsessionへ作業をやり直さない結果の修正再出力を1回だけ要求し、schema違反・`structured_output`欠損・retry枯渇は修正再依頼なしにfail closedする。受理結果のmachine protocolは、status別契約fieldだけを含むcompact 1行JSON(schema語彙と同じkey・HTML escapeなし・空fieldと空配列のkey省略)で、最終stdout・次のmodel呼出のprompt埋め込み・state保存の全機械経路が同じ1行を使う。旧`KEY: value`行表示は人間向けdiagnostic projectionとしてのみ残る。
- packetへ収まらない正確な一覧・監査報告・生成物だけをtask別`ARTIFACT_DIR`へ保存し、worker packetから最終reviewer packetまで`artifacts`の絶対パスだけを引き継ぐ。`artifacts`は空配列(machine JSONではkey省略)またはtask専用dir配下の実在通常ファイルに限定し、machine protocolではJSON配列として渡す。artifactはstate配下でディレクトリ`0700`・通常ファイル`0600`に揃え、symlinkと特殊ファイルを拒否する。
- worker errorの診断tailは最大6 KiBに制限し、Codexへ不要な大量ログを返さない。

## タスク状態と統計

```sh
glm-worker --status
glm-worker --watch
glm-worker --timeline "[task ID]"
glm-worker --convergence "[task ID]"
glm-worker --stats
```

`--status`はmachine JSON 1行を出す。task ID(`task_id`)・task status(`task_status`)・task別artifact保存先(`artifact_dir`)・session(`worker_session`/`reviewer_session`)・判断待ち(`pending_decision`/`parent_review_open`)・rate limit状態(`rate_limited`)・provider-unavailable状態(原因分類・試行数・経過を`provider_unavailable`)・再開可否(`resume_available`)に加え、対象repositoryのlock実保持(`repository_lock`: held/free、probe不能時はnull)と`task_liveness`を出す。`task_liveness`は`task_status`が`active`のときだけrunning/staleへ値が出て、非active時・probe不能時はnullへ統一する。観測できない値はnull/omitとし、`unknown`・`none`のようなpresentation文字列へ落とさない。task不在時の`task_status`もnullである。`task_status`の受理集合は`active`・`waiting-decision`・`waiting-sol-review`・`complete`・`rate-limited`・`provider-unavailable`の6値とnullだけであり、永続state上の未知値・破損値は未観測としてnullへ正規化する。lock保持判定はflock実取得の非破壊probeであり、lock file内のPIDは診断情報(`lock_pid`)としてのみ扱う。GLM workerの生存判定・重複起動待避は対象repoのlockだけを根拠にし、別repoのprocess一覧や`pgrep`は使わない。`active`+`repository_lock: free`はstale候補としてrepo固有の復旧へ導く。
`--watch`は現在taskのevent log保存済みJSONL行をそのままpassthroughし、以降の追記をlocal tailするJSONL streamである。event recordはmetadata(時刻・phase・role・種別・tool名とbyte数・token・result観測値)だけを含み、thinking等の本文は保存対象外のため流れない。watch固有の状態遷移は型付きcontrol event(`watch_start`・`event_log_status`・`watch_exit`)で出し、event logの読取不能行は`event_skipped` control eventで報告する。followはfollow対象taskのauthoritative `task.status`が`active`を離れた時点・別taskへの切替時に残eventを流して`watch_exit` control eventを出力し終了する。event log file不在(ENOENT)時だけ`event_log_status` control event(status=removed)での正常終了と初回openの`watch_start` `event_log_status: empty`になり、permission等のfile不在以外のI/O失敗は正常状態へ偽装せずerrorとしてprocess境界のstderr process error JSON(`kind:"internal"`)とnon-zero exitへ流す。event logへの書込みは必ず`task.status`が`active`の間に行われ、non-activeへの遷移は当該呼出の最終event追記より後に行われるため、watchはstateを読んだ後にeventをdrainし、終端eventを取りこぼさない。それ以前の中断はCtrl-Cでよい。`--watch --verbose`はこのstreamに加えて型付き`live` eventでlive tool観測を出す。task全体の経過時間(`task_age_ms`)・最後のmodel activityからの経過(`model_idle_ms`)・現在実行中tool(`current`: tool名・経過・Bashなら実行command・tool入力のdescription/purpose)・background task待機(current toolの`background`・`wait_task_id`)・直前に完了した長時間tool(`last`: 種類と所要時間)・直近のtool error(`tool_error`)を、tool状態はevent logから、command等の詳細はrunnerがlive受信したstream eventからtool入力の表示要素だけを書いた瞬間snapshot(`events/<task ID>.live.json`)から組み立て、定期またはtool状態変化時に出す。command等の長い本文はこのlive eventへ載せる時だけtruncateする。model activityの受理集合はrunner(producer)とwatch(consumer)の共有契約で、assistant側のthinking・text・tool_use blockと、event logへは保存しない高頻度抑止対象の`system/thinking_tokens`だけとする。`model_idle_ms`基準はgenericな最終event観測時刻(`last_event_at`)とは別にsnapshotへ置くmodel activity専用時刻(`last_model_activity_at`)とevent log側のassistant観測時刻の新しい方だけから組み立てるため、`system/tool_progress`・task notification・user tool result・background通知・resultでは増え続ける経過が止まらない。専用時刻を持たない旧snapshotではevent log側のassistant観測時刻へ落ち、migrationや意味推定は行わない。event logへcommand・thinking・tool入出力本文を保存しない方針とClaude Code内部session JSONL非依存は変わらない。
`--timeline`は追加AI callなしでtask/call単位のtimelineをmachine JSON 1行で出す。callごとにrole・phase・sessionとsession内call番号・resumed別・model alias・message model(実model)・観測窓(最初と最終eventの時刻・span・event数)・result観測値(duration・API duration・turns・token・cost)・tool種別別の呼出数とID対応付けできた測定済みduration(合計・最大)を出す。対応付け不能なtool durationは`unmeasured`計数、result event未観測のcallは`result.observed=false`として推測せず、tool名を持たないblockは`unknown` tool種別へ集計する。task単位ではtool種別別合計(`tool_totals`)とtelemetry由来のsession aging(`session_aging`)を続けて出す。相対barのようなgraph表示は持たない。`task_status`は`--status`と同じ6値とnullの受理集合へ出し、現在task・明示指定task・stats履歴archiveのいずれかで観測できない値・受理集合外の永続値はnullへ正規化する。event logの破損行は`skipped_events`計数として報告し、event logがない現在taskは`event_log.status=none`として正常終了する。event log・telemetry path構築へ使うtask ID(明示引数・現在task両方)はtask採番のUUID v4生成形式(長さ・hyphen位置・小写hex・version・variant)へ検証し、不正形式(相対path・絶対path・非v4等)はfilesystemへ触れずerrorを返す。明示指定task IDのlog不在・読込失敗もerrorを返す。
`--convergence`は追加AI callなしでreview/fix convergenceをround単位で表示する。各task開始時(worker実行前)と各review round開始境界でGit snapshot 3軸digestと変更対象pathの観測(全内容digest・空白行/行末空白/full-line comment除去後の意味digest)をtask単位round log(`rounds/<task ID>.jsonl`)へ`0600`でbest-effort記録し、追記・観測失敗はwarning・CaptureErrorだけでworkflowを止めない。表示はround log・telemetry・event logだけから組み立て、roundごとにreview番号・auto-fix回数・生成worker phase・snapshot digest・前境界に対する差分分類・対応付けたreviewer/worker呼出の呼出数・token・turn・duration・packet結果・risk・risk floor再出力の有無を出す。差分分類は`same-snapshot`(event logで当該worker呼出にtool利用が観測されfile変更toolが無いときは`verification-only`へ細分化)・`comment-format-only`・`doc-change`・`semantic-change`・機械確定不能の`unknown`で、文書fileの追加・変更・削除は内容の意味を機械確定しないためfile種別だけで非意味へ分類せず全て`doc-change`として観測する(AGENTS・instructions・EVAL・仕様等の行動規定文書もここへ出るため省略候補から除外される)。raw string・heredoc・triple quote・行継続を含む内容やshell/yaml等の正規化非対応形式は安全側の`semantic-change`候補として扱う。reviewer/worker呼出の対応付けはround境界の時刻とphaseで行い、seq不連続・reviewer番号不一致でrecord欠落が疑われるroundは分類を`unknown`へ倒して`gap`/`mismatched_reviewer`を出す。task単位では差分分類別のreviewer呼出数・token・duration合計(`summary.by_class`)と未解決issue round・HIGH round件数を出す。round logの破損行は`skipped_rounds`計数として報告し、round logがない現在taskは`rounds_log.status=none`として正常終了する。`task_status`は`--status`と同じ6値とnullの受理集合へ出す。task ID検証は`--timeline`と同じUUID v4生成形式境界を使い、明示指定task IDのlog不在・読込失敗はerrorを返す。分類のための追加model呼出・reviewer呼出の削減・model routing変更は行わない。
`--stats`は通常のworker packetへ混ぜず、完了済みと現在のタスクを集計して次を表示する。machine JSON出力の全map集計fieldは0件でも空object `{}`として出てJSON nullにはならず、`current_task.status`はtask不在時nullで、`--status`の`task_status`と同じ6値とnullの受理集合へ未知永続値を正規化する。

- worker/reviewerとmodel alias別の呼び出し回数・実行時間・turn数
- alias別の呼出しツリー全体、およびClaude CLIが報告した実モデル別のinput、cache creation、cache read、output token
- Sol判断・明示fix・resume・自動fixの回数
- `NEEDS_SOL_DECISION`、`NEEDS_SOL_REVIEW`、`PASS`の件数
- model alias別rate limit、結果の意味修正再依頼、Solへ返したpacket bytes
- model alias別provider-unavailable件数
- risk floor件数(category別)、snapshot mismatch件数(軸別)、packet reject件数(reason別)、probe成功失敗
- probe呼出数(`probe_calls`)、total AI call数(`total_ai_calls` = task呼出+probe呼出)、transient retry数(`transient_retries`)
- parent review outcome(`parent_outcomes`: accepted・fix・decision・unknown)とfix origin(`parent_fix_origins`: codex-review・glm-reviewer・user-amendment・external-review・metadata-repair・unknown)、outcomeのmodel alias別(`parent_outcomes_by_model`)・risk別(`parent_outcomes_by_risk`)内訳
- fix outcomeの差し戻し後rework(`parent_fix_rework`: origin別の呼出数・worker/reviewer呼出数・turn数・tree token・wall時間)とcoverage(`parent_fix_rework_coverage`)
- 現在taskのartifact保存先

新規タスク開始時に前タスクの統計をarchiveし、`--reset`時も現在値を破棄せずarchiveする。
parent review観測はglm-worker側で観測できたparent操作の記録にすぎず、Codex本体のactual token usageでもDirect/orchestratedのA/B比較metricでもない。この限定は`--stats`出力の集計対象がglm-worker側で観測できたparent操作記録だけである点から常時成り立つ。reviewer省略判断・model downgrade判断の根拠としてこの観測を単独では使わない。outcome確定境界はparentの明示操作(`--accept`・`--fix-stdin --origin`・`--decision-stdin`)とtask closeだけに限り、未観測のterminal packetをacceptedへ推定しない。各terminal packetはopportunityとしてopenし、outcome確定かtask close(unknown)で閉じるため、新binaryで実行したtaskでは`PASS`+`NEEDS_SOL_REVIEW`+`NEEDS_SOL_DECISION` packet総数とresolved outcome+open opportunityの総数が一致する。旧binary時代のarchiveはparent outcomeを持たず、補完も書き換えも行わない。
`--stats`の`telemetry_dir`配下には、各呼出しのphase、role、alias、実モデル、effort、session、prompt、最終response、top-level usage、subagentを含むtree usage、所要時間、結果をJSONLで保持する。alias別token集計にはtree usageを用い、top-level turn数は別名で表示する。promptとresponse本文を保存したくない環境では`GLM_WORKER_TELEMETRY_CONTENT=false`を指定し、byte数とSHA-256、usageだけを残す。`sol_packet_bytes` fieldは「親Solへ実際にemitした受理結果payloadのbyte数」の累積というformat非依存metricで、受理結果protocolが旧KEY行表示からmachine JSON 1行へ変わっても実際の新payloadを計る。両形式の値をまたぐ縦断比較ではprotocol切替commit境界を区別すること。
Task Work Call(worker/reviewerの本task呼出。transient障害からの本task再開実行を含む)とProvider Probe Callは`call_type`(task/probe/event)で区別する。task call数・実行時間・token/cost集計へprobeを混ぜず、probeは呼出数を`probe_outcome`/`probe_calls`へ別計上する。probeもClaude CLIが返すinput/output/cache token・cost・resolved model・API/wall durationをJSONL telemetryへ記録し、取得不能値は未観測(零値)のまま推測しない。
statsとtelemetryのschemaはversion 3で、top-level集計だったversion 1に加え、model_callsへprobeを混ぜていたversion 2 statsとcall_typeを持たないversion 2 telemetryも`--stats`とtelemetry読込から除外する。旧値の移行・書き換え・混在は行わない。versionは既存fieldの意味やJSON名を変更するときだけ上げ、上げ時は旧version recordをfail-closedで読み飛ばす。新fieldのomitempty追加は後方互換のためversionを維持し、旧recordでの新field欠落は「未観測/not captured」(0件・一致・LOW等の意味値とは区別)として扱う。telemetry各recordはworker/reviewer報告risk、実効risk、risk floor source/category、worker_end/review_start/review_endのGit snapshot digest(HEAD・index・worktree。生diffやfile内容は保存しない)、snapshot mismatch軸、packet reject理由、provider障害分類、probe/retry試行と経過時間、resume source(rate-limit/provider-unavailable)を同じ呼出へ紐付けて記録し、`--stats`はrisk floor・snapshot mismatch・packet reject・probe outcomeの少数集計を表示する。
artifactはtask更新や`--reset`後もtelemetryと同様にtask ID別で保持する。不要になった成果物の削除は自動化しない。
round logのrecordはversion 1で、task ID・seq・review番号・auto-fix回数・worker phase・観測時刻・snapshot 3軸digest・変更対象pathの分類とdigestだけのmetadataであり、file内容・diff本文は保存しない。旧version行・破損行は読み込み側でskipする。round logもtelemetry・artifactと同じtask ID別の保存境界とし、新たな自動削除・保持期間は設けない。

worker/reviewer呼出は`--output-format stream-json`で実行し、実行中に返る受動eventを追加のprompt/model callなしでtask単位event log(`events/<task ID>.jsonl`)へ`0600`で追記する。recordはversion 1で、task・call・session・role・phase・model alias・resume別・call内seq・時刻・種別(system/assistant/user/result)・tool名とblock byte数・message単位token・result観測値(duration・turns・cost)だけのmetadataであり、assistant/tool本文・prompt・response・thinking/reasoning本文・秘密情報は保存しない。child stdoutはこの縮約だけが受け、非result eventのraw本文はevent log・一時file・最終output・error診断・telemetryのいずれにも書き出さない。最終result event行だけboundedな内部表現へ保持して解析し、result eventがない・壊れている場合はstderrと構造summary(解析error・subtype・is_error)だけの診断へ落とす。summaryへevent数等の任意数値は含めず、transient HTTP status signature(`502|503|504|529`)への誤一致を防ぐ。JSON eventとして解釈できないplain stdout行だけを旧raw出力と同じprovider分類入力とし、5h上限・transientの分類構造値だけをworkflowへ渡してraw本文は保存しない。event logの追記・読込失敗はwarningだけでworkflowを止めず、部分破損行はその行だけskipして以後の追記・表示へ波及させない。最終result eventは`--output-format json`と同一schemaのためPACKET/session/resume/recovery semanticsは変わらない。event logもtelemetry・artifactと同じtask ID別の保存境界とし、新たな自動削除・保持期間は設けない。


## 開発時の検証

```sh
cd glm-worker
go test ./...
go test -race ./...
go vet ./...
go build -o /dev/null ./cmd/glm-worker
cd ..
./tests/install_smoke.sh
```

## ライセンス

本リポジトリはMIT Licenseの下で配布する。詳細は[LICENSE](LICENSE)を参照。
