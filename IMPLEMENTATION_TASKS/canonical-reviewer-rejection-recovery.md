# Task: Reviewer拒否とterminal出力を含む正規Workflow完遂

## Original instruction

親Codexによるblocker修正要求:

```text
canonical TaskのreviewerがNEEDS_SOL_DECISIONを返し、decision boundary validationに拒否された後、Taskがactive・resume checkpointのstop_kindが空のまま残り、主処理とstop endpointが終了して正規resume/fixがadmitされなくなる不具合を修正する。

canonical execution Taskの解決でlegacy active-task stateだけに依存しない。拒否されたreviewer結果をPASS/accept/decision待ちへ推定昇格させず、Task/session/attempt/lease/sourceと証拠を保持する、既存ownerによる正規の停止・回収・再提出または継続経路を成立させる。現在保持されている同一Taskを安全に回収する手順を含む。

復旧のためにRuntime stateを手書きしない。Taskの新規重複起動、成果破棄、quality gateの成功偽装、汎用fallback、不要な最適化機構を追加しない。意味のある状態・CLI・schema変更が必要なら実装前にSol判断へ戻す。
```

## Amendments

### 2026-10-17: 正規Workflow完遂とterminal出力不具合

ユーザー原文:

```text
`canonical-reviewer-rejection-recovery.md`の内容と実行順序を修正してください。

**1. BLOCKEDからNEXT先頭へ移動**

このTaskは現在`BLOCKED / USER_PERMISSION_WAIT`にありますが、Task本文に未解決のDependencyや許可待ち条件はありません。

Controller上の`independent-blocking`と、修正Task自体が実行不能であることを混同しないでください。

実際の阻害条件が存在しなければ、`BLOCKED`から除外し、現ACTIVEの直後となる`NEXT`先頭へ移動してください。現ACTIVEは変更しません。

**2. terminal出力バグを既存Taskへ追加**

`glm-parent-action`のterminal出力に、次の不具合があります。

- terminal出力が10,240 bytesの上限を超えて返却に失敗する。
- overflow用の代替出力自体も上限を超え、正常なエラー応答すら返せない。
- 実例：通常出力24,153 bytes、overflow出力21,964 bytes。

この不具合を、既存のReviewer拒否後の経路不達と合わせて、**「正規WorkflowでTaskを完遂できない」問題として同一Taskで修正**してください。別Taskは作成しないでください。

単純なサイズ上限引き上げやState修復だけを解決とせず、親への結果返却、Handoff、次操作への接続を含めて正常化してください。

**3. 完了条件**

Reviewer拒否後の経路不達とterminal出力不具合の両方を修正し、通常のTask開始からReview・Publication・Retireまで、例外的なState直接修正やCodexの直接実装修正なしで完遂できることを確認してください。

途中で同じ正規Workflowの完遂を阻む別の不具合が判明した場合も、局所的な回避だけで完了扱いにしないでください。

既存TaskのOriginal instructionは維持し、今回の追加要求をAmendmentsへ記録して、Contract・Acceptance criteriaを更新してください。

今回はTask定義とPlanの修正だけを行い、現ACTIVEの実行を妨げず、修正Taskの実装は開始しないでください。
```

## Resolved references

- 発見境界は`go-suite-execution-time-reduction`のreviewer結果拒否。エラーは`reviewer returned NEEDS_SOL_DECISION without an active task decision boundary`。
- 確認対象のownerは`glm-worker/internal/workflow/decision_boundary_review.go`の`validateReviewerDecisionBoundary`、canonical Taskを解決する`activetask.go`、結果拒否時のcheckpoint/lifecycleとparent action admission。
- canonical recovery waiterが`owner_lost:true`を返してもresume/fixがadmitされず、通常stopが`stop_endpoint_absent`となる境界を対象とする。sourceとmachine evidenceは当該TaskのRuntime保持物を正とし、進捗snapshotを本fileへ複製しない。

## Purpose

