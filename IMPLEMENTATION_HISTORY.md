# codex-worker-orchestrator 例外decision記録

このfileは通常taskの完了ledgerではない。ordinary completionのcommit・diff・validation・install・runtime evidenceはGit、CI、bundle / telemetryを正とし、task要求は削除済みtask fileのGit履歴から回収する。通常task完了時にこのfileを読んだり追記したりしない。

親Codexだけが編集し、GLM worker/reviewerは読み取り専用とする。新しいrecordは次をすべて満たす場合だけ追加する。

- production diffやcurrent Rules/task contractだけでは表現されないcross-taskの採否・Go/No-Go decisionである
- 将来のtracked taskがそのdecisionをactivation / adoption条件として明示参照する
- raw transcript、検証chronology、commit一覧を複製せず、decision・根拠の最小要約・再評価境界だけで足りる

参照taskがなくなったrecord、またはcurrent Rules/task contractへ移行して過去decisionを読む必要がなくなったrecordは削除する。escaped defectの詳細診断を保存するためにこのfileを増やさず、再発防止として残す必要がある意味契約はcurrent Rules/task/testへ移す。

## 2026-08-28 Task 012 compaction threshold evaluation

- decision: No-Go。保存済み20 task・69 model call中、compact boundaryは4 call / 4件だった。
- limitation: trigger直前context sizeとcompaction要約costを当時のevidenceから確定できず、transport上のsemantic marker保持だけでもpost-compactionの意味適用を証明できない。
- reevaluation boundary: `IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`のpermission / activation contractに従い、同形式の実測とsemantic quality evidenceを揃えてからthreshold変更を再判断する。

## 2026-08-29 Task 013 worker model routing evaluation

- decision: No-Go。current codex-config telemetryはsingle resolved model `glm-5.3`だけで、alias差からmodel品質差を評価できない。
- reevaluation boundary: `IMPLEMENTATION_TASKS/102-model-routing-redesign.md`に従い、同一repository・role・normalized phase・effective risk・convergence deltaで複数resolved modelを比較できる実運用evidenceとユーザー許可が揃った時だけrouting変更を再判断する。

## 2026-08-23 Claude CLI compatibility preflight

- decision: runtime fail-closed preflightは不採用。test-only checker、依存flag inventory、help snapshot、live no-AI canaryだけを最小採用した。
- tradeoff: PoC時点のruntime overheadは約0.25秒、見込んだ親Codex診断削減は1〜2 turnで、help format依存によるfalse rejectと全task停止riskを上回る採用根拠がなかった。
- reevaluation boundary: `IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`に定義した実互換障害、診断turn反復、またはflag/help drift canaryの実失敗が観測された時だけ再評価する。

## 2026-09-11 prose-only control enforcement audit

このrecordは`5c7b265ad89057dcb8367ca44f464bd053a5230e:IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`のbounded inventoryである。PR #391時点の14-control inventoryはtop-level instruction surfaceとproduction admissionの横断が不足していたため、current main `6bc5096aee058acadfb7a3886257e3a4f65551ff`で再監査した。

監査scopeは`AGENTS.md`、`codex/AGENTS.md`、`IMPLEMENTATION_RULES.md`、current PlanのACTIVE/NEXT/BLOCKED、`codex/instructions/*.md`全件、`codex/instructions/worker/*.md`、関連する`workflow` / `state` / `parentactioncmd` / `packet` / `runner` / `app` production owner、completed/current task/Issueの既存ownerである。分類基準は「proseに規則があるか」ではなく、親Codex/workerがその規則を読み落とす・誤parameter/action/orderを選ぶ前提でproduction admission/postconditionが違反を拒否できるかとする。

### Bounded inventory

29 logical controlを分類した。`machine-enforced=13`、`partial=9`、`prose-only=1`、`semantic-parent-only=3`、`external-unenforceable=3`。

#### machine-enforced

