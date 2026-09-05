# GLM実行と待機

`glm-worker`または`glm-parent-action`を実行・待機する場合だけ適用する。目的は無意味なSol High再起動とpolling・deterministic transportによるトークン消費を防ぐこと。

## 実行

- model実行またはstate変更を行うcommandはsandbox外、実装上read-onlyの`--status`・`--handoff`・`--stats`・`--watch`等のinspection/report commandはsandbox内で実行する。command名で推測せずside effectを正とする。
- 同じ依頼を重複起動せず、GLM処理中にCodex自身が同じ調査・実装を代行しない。release・deploy等の直接許可が既にある場合でも、その途中で新たに必要になった開発変更は`~/.codex/instructions/direct-edit.md`の境界に従い新規taskへ切り出す。
- 1回の新規taskには、同じ責務・変更理由・検証単位に属する要求だけを渡す。相互に独立したsubsystem・workstream・不具合群は別taskへ分けるが、同時変更しないと整合しない要求は分断しない。
- 外部service・取得方式・実行環境等の未検証成立性が本番設計の前提になる依頼は、`~/.codex/instructions/feasibility-gate.md`を読んでから委譲内容を構成する。委譲前にACTIVE task file本文へ`## External feasibility`宣言節があることを確認する。宣言のないtaskはglm-workerがmodel呼出0回でfail closedする。
- 外部取得・parser・integration failureの原因診断にstatus・size・error分類だけでは足りない依頼は、`~/.codex/instructions/failure-evidence.md`を読んでから委譲内容を構成する。
- 外部review・実運用で見つかったescaped bug・escaped reviewの原因分析を委譲する場合は、`~/.codex/instructions/escaped-cause-layer.md`を読んでから委譲内容を構成する。
- worker依頼には調査・実装・必要テスト・lint/build・自己レビューまでを含め、独立reviewerの起動や「独立reviewまで」は要求しない。wrapperがworker完了後に別sessionのreviewerを自動実行する。
- repository rootへ`IMPLEMENTATION_PLAN.local.md`が存在するrepoでは、新規taskの要求はOriginal instruction・Amendments・Contract・Must not・Acceptance criteriaを備えたtask fileとして`IMPLEMENTATION_TASKS/`配下へ置き、Planの`## ACTIVE`節から1件だけ指す。USER_REQUESTへtask詳細を複製せず、task要旨と参照だけを渡す。wrapperは全worker/reviewer呼出で同じtask file本文を読ませる配線を持つ。
- user指示をACTIVE taskのdurable requirementへ反映する境界は`~/.codex/instructions/task-request-boundary.md`に従う。task完了前のtask file削除・history移行・plan昇格は行わない。
- `"status":"NEEDS_SOL_REVIEW"`の理由がACTIVE task解決失敗(`parent_metadata_active_unresolvable`)または親管理metadata検出(`parent_metadata_*`)のときは、GLM側の再実行で解決しない。PlanのACTIVE欄・参照task file・親管理metadata現物を親Codexが直接確認・修復してから同じtaskを再開する。
- 理由が外部成立性宣言検証(`external_feasibility_missing`・`external_feasibility_malformed`・`external_feasibility_unverified`)のときもGLM側の再実行で解決しない。親Codexがtask fileへ`## External feasibility`宣言を追加・修正してから同じtaskを再開する。拒否時点のtask status・resume checkpoint・pending decisionは保持されるため、decision待ちは同じdecision本文を、rate limit・provider停止・--stop停止中は同じresume actionを再送してよい。`status: poc`/`observation`taskの完了は親Go/No-Go待ち(`NEEDS_SOL_DECISION`)として返るため、Go判断は宣言を`status: implementation`へ書き換えてから`glm-parent-action decision`で渡す。
- `AGENTS.md`や既存規約にある一般品質ゲートを依頼文へ列挙し直さず、タスク固有の完了条件・対象・除外事項・必要テストだけを明記する。
- 正確な長い一覧や監査報告がpacket上限へ収まらない場合は、実行時に渡される`REPORT_ARTIFACT_DIR`へ保存させ、packetでは`artifacts`の絶対パスだけを受け取る。
- 同一taskがSol判断待ち・review fix・rate limit中なら分割や新規起動へ切り替えず、保存済みtaskとsessionを継続する。
- モデル配分・token節約・品質バランスの調整を依頼された場合だけ`glm-worker --stats`を実行し、出力の`telemetry_dir`にあるタスク別JSONLで呼出別のusage・実モデル・結果を比較する。総消費量にはsubagentを含むtree usageを使う。通常作業では調整目的のためだけに詳細ログを読まない。

