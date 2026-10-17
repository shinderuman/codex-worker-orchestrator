# Task: 通常Workflowの検証配置と最終source binding

## Original instruction

2026-10-16のユーザー原文（元Taskから分離した要求）:

```text
目的は、正規Workflow全体の検証コストを下げること。個々のテスト高速化・削減だけでなく、実装・修正・Review・Validation・install・公開前readinessの各段階で、なぜ40分級の検証が何度も必要になるのかを既存証拠から確認する。

検証の配置、重複実行、証拠の失効条件、再利用条件、最終sourceへのbindingを一体で評価し、品質保証を維持した最小限の実行で完遂できる通常Workflowを設計・実装する。テスト自体の削減・高速化も有効なら行うが、それだけで解決扱いにしない。

必要なcoverage・安全境界を維持し、検証省略や過剰なfallbackで通さない。前Task限定の例外は引き継がない。

通常成功と修正・再Review時の経路で、必要な検証が成立し、不要な重複実行が減ることを実証する。
```

2026-10-17のユーザー原文（分離と費用対効果の判断条件）:

```text
D5'を維持すること自体を目的にしないでください。合理的なら完成させてよいですが、今回のTask完了に必要でない複雑な実装になっている場合は、独立Taskへの分離や採用取り消しも検討してください。

当初のGoテスト高速化と、途中で追加されたWorkflow全体の改善要求を区別してください。

既存要求を無断で破棄せず、今回完成させるものと、独立した後続Taskに分離できるものを判断してください。

不要な機構を残すために新しい機構を追加するような循環を避けてください。後方互換性のためだけの実装も維持しないでください。
```

## Amendments

## Resolved references

- 分離元は`IMPLEMENTATION_TASKS/go-suite-execution-time-reduction.md`。元TaskはGo suite、特に`internal/app`の高速化・安定化を収束させる。本Taskは未実装の一般化Workflow要求を保持し、元Taskの完了を本要求の実装済み証拠と扱わない。
- D5'は完成・採用を前提にしない。元Taskでの採用取り消し後も、既存Task artifactの調査・不採用差分は参照証拠として利用できる。40分級というユーザー指摘、失敗runの短縮、成功した同条件測定を区別する。

## Purpose

品質保証を維持したまま、通常Workflow全体で必要な検証の配置・回数・時間と最終sourceへの帰属を改善する。

## External feasibility

status: not-applicable

## Contract

- 既存証拠を優先し、実装・修正・Review・Validation・install・公開前readinessの各owner、検証目的、sourceと実効入力、失効・再利用条件を具体化する。
- full-suite実行時間、package時間、Git/Fixture等の処理単価、gate呼び出し回数を区別し、実production runの帰属とPASS/FAILを示す。未確認の重複候補を削減実績へ数えない。
- 通常成功と修正・再Review経路で、品質を維持する最小限の実行へ収束するWorkflowを設計・実装する。新機構は実測上の削減効果が実装・検証・保守コストに見合う場合だけSol判断を通して採用する。
- 既存ownerを優先し、最終source・検証義務・実効入力に合致しない証拠を転用しない。変更後の失敗・不完全・破損をPASSへ昇格させない。

## Must not

- D5'の採用や維持を目的化しない。必要なcoverageや安全境界を減らすことで時間短縮を代替しない。
- 元Task限定の通し再実行省略特例を引き継がない。
- 推定fallback、旧schema互換bridge、第二State正本、汎用executor/cacheを根拠なく追加しない。
- GLMへGit remote writeやparent-managed metadata編集を許可しない。

## Acceptance criteria

- 既存証拠とcurrent sourceから、各段階の実行理由・重複・失効・再利用・最終bindingを説明できる。
- 採用した変更で、通常成功と修正・再Reviewの両経路の必要な検証と最終source bindingが成立し、不要な重複が減ることを実証する。
- 測定条件、実行回数と時間、未検証事項、採用案の費用対効果を明示する。
- 独立Review、必要なValidation、lint/build、installと公開前readinessを正規Lifecycleで成立させる。

## Historical invariants

- Codex ReductionのためにQuality Deltaを悪化させない。
- 決定可能な機械処理は既存ownerへ寄せ、追加model反復や検証反復で効率化を代替しない。

## Dependencies

none
