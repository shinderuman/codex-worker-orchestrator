# Task: 新規symbolと削除行のreview evidence対応

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
受理するfile:symbol/行範囲targetでも、新規untracked symbolはgit diff HEADに出ずsource coverageもnumeric専用で証明不能になる。削除fileの行指定もcurrent source不在とnew-sideだけのhunk判定で証明不能になる。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F11。実装ownerは `glm-worker/internal/parentevidence/review.go の BuildReviewManifest / reviewSourceCoversTarget / reviewDiffHunkCurrentRange と projector.go の captureDiff / projectSource`。
- 再現資料: `review-evidence/target_audit_test.go.txt`。対象test: `TestAuditReviewUntrackedSymbolCanBeProven`, `TestAuditReviewDeletedNumericTargetCanBeProven`.

- 保存fixtureはreview時点のapp packageを対象とする。current ownerは上記parentevidenceへ移動済みなので、実装開始時は同じbehaviorの再現を正規ownerへ移す。削除されたapp APIのalias/互換層を復活させない。

## Purpose

新規APIと削除の通常reviewを、不必要なpacket再発行なしで採否へ進める。

## External feasibility

status: not-applicable

## Contract

- 受理するtarget locatorとevidenceのold/current sideの意味を親が確定し、untracked sourceと削除diffを正規proofへ結び付ける。
- symbolの存在を無関係な本文の単なる部分文字列一致で代替せず、targetと取得箇所の対応を検証する。
- 証明できない曖昧locatorは理由付きで拒否し、同じ読取を無限に要求しない。

## Must not

- evidence guardを削除したり推測でacceptを許可しない。
- 新規fileを親にstageさせることだけを解決策にしない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- untracked new.go:NewAPIを正常配信したsourceに基づいて検証できる。
- 全削除review.go:2を該当old-sideの削除証拠で検証し、無関係な現行行で代替しない。
- 通常numeric range・symbol、範囲不一致、存在しないsymbol、snapshot変更のnegative caseを維持する。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
