# Task: 親evidenceのlocatorと本文dedupの分離

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
source evidenceのdigestが本文byteだけであり、別path・別rangeでも同じ内容だと片方が消えてreview proofを作れない。本文転送のdedupと対象identityを分ける。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F9。実装ownerは `glm-worker/internal/parentevidence/projector.go の projectSource / degradeDuplicateParts と review.go の ReviewClaims`。
- 再現資料: `review-evidence/evidence_audit_test.go.txt`。対象test: `TestAuditIdenticalSourceAtDifferentPathsProvesBothTargets`.

- 保存fixtureはreview時点のapp packageを対象とする。current ownerは上記parentevidenceへ移動済みなので、実装開始時は同じbehaviorの再現を正規ownerへ移す。削除されたapp APIのalias/互換層を復活させない。

## Purpose

必要な別位置の証拠を読んだのに採否へ進めない手戻りを除く。

## External feasibility

status: not-applicable

## Contract

- path・range・review/lease/snapshotを含む対象identityを保持し、別位置の同一本文を同じ対象として消さない。
- 本文を再送しない場合は、配信済み本文への参照と対象の対応を機械的に検証してproofへ反映する。方式と永続ledgerの意味は親が確定する。
- budget省略・配信失敗・未知の参照を正常な配信proofにしない。

## Must not

- 周辺行を余分に読ませてdigestを変える回避策にしない。
- review evidence guardを削除してacceptを通さない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- 異なる2pathの同じpackage行、同一pathの別rangeの同一内容の双方を対象としてcoverageを成立させる。
- 同一対象の再要求では不要な本文転送を抑え、別snapshot・別leaseへ証拠を流用しない。
- stdout配信失敗とbudget refinementで未配信本文がproofにならない。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
