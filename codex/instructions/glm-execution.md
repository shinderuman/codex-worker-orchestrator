# GLM実行と待機

`glm-worker`または`glm-parent-action`を実行・待機する場合だけ適用する。目的は、deterministic orchestrationをmachineへ寄せ、Sol Highをsemantic判断へ集中させること。

## 実行

- model実行またはstate変更を行うcommandはsandbox外、実装上read-onlyのinspection/report commandはsandbox内で実行する。command名で推測せずside effectを正とする。
- 同じ依頼を重複起動せず、GLM処理中にCodex自身が同じ調査・実装を代行しない。直接許可済みのrelease/deploy途中で新しい開発変更が必要になった場合も`~/.codex/instructions/direct-edit.md`の境界に従う。
- 1回の新規taskには同じ責務・変更理由・検証単位に属する要求だけを渡す。独立workstreamは分け、同時変更しないと整合しない要求は分断しない。
- 未検証の外部成立性は`~/.codex/instructions/feasibility-gate.md`、外部failureの追加evidenceは`~/.codex/instructions/failure-evidence.md`、escaped bug/reviewの原因分析は`~/.codex/instructions/escaped-cause-layer.md`に従う。機械的admissionやschemaは`control:external-feasibility-admission`を正とし、親はsemantic十分性だけを判断する。
- worker依頼には調査・実装・必要テスト・lint/build・自己レビューまでを含める。独立reviewerはwrapperが別sessionで起動するため、依頼文へ重複要求しない。
- `.glm-worker-repository-harness`でopt-inしたrepoでは、task要求は`IMPLEMENTATION_TASKS/`のtask fileをauthorityとし、Planの`## ACTIVE`から1件だけ指す。USER_REQUESTへtask全文を複製しない。user指示のdurable requirement化は`~/.codex/instructions/task-request-boundary.md`に従う。
- parent metadata failureや外部成立性admission failureはGLM再実行で推測修復せず、親がauthority/evidenceを修復して同じtaskを継続する。
- `AGENTS.md`等にある一般品質ゲートをworker依頼へ列挙し直さず、task固有の条件だけを明記する。packet上限を超える正確な一覧は`REPORT_ARTIFACT_DIR`へ保存させる。
- 同一taskがSol判断待ち・review fix・rate/provider stop中なら新規taskへ切り替えず、保存済みstateを継続する。
- token/モデル配分の評価を依頼された場合だけ`glm-worker --stats`等のtelemetryを読む。通常作業で調整目的だけの詳細ログ再読をしない。

## machine executionの反復cost観測

- worker/reviewerの`反復コスト観測:`報告や通常orchestration中に見つけた反復について、同一/重複処理か、通常loopで再発するか、品質coverageを保ったまま回数・待ち時間・model/provider消費を減らせるかを判断する。
- 改善価値があれば現在ACTIVE taskへ混ぜず、semanticに独立したfollow-upへする。一時migration、必要なintegration test、効果の小さい最適化はtask化しない。
- deterministicな既知手続きはまずmachine ownerへ寄せる。unknown failureを網羅する専用harnessを増やすためにCodex判断やsession lifecycleを増やさない。

## 親action surface

- lifecycleの正規入口はcanonical handoffである。`control:parent-action-staging-admission`とmachineが返す`consistent`・`required_action`・`allowed_actions`・`action_specs`を次操作のtransport authorityとする。packet本文や記憶した手順からcommandを再構成しない。
- 親Codexが行うのは、machine-admitted actionからsemanticに適切なものを選ぶことと、decision/fix等のsemantic payloadを確定することだけである。
- `action_specs[action].kind:"direct"`なら同specの`command`をlosslessに実行する。引数・順序・required parameterを親で補完しない。
- `kind:"staged"`なら同specの`prepare_command`を実行し、返されたexact staging pathに対してmachine-declared `slots`だけをsemantic値へ置換する。staging file全体の再解釈、path/token/header/placeholder名の推測をしない。編集成功後は返された`next_command`を正とする。
- decisionの`execution_unit`だけはsemantic判断であり、`single`または`milestones`を選ぶ。`milestones`なら同じ自然停止境界で残作業を2〜8個のbounded scope/acceptanceへ分ける。token量・file数だけでは決めない。既存pending milestone authorityはbypassしない。
- fixのorigin/cause/accepted-scopeは観測・semantic metadataである。起点と原因層は一次証拠から親が決め、GLMへ追加推定させない。machine contractが受理するsurface以外を作らない。
- machine projectionに必要なaction spec/slot/next commandが欠落・矛盾している場合はfail closedし、旧stdin transport、shell quoting、独自path探索へfallbackしない。
- `glm-worker --decision-stdin`/`--fix-stdin`はrecovery/debug互換であり、normal parent workflowのtransport authorityではない。