無効なReviewer結果とterminal出力不達でTask継続性が失われる状態を解消し、親への結果返却から次操作、Review・Publication・Retireまで正規Workflowを接続する。

## External feasibility

status: not-applicable

## Contract

- source改訂後の未コミットproject metadataとfresh materializationのtree源の不整合も同じ正規Workflow阻害として確認する。根拠はACTIVE runtime artifact `root-attempt-source-import-implementation.md`の「残余として記録する新規境界」。現在TaskのPublication/Retireへ影響しないことを局所回帰と実運用で区別し、後続開始時のpostcondition mismatchを局所回避で成功扱いしない。

- 同じ正規Workflowの阻害要因として、root attemptの親source同期が空PredecessorAttemptIDを前提に失敗する境界も扱う。現ACTIVE継続に直接必要な最小owner修正が先行した場合は成果を再実装せず、残る統合保証と通常Lifecycle実証を引き継ぐ。

- Reviewer拒否後の経路不達とterminal出力の二重overflowを、正規Workflow完遂を阻む同一責務として修正する。独立した別Taskへ分離しない。
- terminal通常出力とoverflow時の応答を、親が結果とHandoffを受け取り合法な次操作へ接続できる形へ正常化する。10,240 bytesの単純な上限引き上げやState修復だけを解決としない。
- 通常のTask開始からReview・Publication・Retireまで、例外的なState直接修正やCodexによる直接実装修正に依存せず完遂する。途中で同じ完遂経路を阻む別不具合が見つかった場合も、局所回避だけで完了扱いせず原因ownerと経路を是正する。
- Controller上のindependent-blockingというfinding分類と、修正Task自体の未解決dependency・許可待ちを区別する。実際の阻害条件がなければNEXTで実施可能なTaskとして扱い、scheduleの正本はPlanに保つ。
- 現Taskのcanonical authorityと旧state参照の関係を確認し、実際の原因を限定する。
- reviewer結果の拒否と、機械的にadmitされる停止・回収・継続操作を一体で扱う。正規Taskを解決できる場合でも、未確定axisがない結果を無条件に承認しない。
- 拒否直後・主owner消失後・stop endpoint不在の各境界で、同一Task/sessionとsourceを保った正規復旧を保証する。
- 既存ownerを優先し、必要最小限の修正と直接保証testで収束する。

## Must not

- terminal本文の返却失敗をHandoffの推定・捏造や親の例外操作で覆い隠さない。通常出力だけでなくoverflow応答自体の失敗も解消する。
- state/checkpoint/leaseを手書きで補完せず、拒否を成功へ読み替えない。
- 新Taskへ要求を再構成して再起動しない。正常なin-flight modelを重複起動しない。
- 一般的なrecovery framework、互換bridge、品質省略の恒久機構を追加しない。
- GLMによるGit remote writeとparent-managed metadata編集を認めない。

## Acceptance criteria

- Reviewer拒否と、通常出力24,153 bytes・overflow出力21,964 bytesとなった二重overflowの両故障を再現し、各原因ownerの修正を直接保証する。
- 通常terminalとoverflow境界のいずれも、親への結果返却・整合したHandoff・合法な次操作への接続が成立する。サイズ上限の単純引き上げや例外的State修復を成功証拠にしない。
- 通常のTask開始からReview・Publication・Retireまで、例外的なState直接修正やCodexの直接実装修正なしで完遂することを確認する。途中で見つかった同じ正規Workflowの阻害要因を局所回避だけで残したまま完了にしない。
- canonical Task解決と無効reviewer packetの扱いが直接保証される。
- activeだけが残り正規操作がなくなる状態を再現し、正式な停止・回収・継続で同じTaskへ戻れることを実証する。
- PASS/acceptの証拠とsource bindingを偽装・緩和せず、必要な独立Reviewと検証を行う。

## Historical invariants

- 機械的admissionを親の推測で迂回しない。
- 復旧はTask継続性と証拠を保持し、Codex Reductionのために品質を下げない。

## Dependencies

none
