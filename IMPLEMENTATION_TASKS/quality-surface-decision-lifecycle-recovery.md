# Task: Quality-surface decision lifecycle recovery

## Original instruction

````text
decision継続のworker結果がquality policy surface変更としてNEEDS_SOL_REVIEW停止した際、task.statusがwaiting-sol-reviewへ遷移してもpending-decision markerが残存し、canonical handoffがlifecycle inconsistencyとなってapprove-surface / fix / parkの全操作を拒否する事象を解消する。

観測したcurrent stateは、resume checkpointがstage=worker、phase=worker-decision、stop_kind=""、quality_surface_approval_pending=true、completed_result.status=IMPLEMENTEDを保持する一方、task.status=waiting-sol-reviewとpending-decision markerが同時に存在していた。通常の--recover-parent-actionはpre-call guard failure後のactive leftoverだけを対象とするため、この状態を復旧できなかった。
````

## Amendments

### 2026-09-09 approval outcome leak

````text
quality-surface承認後にreviewerのauto-fixへ進み、そこでrate limit停止した際、resume checkpointはstage=auto-fix、phase=worker-auto-fix-1、stop_kind=rate-limitedとして正しく保存された一方、parent_review_open=NEEDS_SOL_REVIEWが残存した。stopped lifecycleはopen parent reviewを拒否するため、canonical handoffとresumeが再びlifecycle inconsistencyで停止した。
````

### 2026-09-09 one-time state repair authorization

````text
死ね
やれ
````

Resolved meaning: 直前に提示した、`task-stats.json`のstaleな`parent_review_open` fieldだけを除去して同一checkpointから再開する一回限りの内部state修復を実行する明示許可。

## Resolved references

- incident task ID: `1f16a904-2d8b-4ebd-822c-a60e194b55c8`
- triggering phase: `worker-decision`
- triggering packet: `NEEDS_SOL_REVIEW` from quality policy surface protection

## Purpose

decision継続後のquality-surface停止を正規の`approve-surface`または`fix`待ちへ収束させ、内部marker矛盾による親workflowの行き止まりをなくす。

## External feasibility

status: not-applicable

## Contract

- `worker-decision`からquality-surface approval待ちへ遷移するowner pathを特定し、`pending-decision`とresume checkpointの更新を原子的なstate transitionとして扱う
- `waiting-sol-review`かつ`quality_surface_approval_pending=true`の正規状態では`pending-decision`を残さない
- `approve-surface`受理時に対象のopen parent review opportunityを明示的にcloseし、reviewer/auto-fix/rate-limit経路へ持ち越さない
- transition途中のwrite failureでは元状態へrollbackし、矛盾したwaiting stateを公開しない
- 既発生stateに対して、安全条件を機械検証したうえで正規状態へ戻せるbounded recovery pathを提供するか、既存recovery commandへ同等の限定経路を追加する
- 通常decision、通常review、explicit fix、rate limit/provider stop、guard/quality-gate recoverableの既存遷移を維持する

## Must not

- `waiting-sol-review`の任意状態から無条件に`pending-decision`を削除しない
- stopped taskの整合性検査を緩めてopen parent reviewを黙認しない
- resume checkpoint、completed worker result、open parent review、quality-surface approval bindingを破棄して復旧しない
- canonical handoffの整合性検査を緩めて矛盾状態を許容しない
- 手動state編集を恒久的な通常復旧手順にしない
- GLM worker/reviewerへGit remote write authorityを与えない

## Acceptance criteria

- decision継続後のquality-surface停止が`waiting-sol-review`、`pending-decision=false`、`quality_surface_approval_pending=true`へ収束する直接testがある
- 同状態のcanonical handoffが`required_action=approve-surface`と`accepted-scope=current-diff`を返す
- `approve-surface`後にreviewer/auto-fixでrate limit停止しても`parent_review_open=none`を保ち、canonical handoffが`required_action=resume`を返す直接testがある
- state write failure時のrollbackを直接testできる
- 既発生する旧矛盾状態のrecoveryはtask ID、status、checkpoint phase/result、quality approval、parent review等の限定条件が一致する場合だけ成立する
- recovery後もcompleted worker resultを再実行せずreviewerへ継続できる
- relevant/full test、independent reviewer、Sol semantic review、必要なruntime install/smokeを完了する

## Historical invariants

- parent-managed implementation metadataは親Codexだけが編集する
- GLM worker/reviewerにGit remote write authorityを与えない

## Dependencies

none

## Review findings

- `Workflow.failClosedQualitySurface`は`WaitForSolReview`を呼ぶが、decision継続で残る`pending-decision`を消去しない
- `waitingReviewActionPlan`は`pending-decision=true`をlifecycle inconsistencyとして正しく拒否するため、問題はhandoff検査ではなく停止遷移側にある
- `--recover-parent-action`は`active`かつpre-call guard errorだけを対象とし、このpost-result waiting stateは対象外
- `ExecuteQualitySurfaceApproval` / `ActivateQualitySurfaceApproval`は承認前に開かれた`NEEDS_SOL_REVIEW` opportunityをcloseせず、rate-limit停止時の`stoppedActionPlan`が正しく矛盾として拒否した

## Current boundary

現ACTIVE taskのquality-surface承認停止で実際に行き止まりが発生した。現taskはexact state確認とrecoverableな一回限りのmarker退避で継続し、本taskでは同じ手動介入を不要にするproduction transitionとbounded recoveryを実装する。