## 親操作のsemantic disposition

- PASS/`NEEDS_SOL_REVIEW`/`NEEDS_SOL_DECISION`等の意味、fix内容、Go/No-Go、quality policy surface承認の是非はSol Highが判断する。machine action specは合法な操作とtransportを決めるだけで、semantic採否を決めない。
- fix originは実際の起点に合わせる。reviewer既記載の指摘は`glm-reviewer`、親自身の新規検出は`codex-review`、user追加指示は`user-amendment`、外部reviewは`external-review`、parent metadata修復は`metadata-repair`とする。確定不能時だけunknown扱いにする。
- `codex-review`のcauseはparent-orchestration / requirement-preservation / worker / reviewer / sol-gate / production-wiring / test-scenario / cross-cutting-invariantから一次証拠で選ぶ。確定不能時はunknownとする。
- recoverable taskはcanonical handoffのresume actionを継続し、元依頼を再構成しない。

## 対象repoの生存判定

- task生存判定はrepository-scoped machine stateを正とし、global process一覧や別repositoryのprocessから推測しない。
- 同じrepoのrepository lockだけが重複起動の待避理由になる。別repoの終了待ちやglobal mutexを追加しない。
- activeなのにrepository lockがfreeでresume checkpointもない場合はstale候補としてrepo固有の復旧へ進む。lock/state間raceは次command自身のadmissionで収束させ、親が独自lock protocolを組まない。

## 待機

- model実行を伴う主`glm-parent-action`はproductionのparent-wait leaseを保持し、同repoの重複ownerをmachine側で拒否する。親は別のpoll ownerを作らない。
- Desktop/tool transport境界の長時間接続だけはrepository外である。主model呼出または`glm-parent-action wait`のcode-mode cellでは外側`// @exec: {"yield_time_ms":21600000,"max_output_tokens":1000}`と`background_terminal_max_timeout=21600000`を使い、内側`tools.exec_command`がrunning sessionを返す場合の`tools.write_stdin`も同じ長時間yieldを使う。この指定はlifecycle判断ではない。
- 主tool sessionを失った場合は`glm-parent-action wait`をcanonical recovery waiterとして1回使う。machineがowner release・worker release・owner epochを処理し、最後にrecovery handoffを返す。taskがactiveのまま親ownerだけ失われた場合は`owner_lost:true`として返し、親はstatus/watch loopや新規task起動へ切り替えない。
- 主呼出またはwait継続中は、経過時間だけを理由にpoll・進捗用model return・別terminal操作を追加しない。terminal、Sol/user attention、rate/provider stop、user interruptionだけを制御復帰境界とする。
- packet受理・install完了は局所終端であり、親USER_REQUESTの完了は`~/.codex/instructions/task-lifecycle.md`のsemantic lifecycleで判断する。

## terminal result

- normal `glm-parent-action`はterminal machine resultとcanonical handoffを同一`parent_action_terminal` envelopeで返す。親tool側で追加`--handoff`を組み立てたり、raw terminal bytesを再取得したりしない。
- `terminal`はworker/actionのauthoritative semantic result、`handoff`はnext-action authorityである。envelopeのstatusをPASS/NEEDS_SOL_*の代用にしない。
- terminal transport/parse failure等でenvelopeを取得できない場合だけ、read-only `glm-worker --handoff recovery`をbounded recovery入口として使う。通常pathの追加handoff取得へ使わない。
- terminal/handoffがmalformed・矛盾・欠落ならfail closedとし、telemetryやartifactを広く探索してtransportを補完しない。必要なrecovery projectionがある場合だけそのbounded surfaceを使う。
- Desktop/UI上の重複描画だけを理由にrepo側のblind dedupeや追加model turnを作らない。

## rate limit停止(stderr error JSON `kind: rate_limited`)

- `detail.limit: ZAI_GLM_CODING_PLAN_5H`は正常な一時停止であり、task/worktree/session/checkpointを破棄しない。
- auto resume可能なら`~/.codex/instructions/glm-auto-resume.md`のmachine transactionを使う。登録不能時だけ手動再開へ落とす。
- manual resumeでもcanonical resume actionで同一task・phase・sessionを継続し、元依頼を再構成しない。
- reset未到達ならrate-limited stateを保持したままfail closedする。
