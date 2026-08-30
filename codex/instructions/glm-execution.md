# GLM実行と待機

`glm-worker`または`glm-parent-action`を実行・待機する場合だけ適用する。目的はSol Highのsemantic判断を維持しながら、transport・polling・deterministic caller作業によるトークン消費を減らすこと。

## 実行

- model実行またはstate変更を行うcommandはsandbox外、実装上read-onlyの`--status`・`--handoff`・`--stats`・`--watch`等のinspection/report commandはsandbox内で実行する。command名で推測せずside effectを正とする。
- `~/.codex/config.toml`の`background_terminal_max_timeout`は`21600000`ms（6時間）を前提とする。
- 同じ依頼を重複起動せず、GLM処理中にCodex自身が同じ調査・実装を代行しない。release・deploy等の直接許可が既にある場合でも、その途中で新たに必要になった開発変更は`~/.codex/instructions/direct-edit.md`の境界に従い新規taskへ切り出す。
- 1回の新規taskには、同じ責務・変更理由・検証単位に属する要求だけを渡す。相互に独立したsubsystem・workstream・不具合群は別taskへ分けるが、同時変更しないと整合しない要求は分断しない。
- 外部service・取得方式・実行環境等の未検証成立性が本番設計の前提になる依頼は、`~/.codex/instructions/feasibility-gate.md`を読んでから委譲内容を構成する。委譲前にACTIVE task file本文へ`## External feasibility`宣言節があることを確認する。宣言のないtaskはglm-workerがmodel呼出0回でfail closedする。
- 外部取得・parser・integration failureの原因診断にstatus・size・error分類だけでは足りない依頼は、`~/.codex/instructions/failure-evidence.md`を読んでから委譲内容を構成する。
- 外部review・実運用で見つかったescaped bug・escaped reviewの原因分析を委譲する場合は、`~/.codex/instructions/escaped-cause-layer.md`を読んでから委譲内容を構成する。
- worker依頼には調査・実装・必要テスト・lint/build・自己レビューまでを含め、独立reviewerの起動や「独立reviewまで」は要求しない。wrapperがworker完了後に別sessionのreviewerを自動実行する。
- repository rootへ`IMPLEMENTATION_PLAN.local.md`が存在するrepoでは、新規taskの要求はOriginal instruction・Amendments・Contract・Must not・Acceptance criteriaを備えたtask fileとして`IMPLEMENTATION_TASKS/`配下へ置き、Planの`## ACTIVE`節から1件だけ指す。USER_REQUESTへtask詳細を複製せず、task要旨と参照だけを渡す。wrapperは全worker/reviewer呼出で同じtask file本文を読ませる配線を持つ。
- user指示をACTIVE taskのdurable requirementへ反映する境界は`~/.codex/instructions/task-request-boundary.md`に従う。task完了前のtask file削除・history移行・plan昇格は行わない。
- `"status":"NEEDS_SOL_REVIEW"`の理由がACTIVE task解決失敗(`parent_metadata_active_unresolvable`)または親管理metadata検出(`parent_metadata_*`)のときは、GLM側の再実行で解決しない。PlanのACTIVE欄・参照task file・親管理metadata現物を親Codexが直接確認・修復してから同じtaskを再開する。
- 理由が外部成立性宣言検証(`external_feasibility_missing`・`external_feasibility_malformed`・`external_feasibility_unverified`)のときもGLM側の再実行で解決しない。親Codexがtask fileへ`## External feasibility`宣言を追加・修正してから同じtaskを再開する。拒否時点のtask status・resume checkpoint・pending decisionは保持される。`status: poc`/`observation`taskの完了は親Go/No-Go待ち(`NEEDS_SOL_DECISION`)として返るため、Go判断は宣言を`status: implementation`へ書き換えてから同じtaskへdecisionを渡す。
- `AGENTS.md`や既存規約にある一般品質ゲートを依頼文へ列挙し直さず、タスク固有の完了条件・対象・除外事項・必要テストだけを明記する。
- 正確な長い一覧や監査報告がpacket上限へ収まらない場合は、実行時に渡される`REPORT_ARTIFACT_DIR`へ保存させ、packetでは`artifacts`の絶対パスだけを受け取る。
- 同一taskがSol判断待ち・review fix・rate limit中なら分割や新規起動へ切り替えず、保存済みtaskとsessionを継続する。
- モデル配分・token節約・品質バランスの調整を依頼された場合だけ`glm-worker --stats`を実行し、出力の`telemetry_dir`にあるタスク別JSONLで呼出別のusage・実モデル・結果を比較する。通常作業では調整目的のためだけに詳細ログを読まない。

## machine executionの反復cost観測