## machine executionの反復cost観測

- worker/reviewerの結果へ接頭辞`反復コスト観測:`付きの報告がある場合、および通常orchestration中に自ら発見した場合は、ユーザーの個別指摘を待たず、反復costを別task化して改善する価値があるか判断する。対象は親Codexの手作業だけでなく、worker/reviewer/test/build/lint/smoke/provider probe/polling/resume verification等のmachine executionの反復を含む。
- 判断は最低限、同一または意味的に重複した処理が反復しているか、今回限りではなく今後の通常loopでも再発するか、品質coverageを維持したまま実行回数・待ち時間・model/provider消費を減らせるか、expensive real executionとcheap contract/mock verificationを分離できるか、改善実装と保守costに見合うか、false success・flakiness・観測不能化を生まないかで行う。
- 改善価値がある場合は現在ACTIVE taskへ無関係なrefactorとして混ぜず、semanticに独立したfollow-up taskとして通常Plan lifecycleへ追加する。一時的なmigration、一度限りの長時間処理、意図的に必要なintegration test、改善効果が小さいものはtask化しない。本節は既存のparent orchestration product化判断をmachine executionの反復costまで拡張した運用であり、独立した新frameworkではない。

## 親action surface

- Plan管理repositoryでcurrent ACTIVE taskを開始するときは、sandbox外で`glm-parent-action start`を1回だけ実行する。wrapperは固定semantic requestを既存glm-worker new-task admissionへ渡す。ACTIVE task本文をUSER_REQUESTへ複製しない。
- decision・fixのsemantic payloadを親Codexが確定した後は、`prepare -> placeholder apply_patch -> 実action`を1つのcode-mode/tool orchestration内で連続実行し、その間にSolへ戻らない。まずsandbox内で`glm-parent-action prepare decision|fix`を実行し、machine JSONが`status:"prepared"`、期待した`action`、現在のprepareが返した`token`・`path`を持ち、`path`がrepository直下の`.glm-worker-parent-actions/`内を指すことを機械確認する。parse失敗・欠落・不一致ならpatch/actionを行わず停止する。
- prepare直後のstaging fileは再読しない。production prepare contractが作る既知のtoken binding headerと`__GLM_PARENT_ACTION_PAYLOAD__`だけを前提に、返されたexact `path`のplaceholderだけをCodex標準の`apply_patch`でsemantic payloadへ置換し、headerを保持する。patch失敗時は実actionを呼ばない。`cat`・`sed`・heredoc・shell redirect・Python等のread/write代替へ切り替えず、staging filenameを推測しない。
- patch成功後、同じtool orchestration内でsandbox外の`glm-parent-action decision <token>`または`glm-parent-action fix <token> [--origin <値>] [--cause <値>] [--accepted-scope current-diff] [--approval-only]`へ進む。実actionは返されたexact tokenだけを使い、file pathは渡さない。長時間実actionの待機は下記「待機」の同一cell境界をそのまま使う。
- quality policy surface変更で`NEEDS_SOL_REVIEW`停止し、親Codexがsemantic fixを要求せず停止時点のexact current diffだけを承認する場合は、`prepare fix`で通常どおりtoken-bound payloadを用意したうえで`glm-parent-action fix <token> --accepted-scope current-diff --approval-only`を使う。`--approval-only`はこの場合だけ使い、`--origin`を併用しない。semantic修正を要求する場合は従来の通常fixを使う。
- staging rootはrepository直下の`.glm-worker-parent-actions/`に固定する。token形式不正、token binding不一致、placeholder未置換、symlink化されたdirectory/file、1 MiB超payloadはstate変更・model呼出前にfail closedする。
- wrapperはpayloadをmemoryへ読み、staging fileを削除してからUTF-8 byte長・SHA-256を機械計算し、既存`glm-worker --decision-stdin`/`--fix-stdin`へ直接渡す。semantic本文中のbacktick、dollar、single quote、double quote、NUL、改行を無変換で保持する。
- `glm-worker --decision-stdin`/`--fix-stdin`はrecovery/debug用に残すが、通常親workflowではbyte長・hash・TTY・`stdin_ready`・`write_stdin`・shell quotingを扱わず、旧transportへfallbackしない。
- staging fileをconsumeした後にactionが失敗した場合は同じfile/tokenを再利用せず、新しいprepareから同じsemantic payloadを再送する。

