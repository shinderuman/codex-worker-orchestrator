# Task: Codex review gap telemetry

## Original instruction

````text
じゃあ022より前のタスクとして全部積んで対応してくれるか
なおCodexのトークン消費は減らしたいがGLMのトークン消費節約の優先度はそこまでではない
GLMのトークン消費を節約するためにCodexのトークンが増えるみたいなのは本末転倒
````

## Amendments

none

## Resolved references

- 2026-09-03調査では`codex-review`起点fixが31回/21 tasks、`glm-reviewer`起点fixが14回/11 tasksあったが、現telemetryはoriginより深いsemantic causeを集計できない
- 対象はSol review省略ではなく、GLM reviewerの反復的な見落としを改善するための低cardinality観測である

## Purpose

Sol品質gateを維持したまま、Codexが繰り返し発見しているreview gapと親Codex再作業量を機械集計可能にする。

## External feasibility

status: not-applicable

## Contract

- Codex review起点fixへ既存cause-layer taxonomy、対象category、semantic/non-semantic、downstream worker/reviewer callと親Codex token deltaを関連付ける
- 親Codex tokenは`parent-codex-token-attribution.md`の共通projectionを再利用し、unknownを保持する
- 低cardinality fieldとcount/source locatorだけをtelemetry/reportへ保存し、prompt本文やreview本文を複製しない
- read-only summaryでorigin/cause/category別の件数とCodex reworkを追加AI callなしで返す

## Must not

- 観測追加だけをreviewer省略・downgradeの許可にしない
- GLM token削減を成功基準の上位に置かない
- free-form causeを無制限保存しない
- 親Codexのsemantic classificationをGLMへ委譲する追加model callを作らない

## Acceptance criteria

- origin/cause/category、known/unknown、rework associationをfixtureで検証する
- existing parent outcome telemetryとの互換を保ち、追加AI callなしで集計できる
- parent token attribution不能時もfix countを失わずunknown理由を返す
- 106 review-call-reductionは自動ACTIVE化せず、品質比較証拠なしの省略を防ぐ
- 独立reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- Codex新規検出とGLM reviewer既記載の差戻しをoriginで分離する

## Dependencies

- `IMPLEMENTATION_TASKS/parent-codex-token-attribution.md`

## Review findings

none

## Current boundary

timeline fallback完了後に実行する。