- `parent-metadata-integrity` — ownerは`glm-worker/internal/workflow/planfile.go`のparent-file before/after guardとStateStore parent-file snapshot。worker/reviewerによるparent-managed metadata mutationはproduction model-call境界で拒否される。
- `worker-git-authority-snapshot` — ownerは`glm-worker/internal/runner/git_authority_guard.go`とworkflow/state repository snapshot。HEAD/index/protected ref/config/worktree authority driftをproduction guard/recoveryで検出し、worker instructionだけを成立条件にしない。
- `packet-schema-result` — ownerは`glm-worker/internal/packet/schema.go` / `contract.go` / `result.go`と`workflow/model_call.go`のparse/retry path。不正status/field/contractはproduction packet acceptanceで拒否される。
- `reviewer-session-capability-separation` — ownerはrole別session/state lifecycleとrunner invocation。worker/reviewerのsession/capabilityをproductionで分離し、prompt上のrole宣言だけに依存しない。
- `quality-snapshot-binding` — ownerはquality-gate snapshot evidenceと`glm-worker/internal/parentactioncmd/finalization.go`。routing/working-dir/HEAD/index/worktree evidenceがcurrent task境界と一致しない成功はfinalizationへ通らない。
- `remote-completion-sync` — ownerは`glm-worker/internal/parentactioncmd/complete.go::verifyParentCompletion` / `verifyCompletionRemoteSync` / `verifyCompletionUnchanged`。remote未同期・raceをterminal completionとして受理しない。
- `session-rotation-claim-bind-start` — ownerは`glm-worker/internal/state/session_rotation.go`と`workflow/model_call.go::acknowledgeSessionRotationStart`。directive/claim/thread/target-task identityと順序をproduction stateで検証する。
- `external-feasibility-admission` — ownerは`glm-worker/internal/workflow/externalfeasibility.go::ensureExternalFeasibility`。taskのExternal feasibility status/evidenceと変更前Go/No-Go境界をmachine admissionへ結び、最終Go/No-Go判断だけを親semantic authorityに残す。
- `repo-search-exhaustive-activation` — ownerは`glm-worker/internal/workflow/exhaustive_search.go`とrepo-search command/state。opt-in、exhaustive marker、search evidenceをproduction pathで扱い、`glm-repo-search.md`だけをauthorityにしない。
- `stop-isolate-park-lifecycle` — ownerはStateStore stop/isolate/park stateと`glm-worker` command lifecycle。canonical停止・隔離・park/unparkはmachine state transitionを持ち、任意killを正常系にしない。
- `orphan-watch-terminalization` — ownerは`glm-worker/internal/app`のwatch/orphan terminal pathとそのregression tests。terminal childを親のliveness proseだけに依存して待ち続けない。
- `parent-action-staging-admission` — ownerは`glm-worker/internal/parentactioncmd`のaction parser/staging/lifecycle checks。decision/fix/accept/resume/finalize等の合法順序・token/staging条件をmachine admissionで拒否できる。
- `parent-evidence-projection-dedup` — ownerは`glm-parent-action evidence`とparent evidence state。canonical structured evidenceはbounded projection、authority snapshot binding、duplicate projection rejectionを持つ。任意shell readまで全面禁止できるとは分類しない。

#### partial