## 親操作のoutcome申告

- terminal packet(PASS・`NEEDS_SOL_REVIEW`・`NEEDS_SOL_DECISION`・fail closed結果)を受理して追加操作なしで当該taskを完了させるとき、次の作業へ移る前に`glm-parent-action accept`を1回だけ実行する。underlying `--accept`は観測記録専用でmodel呼出・Git操作を行わず、open opportunityがないときの再実行はno-opである。
- `NEEDS_SOL_DECISION`待ちへacceptを使わない。判断は`glm-parent-action decision`で渡し、decision outcomeはglm-workerが自動確定する。
- fixでは差戻しの実際の起点に合わせて`--origin codex-review|glm-reviewer|user-amendment|external-review|metadata-repair`を申告する。glm-worker reviewerのterminal result(`NEEDS_SOL_REVIEW`等)へ既に記載された指摘をそのまま差し戻すときは`glm-reviewer`、親Codex自身がterminal packet受領後の最終reviewで新たに検出した指摘のときだけ`codex-review`とする。userの修正要求・追加指示は`user-amendment`、repo外の外部reviewは`external-review`、`parent_metadata_*`等の親管理metadata修復は`metadata-repair`である。新規検出かreviewer既記載か確定できないときだけ申告を省略し、`unknown`として計上される。`codex-review`への推定fallbackは行わない。
- `--origin codex-review`のfixでは`--cause`（8層: parent-orchestration/requirement-preservation/worker/reviewer/sol-gate/production-wiring/test-scenario/cross-cutting-invariant、確定不能時はunknown）の申告も必須である。原因層は一次証拠に基づき親Codexが確定し、GLMへ推定・追加model callさせない。他originでは`--cause`は任意、`--approval-only`との併用は拒否される。fix roundのcategoryとsemantic/non-semanticはround recordから機械導出される。
- `--accepted-scope`は`current-diff`だけを許し、origin/cause/accepted-scopeは観測申告であってfix本文の内容・範囲を替わってはならない。
- recoverable taskの再開は`glm-parent-action resume`を使い、保存済みtask・phase・sessionを継続する。

## 対象repoの生存判定

- glm-worker taskの生存判定は`glm-worker --status`出力JSONの`repository_lock`(held/free、probe不能時はnull)と、`task_liveness`(running/stale。`task_status`が`active`のときだけ値が出て、非active時・probe不能時はnull)だけを使う。global process一覧・`pgrep`・Claude Code processの存在を生存判定や起動可否の根拠にしない。lock file内のPIDは診断情報であり、stale PIDやPID reuseでrunning扱いしない。
- `repository_lock`は対象repoのlockだけを意味する。別repositoryのglm-worker processやlockの解放を待たず、「GLM全体で同時実行不可」と推測して待機しない。global mutexは追加しない。
- 同じrepoの`repository_lock: held`だけが重複起動の待避理由になる。
- `task_status: active`・`repository_lock: free`・`resume_available: false`を別repo終了待ちにせず、stale候補として扱う。対象repoのworking treeとstateを確認し、repo固有の復旧へ進む。checkpointがある場合は既存のresume手順に従う。
- status観測と次command実行の間のraceは、次command自身が同じrepoのlockを取り直すことで安全に収束する。lock取得失敗だけを重複起動の根拠にする。

## 待機

- 通常の完了待機は当該taskを起動した主`glm-parent-action`/`glm-worker`呼出1件だけをownerとし、そのtool resultを待つ。
- 長時間の主呼出を起動するcode-mode cellは、外側cell先頭へ`// @exec: {"yield_time_ms":21600000,"max_output_tokens":1000}`を指定する。`~/.codex/config.toml`の`background_terminal_max_timeout=21600000`と同じ6時間境界を使い、outer cellを短いyieldで終了させない。
- 内側の初回`tools.exec_command`も`yield_time_ms=21600000`で待つ。hostがrunning session IDを返した場合は同じcode-mode cellを終了せず、空の`tools.write_stdin`を`yield_time_ms=21600000`で同じsessionへ送り、terminal・Sol/user attention・rate/provider stop等の意味のある状態変化まで同一tool orchestration内に留まる。running状態だけをSolへ返さない。
- 主呼出が継続中は、別の`--status`・`--watch`・terminal操作や経過時間だけを理由とする進捗発言を追加しない。無出力や経過時間だけを理由に中断・再実行・重複起動しない。ユーザーが状態確認を明示した場合は確認して応答してよい。
- 主呼出のtool sessionを失った・中断した場合だけ`glm-worker --handoff`を1回実行し、`consistent`・`required_action`・`allowed_actions`を正規入口とする。`consistent:false`では操作を推測しない。handoffがcurrent taskを`active`かつ`required_action:"none"`として返した場合だけ`glm-worker --watch`をread-only attach recoveryに使い、詳細診断が必要な場合だけ`--status`を追加する。
- terminal・Sol/user attention・rate/provider stop等の意味のある状態変化で制御が戻ったらpacketを処理し、可能な次工程へ進む。経過時間だけのliveness報告は行わない。
- packet受理・install完了は局所終端であり、親USER_REQUESTの完了か次の継続操作かは`~/.codex/instructions/task-lifecycle.md`を読んで判断する。