- worker/reviewer結果の`反復コスト観測:`報告、および通常orchestration中に自ら発見した反復costは、ユーザーの個別指摘を待たず改善価値を判断する。対象は親Codexの手作業だけでなく、worker/reviewer/test/build/lint/smoke/provider probe/polling/resume verification等を含む。
- 同一または意味的に重複した処理が今後も再発するか、品質coverageを維持して実行回数・待ち時間・model/provider消費を減らせるか、expensive real executionとcheap contract/mock verificationを分離できるか、改善実装と保守costに見合うか、false success・flakiness・観測不能化を生まないかで判断する。
- 改善価値がある場合は現在ACTIVE taskへ無関係なrefactorとして混ぜず、semanticに独立したfollow-up taskとして通常Plan lifecycleへ追加する。一度限りのmigration・長時間処理、意図的に必要なintegration test、改善効果が小さいものはtask化しない。

## 親action surface

通常の親lifecycle操作は`glm-parent-action`を使う。親CodexはUTF-8 byte長、SHA-256、TTY、raw mode、`stdin_ready`、exactly-once `write_stdin`、shell quotingを扱わない。これらはrepository-owned wrapperと既存`glm-worker --decision-stdin`/`--fix-stdin`内部の責務である。

### 新規task開始

- Plan管理repositoryでcurrent ACTIVE taskを開始するときは、sandbox外で`glm-parent-action start`を1回だけ実行する。wrapperは固定semantic requestを既存`glm-worker` new-task admissionへ渡し、ACTIVE task本文を要求authorityとして使わせる。
- 新規task開始のためにpayload fileを作らず、USER_REQUESTへACTIVE task本文を複製しない。

### decision/fix payload

decision・fixだけ次の固定手順を使う。

1. sandbox内で`glm-parent-action prepare decision|fix`を実行し、JSONの`token`とrepository内の`path`を受け取る。
2. 返された既存staging fileのplaceholderをCodex標準の`apply_patch`でsemantic payload本文へ置換する。`cat`・heredoc・shell redirect・Python等の別write手段へ切り替えない。
3. state/model変更を行う実actionはsandbox外で`glm-parent-action decision <token>`または`glm-parent-action fix <token> [--origin <値>] [--accepted-scope current-diff]`を実行する。
4. 実action開始後は`~/.codex/instructions/glm-wait.md`に従って同じprocessを最大待機境界で待つ。

- staging pathはrepository root直下の`.glm-worker-parent-actions/`だけで、prepareがcrypto-random token付きregular fileを生成する。実actionはfile pathを受け取らずtokenだけを受け取る。親が任意pathを指定してsandbox外のprocessへ読ませる経路を作らない。
- staging fileはGit管理外で、wrapperが内容をmemoryへ読み込んだ後、`glm-worker`を起動する前にconsume/removeする。action失敗後にpayloadを再送する必要があれば、新しいprepareからやり直す。
- placeholder未置換、token形式不正、symlink化されたstaging directory/file、1 MiB超payloadはstate変更・model call前にfail closedする。
- `decision`本文はSol判断、`fix`本文は修正指示そのものとし、wrapperが内容を生成・要約・書換えしない。
- fixの`--origin`は`codex-review|glm-reviewer|user-amendment|external-review|metadata-repair`だけ、`--accepted-scope`は`current-diff`だけを許す。wrapperは同じ値を既存glm-worker admissionへ渡す。
- direct `glm-worker --decision-stdin`/`--fix-stdin`はdebug/recovery用に残すが通常親workflowでは使わない。旧byte-count/READY/write_stdin choreographyへfallbackしない。

### payloadを持たないaction

- terminal packetを受理して追加操作なしで当該taskを完了させるときは、次の作業へ移る前にsandbox外で`glm-parent-action accept`を1回だけ実行する。underlying `glm-worker --accept`と同じ観測記録で、model呼出・Git操作を追加しない。
- recoverable taskの再開はsandbox外で`glm-parent-action resume`を使う。保存済みtask・phase・sessionを継続し、新規taskとしてやり直さない。
- `--status`・`--handoff`・`--watch`等のread-only inspection/reportは従来どおり`glm-worker`をsandbox内で直接使う。

## 親操作のoutcome申告

- `NEEDS_SOL_DECISION`待ちへacceptを使わない。判断は`glm-parent-action decision`で渡し、decision outcomeはglm-workerが自動確定する。
- fixでは差戻しの実際の起点に合わせてoriginを申告する。reviewerのterminal resultへ既に記載された指摘は`glm-reviewer`、親Codex自身がterminal packet受領後の最終reviewで新たに検出した指摘だけ`codex-review`、userの修正要求・追加指示は`user-amendment`、repo外reviewは`external-review`、親管理metadata修復は`metadata-repair`とする。確定できないときだけ省略し`unknown`として計上する。
- originは観測申告であり、fix本文の内容・範囲を替わってはならない。