- `session-rotation-fail-proof` — matching claimだけで`rotation-fail`をpendingへ戻せ、new-thread作成失敗確定のmachine evidenceを要求しない。ownerは#390。
- `runtime-install-completion` — canonical install actionはmachine化済みだが、runtime変更taskのterminal completionとcurrent HEAD/source digest/installed一致/smoke evidenceの束縛が未完了。ownerは#369。
- `parent-plan-continuation` — project-state projectionはmachine化済みだが、継続許可scopeが残る局所terminalをUSER_REQUEST terminalへ誤変換する親returnを完全には拒否しない。ownerは#371。
- `glm-auto-resume-automation-transaction` — repository側はwake spec/coalesce/verifyを持つが、Codex app create/update/wake deliveryは外部境界を含む。ownerはcurrent `IMPLEMENTATION_TASKS/auto-resume-heartbeat-transaction.md`。#339はre-entry choreographyの別ownerであり重複しない。
- `codex-auto-resume-automation-transaction` — `codex-auto-resume.md`はexpected key、PAUSED placeholder、UTC one-shot update、exact returned ID、failure cleanupを親proseで要求するが、repository側の`--codex-limit` / `--verify-codex-wake`の間をmachine transactionとして束縛していない。新規ownerは#394。
- `failure-artifact-confidentiality` — `failure-evidence.md`はcredential/token/cookie/session ID/個人情報をartifact保存前に除去するよう要求するが、`glm-worker/internal/packet/validate.go::ValidateArtifacts`はpath、regular file、symlink containment、duplicateだけを検証し内容のsensitive admissionを持たない。新規ownerは#395。
- `sol-review-evidence-before-accept` — `glm-packets.md`は`NEEDS_SOL_REVIEW`で親Solがtargets/current source/diffを実査するよう要求する。#316はそのread量をbounded化したが、`glm-worker/internal/state/parent_review.go::ParentReviewOpenState` / `resolveParentOutcome`はmatching current review evidence未取得でも`accept`を成立させられる。新規ownerは#396。
- `execution-permission-convergence` — known canonical operationはmanaged `glm-parent-action`/execpolicyへ収束しているが、外部approval/sandbox denialの意味分類や新しいexecution boundaryはrepository単独では完全強制できない。既存permission-convergence implementationを再実装せず、`execution-permission.md`のsemantic/external residualだけを残す。
- `user-global-instruction-config-ownership` — installer/tool-owned scopeとuser-owned global Codex/Claude settingsの境界は一部production ownershipを持つが、global instruction mutation全体は親操作を含む。`agents-management.md`の対象を含め、ownerは#362。別のglobal-config state machineを作らない。

#### prose-only

- `parent-wait-ownership` — 長時間GLM処理中の途中return、短周期poll、liveness turn、重複起動の禁止は親instruction依存が残る。ownerは#370。

#### semantic-parent-only

- `continuous-improvement-capture` — 改善候補の採否/task化はworkflow preferenceとsemantic productization判断である。architecture auditでdurable candidate admission state machineを不採用とし、`IMPLEMENTATION_RULES.md`のproduct化判断と`IMPLEMENTATION_TASKS/codex-efficiency-control-loop-checkpoint.md`のbounded取りこぼし再精査をownerとする。non-blocking captureを含む再評価は同checkpointで行い、prose違反可能性だけを理由にhard lifecycle stateへ昇格しない。
- `escaped-cause-semantic-classification` — `escaped-cause-layer.md`が要求する「どの原因層へ再発防止を置くか」は一次証拠を入力にしたsemantic判断でありgeneric classifierへ置換しない。machine evidence取得・task lifecycleと区別する。
- `goal-task-review-semantic-disposition` — Goalの妥当性、質問/run-control/新要求の意味分類、Sol reviewのfinding採否そのものは親semantic authorityに残す。machine layerはidentity/state/action admissibilityだけを強制し、意味判断をregexやauto-acceptへ移さない。

#### external-unenforceable

- `user-requirement-ingress` — repository processが信頼できるuser-turn identity/bindingを取得できない現状では、最新要求のtracked化を完全machine-enforcedと宣言できない。`IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md`をMEASURE FIRST/BLOCKEDの再評価境界とする。
- `direct-edit-user-authority` — `direct-edit.md`の「ユーザーがCodex自身の直接編集を明示したか」は同じtrusted user-turn identity制約を持つ。別taskを増やさず`user-requirement-ingress-binding.md`の外部境界へ統合する。repositoryが親の任意host editを完全監視できると偽らない。
- `backup-destructive-host-operation` — `backup.md`のcopy/checksum後だけ元backupを削除する規則は重要なdata-loss境界だが、repository process外の任意`cp`/`mv`/`rm`をrepo helper追加だけで強制不能である。user-global shell policyを本repository都合で全面拘束するwrapper/daemonは責務とriskが不相応なので新規mechanizationを作らず、外部host-operation boundaryとして残す。