## 親tool orchestrationのterminal result返却

- terminal payload二面表示の原因層はglm-worker内部emitではなく親tool orchestrationとDesktop表示である。glm-workerの主呼出は受理したterminal resultをstdoutへ1回だけ出力する。本契約手順が強制するのはmodel contextへのaccepted terminal result流入を1回にすることであり、Desktopの同一描画が再発しても、それだけを理由にtransport-only model turnを追加しない。
- 長時間cell(主`glm-parent-action`/`glm-worker`呼出)では、`functions.exec`のorchestration内でnested `exec_command`・`write_stdin`の各outputを変数へ蓄積し、raw stdout・packetをtext・notify・image等の即時描画経路へ出さない。stdin_ready等のtransport control event、進捗表示、重複echoはterminal resultへ含めず、owner commandが返したauthoritative machine JSONだけを機械抽出する。
- `glm-parent-action start|decision|fix|resume|accept|no-go`が正常terminalへ到達した場合は、同じcode-mode cell内で直ちにread-onlyの`glm-worker --handoff`を1回実行する。terminal JSONとhandoff JSONをそれぞれparseし、handoffの`consistent`・`required_action`・`allowed_actions`をcanonical next-action authorityとして保持する。`consistent:false`も隠さず返し、次actionは実行しない。`glm-parent-action finalize-check`は既存machine result自身がcanonical `handoff`を同梱するため、二度目の`--handoff`を呼ばずそのfieldを使う。
- cellの唯一のmodel-visible返り値はbounded machine resultとし、通常parent actionでは`{"status":"parent_action_terminal","terminal":<authoritative terminal JSON>,"handoff":<canonical handoff JSON>}`を返す。finalize-checkはhandoff重複を避けるため既存machine resultをそのまま返す。terminal/handoffが空・malformed・複数候補で一意に定まらない場合は任意のtextを選ばず、compactなtransport errorと利用可能なevidence referenceだけを返してfail closedする。
- raw transcriptやlarge diagnosticは既存telemetry/artifact参照へ残し、envelopeへ丸ごとinlineしない。terminal packetの`NEEDS_SOL_DECISION`・`NEEDS_SOL_REVIEW`・PASS・failure semanticsは`terminal`内で損なわず、handoffはnext-action admission専用とする。
- `functions.store`→`GLM_TERMINAL_CAPTURED <key>`→別model turn→`load(key)`はnormal pathで使わない。background orchestrationを`functions.wait`で受ける場合も、そのwait結果が上記bounded machine resultを直接親へ返す終端とし、同じbytesを取得するためだけの同期`functions.exec`/store loadを追加しない。
- background cellの完了表示と`functions.wait`に同じbounded resultがUI上二重描画され得ても、model context二重流入・測定可能なCodex実消費増・Quality Delta低下の新証拠がない限り、repo側PACKET/JSON blind dedupeやstore/load再導入を行わない。

## rate limit停止(stderr error JSON `kind: rate_limited`)

- `detail.limit: ZAI_GLM_CODING_PLAN_5H`は正常な一時停止であり、`worker_error`にしない。`detail.reset_at_cst`は中国標準時（UTC+8）。
- working tree、task state、session、resume checkpointを破棄・resetせず、新規taskとしてやり直さない。
- `detail.auto_resume_available: true`なら`~/.codex/instructions/glm-auto-resume.md`を読み、現在のCodexタスクへ自動再開automationを作成または更新する。作成不能な場合だけ手動再開を案内する。
- 手動再開指示では`glm-parent-action resume`を使い、保存済みの同一task・phase・sessionから継続する。元依頼を再構成しない。
- 枠が未回復なら再び`kind: rate_limited`として状態を保持する。
