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

このrecordは`5c7b265ad89057dcb8367ca44f464bd053a5230e:IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`で要求されたbounded inventoryのcross-task decisionである。分類は「proseに規則があるか」ではなく、親Codexがその規則を読み落とす・誤parameter/action/orderを選ぶ前提でproduction admission/postconditionが違反を拒否できるかで決める。

### Bounded inventory

14 controlを分類した。`machine-enforced=7`、`partial=4`、`prose-only=1`、`semantic-parent-only=1`、`external-unenforceable=1`。

- `parent-metadata-integrity` — `machine-enforced`。ownerは`glm-worker/internal/workflow/planfile.go::captureParentFileGuard` / `verifyParentFileAfterCall`とStateStore parent-file snapshot。worker/reviewerが親managed metadataを変えればmodel call後guardで停止する。文字列存在testではなくproduction call境界のbefore/after検証を根拠にする。
- `worker-git-snapshot-safety` — `machine-enforced`。ownerはworkflowのrepository snapshot/guardと`glm-worker/internal/state`のsnapshot state。HEAD/index/worktree driftはresume/review/finalizationのadmissionで一致を要求し、instructionの「Gitを触るな」だけを成立条件にしない。
- `packet-schema` — `machine-enforced`。ownerは`glm-worker/internal/packet/schema.go`、`contract.go`、`result.go`と`glm-worker/internal/workflow/model_call.go::parseModelCallResult` / `handleInvalidModelResult`。`glm-worker/internal/packet/*_test.go`はschema/status constraint rejectionを検証し、prompt内schema記載だけをevidenceにしない。
- `reviewer-session-separation` — `machine-enforced`。ownerはrole別session/state lifecycleとrunner invocation。worker/reviewer roleは別session identity/stateで扱い、reviewer再利用をinstructionだけに依存させない。session invalidation/role separation testをproduction evidenceとする。
- `quality-snapshot-binding` — `machine-enforced`。ownerはquality-gate/finalizationのrepository snapshot evidenceと`glm-worker/internal/parentactioncmd/finalization.go`。routing/working-dir/head/index/worktree evidenceがcurrent task境界と一致しない成功結果はfinalizationへ通さない。
- `remote-completion-sync` — `machine-enforced`。ownerは`glm-worker/internal/parentactioncmd/complete.go::verifyParentCompletion` / `verifyCompletionRemoteSync` / `verifyCompletionUnchanged`。`complete_test.go::TestCompleteKeepsAwaitingWhileFinalHeadIsAhead`、`TestCompleteKeepsAwaitingForRemoteFailures`、`TestVerifyCompletionUnchangedDetectsRaceBeforeTransition`がwrong/missing remote postconditionをproductionで拒否する。過去のmanual `push-binding`だけに依存したpartial状態はcurrent implementationでは解消済みであり、再実装しない。
- `session-rotation-claim-bind-start` — `machine-enforced`。ownerは`glm-worker/internal/state/session_rotation.go::ClaimSessionRotation` / `BindSessionRotationClaim` / `AcknowledgeSessionRotationClaim`と`glm-worker/internal/workflow/model_call.go::acknowledgeSessionRotationStart`。directive/claim/thread/target task identityとstate順序をproductionで照合し、wrong claim/bind/startは拒否する。
- `session-rotation-fail-proof` — `partial`。`glm-worker/internal/parentactioncmd/parentactioncmd.go::executeSessionRotationFail`は`state/session_rotation.go::ReleaseSessionRotationClaim`へ委譲するが、後者はmatching claimed stateだけでpendingへ戻し、「新thread作成失敗が確定した」というmachine evidenceを要求しない。作成結果unknownで解放可能なのはstate correctness gapなので独立修正対象とする。
- `runtime-install-completion` — `partial`。canonical install execution自体は既存`glm-parent-action install`に集約済みだが、runtime変更taskのterminal completionがcurrent HEAD/source digest・installed一致・必要smoke evidenceを必須postconditionとしてまだ束縛していない。旧source `5c7b265ad89057dcb8367ca44f464bd053a5230e:IMPLEMENTATION_TASKS/runtime-install-completion-binding.md`の責務へ統合し、別permission pathを作らない。
- `parent-wait-ownership` — `prose-only`。長時間model実行中の親Codex途中return/短周期poll/liveness turn禁止は親instruction依存が残る。旧source `5c7b265ad89057dcb8367ca44f464bd053a5230e:IMPLEMENTATION_TASKS/codex-instruction-conflict-reduction.md`のruntime wait ownershipとして独立処理する。
- `parent-plan-continuation` — `partial`。Plan/project-stateのdeterministic projectionは存在するが、局所task/install終端後に継続許可scopeが残る状態でUSER_REQUEST完了を宣言する親returnをterminal postconditionが拒否しない。旧source `5c7b265ad89057dcb8367ca44f464bd053a5230e:IMPLEMENTATION_TASKS/parent-plan-continuation-enforcement.md`の責務へ統合する。
- `automation-authority-transaction` — `partial`。repository側には`IMPLEMENTATION_TASKS/auto-resume-heartbeat-transaction.md`のspec/state/verify contractがあるが、外部Codex appのautomation作成・wake delivery境界をrepository processだけで完全に強制できない。active atomic transactionを正とし、別automation state machineを追加しない。
- `continuous-improvement-capture` — `semantic-parent-only`。改善候補の採否・task化はworkflow preferenceとsemantic productization判断であり、2026-09-10 architecture auditでdurable candidate admission state machineを不採用とした。`IMPLEMENTATION_RULES.md`のparent orchestration product化判断と`IMPLEMENTATION_TASKS/codex-efficiency-control-loop-checkpoint.md`のbounded取りこぼし再精査をownerとし、LLMがproseを読み落とし得ることだけを理由にhard lifecycle stateへ昇格しない。
- `user-requirement-ingress` — `external-unenforceable`。repository processが信頼できるuser-turn identity/bindingを取得できない現状では、latest user requirementのtracked化をmachine-enforcedと宣言するとfalse guaranteeになる。`IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md`をMEASURE FIRST / BLOCKEDの再評価境界として維持し、一次証拠が得られるまでruntime state machineを実装しない。