## 対象repoの生存判定

- glm-worker taskの生存判定は`glm-worker --status`出力JSONの`repository_lock`(held/free、probe不能時はnull)と`task_liveness`(running/stale、非active時・probe不能時はnull)だけを使う。global process一覧・`pgrep`・Claude Code processの存在を生存判定や起動可否の根拠にしない。lock file内PIDは診断情報でありstale PIDやPID reuseでrunning扱いしない。
- `repository_lock`は対象repoのlockだけを意味する。別repositoryのglm-worker processやlockの解放を待たない。同じrepoの`repository_lock: held`だけを重複起動の待避理由にする。
- `task_status: active`・`repository_lock: free`・`resume_available: false`はstale候補として扱い、対象repoのworking treeとstateを確認してrepo固有の復旧へ進む。checkpointがある場合は既存resume手順に従う。
- status観測と次command実行のraceは次command自身が同じrepo lockを取り直すことで安全に収束する。lock取得失敗だけを重複起動の根拠にする。

## 待機

- 完了待機対象は当該taskを起動した主`glm-parent-action`/`glm-worker`process(session)だけとする。主呼出はterminal・Sol/user attention状態でpacketを出力して終了するため、観測用の別commandを完了待機へ使わない。
- 主呼出のexec cell・session IDを失ったattach recovery時だけ`glm-worker --watch`で既存taskへ追加AI callなしでattachする。resident monitorとして付けっぱなしにしない。
- `--watch`終了後も`--status`等を固定間隔で繰り返すpollingへfallbackしない。
- 最初のexecからbackground terminalで利用可能な最大待機時間を指定し、可能な限り同一tool orchestration内で完了までblocking waitする。tool内部上限でsession IDが返る場合も短時間・固定間隔でwaitを掛け直さない。
- tool orchestrationやexec cellに対する短時間・固定間隔の反復wait、固定間隔の`write_stdin`、status・端末出力・生存確認を行わない。一定時間無出力であることだけを理由に失敗・再実行しない。
- 無出力を理由にした定期進捗発言、進捗報告目的のwake・待機短縮・中断・GLMへの問い合わせをしない。ユーザーが状態確認を明示した場合だけ中間状態を確認してよい。
- 最大待機時間後も生存していれば再調査・代替作業・重複起動をせず再び最大時間で待つ。
- 完了時はユーザーの追加入力を待たずpacket処理と可能な次工程を進める。packet受理・個別commit・install完了は局所終端であり、親USER_REQUESTの完了か次の継続操作かは`~/.codex/instructions/task-lifecycle.md`を読んで判断する。

## 親tool orchestrationのterminal payload単一描画

- terminal payload二面表示の原因層はglm-worker内部emitではなく親tool orchestrationとDesktop表示である。glm-workerの主呼出は受理したterminal resultをstdoutへ1回だけ出力する。ユーザー可視描画回数はDesktop表示層の外部境界としてrepoから強制できない。
- 単一postconditionは「1 accepted terminal resultにつき、親tool orchestration全体でユーザー可視payloadは1回」である。同一payloadのmodel context・永続contextへの二重流入や測定可能なCodex実消費増などの実害証拠がない限り、表示の再発をrepo内再調査・orchestration変更の理由にしない。
- 長時間主呼出ではnested command outputを変数へ蓄積し、raw stdout・packetを即時描画経路へ出さない。cell終端でraw terminal payloadをFunctions storeへtask固有keyで保存し、cell返値は短いcaptured markerだけにする。その後の短い同期取得でrawを1回だけ親へ渡す。
- background cellの完了outputとwaitの双方へ同じraw payloadを流さない。repo側のblind dedupeで正当な別terminal resultを抑止しない。
- 境界検証は追加AI callなしのdelayed markerと実terminal resultを同じbackground exec→wait→同期取得境界で行う。単発live観測・模擬test・契約文面だけでDesktop renderer問題まで解消済みと報告しない。

## rate limit停止(stderr error JSON `kind: rate_limited`)

- `detail.limit: ZAI_GLM_CODING_PLAN_5H`は正常な一時停止であり`worker_error`にしない。`detail.reset_at_cst`は中国標準時（UTC+8）。
- working tree、task state、session、resume checkpointを破棄・resetせず新規taskとしてやり直さない。
- `detail.auto_resume_available: true`なら`~/.codex/instructions/glm-auto-resume.md`を読み、現在のCodexタスクへ自動再開automationを作成または更新する。作成不能な場合だけ手動再開を案内する。
- 手動再開では`glm-parent-action resume`を使い、保存済みの同一task・phase・sessionから継続する。元依頼を再構成しない。
- 枠が未回復なら再び`kind: rate_limited`として状態を保持する。