### Gap disposition

旧14-control inventoryで既知ownerだった#369/#370/#371/#372と、当時新規に切った#390は維持する。再監査で新たに成立したhigh-risk gapは#394 `Mechanize Codex 5h wake scheduling transaction`、#395 `Enforce sensitive failure-artifact admission`、#396 `Bind Sol review evidence before parent accept`へ独立DO化した。

#394は外部Codex automation writeとのtransaction境界、#395はartifact confidentiality、#396はSol review Quality Delta admissionであり、互いに別owner/別failure domainを持つため#368内の一括implementationへ混在させない。各Issueはproduction command/state/testをAcceptanceに持ち、単なるinstruction追記では閉じない。

`backup-destructive-host-operation`、trusted user-turn identityに依存する`user-requirement-ingress`/`direct-edit-user-authority`はrepository単独のproduction gateを作るとfalse guaranteeまたはuser-global authority侵害になるためtask化しない。`continuous-improvement-capture`等のsemantic判断も同様にhard state machine化しない。これは見落としではなくdisposition gateの結果である。

### Instruction-presence vs production evidence

instruction文字列を検査するtestはrouting/install/prose driftの検出には使えるが、control成立の証拠には数えない。`machine-enforced`分類は上記production ownerの拒否/postcondition/state transitionとbehavior testを根拠にする。代表negative evidenceはpacket invalid-result rejection、Git authority guard、finalization snapshot/remote-sync rejection、session-rotation identity admission、external-feasibility admissionであり、これらを新規mechanization対象へ戻さない。

代表positive gap evidenceは、`ValidateArtifacts`がartifact path/symlinkだけを検査しsecret内容を受理可能であること、`ParentReviewOpenState`がreview evidence proofを持たず`NEEDS_SOL_REVIEW` acceptをgateしないこと、Codex 5h wakeでmachine-generated create/update transaction specが存在しないことである。

### Prose thinning input

既存の主要重複surface proxyはcurrent sourceでも、`codex/AGENTS.md`約7.8KB、`codex/instructions/glm-execution.md`18,998 bytes、`glm-packets.md`11,230、`quality-gate-capability.md`5,183、`session-rotation.md`4,185、`execution-permission.md`4,564で約52KB / `ceil(bytes/4)`約13k-token proxyである。加えて再監査で、まだpartialの`glm-auto-resume.md`17,452 bytesと`codex-auto-resume.md`12,056 bytesを確認した。

重複procedure groupはpacket(`AGENTS.md` / `glm-execution.md` / `glm-packets.md`)、parent action/permission(`AGENTS.md` / `execution-permission.md`)、quality gate(`AGENTS.md` / `quality-gate-capability.md`)、session rotation(`AGENTS.md` / `session-rotation.md`)、auto-resume(GLM/Codex各instructionとmachine spec/verify)である。#372ではmachine ownerへ移った手続きだけをcompact indexへ縮め、#369/#370/#371/#390/#394/#395/#396やACTIVE automation transactionなど未解決partial/prose-only controlをmachine-enforced扱いで先に薄くしない。

### Reevaluation boundary

`IMPLEMENTATION_TASKS/post-105-codex-efficiency-reevaluation.md`はこのheadingを先行bounded inventoryとして参照し、以後はこのbaseline後のcontrol delta、未処理candidate、machine guard違反、locator driftを評価する。新しいparent instruction、new state-changing parent action、new external automation/file mutation boundaryが追加された場合は「既存inventory外だから対象外」とせず、同じthreat modelで分類する。