External reviewで懸念されたsession-rotation markerのread-modify-write競合は、current caller/state順序を再確認したが独立したlive prose controlとして成立する再現pathを確認できなかった。`rotation-claim` / `rotation-bind` / `rotation-fail`はrepository lock配下、new-task start acknowledgeはbound target identityを照合する。将来callerがこのserialization/phase ownershipを外す変更をする場合は、その変更自身のtestで再評価する。現時点では追加state/recoveryを作らない。

### Prose thinning input

machine-enforced controlの手続き説明が重複する主要candidate surfaceを、installed sourceのUTF-8 file byte数でbounded proxy化した。`codex/AGENTS.md` 7,794 bytes、`codex/instructions/glm-execution.md` 18,998、`glm-packets.md` 11,230、`quality-gate-capability.md` 5,183、`session-rotation.md` 4,185、`execution-permission.md` 4,564、合計51,954 bytes。token値はtokenizer exact値ではなく比較用`ceil(bytes/4)` proxyで約12,989 tokensとする。

重複手続きgroupは少なくとも4つある: packet手順(`AGENTS.md` / `glm-execution.md` / `glm-packets.md`)、parent action/permission(`AGENTS.md` / `execution-permission.md`)、quality gate(`AGENTS.md` / `quality-gate-capability.md`)、session rotation(`AGENTS.md` / `session-rotation.md`)。thinningでは目的・machine owner・provenance・残余semantic judgmentを残し、上の`partial` / `prose-only` / `semantic-parent-only` / `external-unenforceable`をmachine-enforced扱いで薄くしない。

### Reevaluation boundary

`IMPLEMENTATION_TASKS/post-105-codex-efficiency-reevaluation.md`はこのheadingを先行bounded inventoryとして参照し、その後のcontrol delta、未処理candidate、machine guard違反、locator driftだけを再評価する。全履歴inventoryを再実行しない。machine-enforced controlのcompact registry化と実際のinstruction thinningは、このrecordを入力にする後続の専用mechanization/thinning責務で行い、本auditでは新しいruntime registry/stateを追加しない。
