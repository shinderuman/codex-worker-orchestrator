# Task: 分割配信した親review evidenceのcoverage蓄積

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
review proofは1response内で全targetを満たす時しか保存しないが、partial配信は即dedupされる。分割読取後に全件再要求してもproofを作れないため、scopeにbindした配信済みcoverageを合算する。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F10。実装ownerは `glm-worker/internal/parentevidence/review.go の markReviewProof / ReviewClaims、projector.go の saveSurvivingClaims と state/parent_review_evidence.go`。
- 再現資料: `review-evidence/evidence_audit_test.go.txt`。対象test: `TestAuditEvidenceReadAcrossBatchesCanCompleteReview`.

- 保存fixtureはreview時点のapp packageを対象とする。current ownerは上記parentevidenceへ移動済みなので、実装開始時は同じbehaviorの再現を正規ownerへ移す。削除されたapp APIのalias/互換層を復活させない。

## Purpose

bounded readと重複取得抑制を両立させ、親の再読・再説明を減らす。

## External feasibility

status: not-applicable

## Contract

- 同一task/review/lease/snapshotで正常配信したtarget coverageをcall間で保持し、全対象に到達した時だけaccept-readyにする。
- 途中のfailure・budget省略・古いscopeを合算せず、target変更やlease更新時の無効化と中断復旧を定義する。
- 並行readとledger/proofの書込順序を整合させ、未配信bodyのproof化と正規coverageの喪失を防ぐ。

## Must not

- 巨大な一括readやdedup無効化を必須にしない。
- 内容一致だけで別targetへcoverageを推定しない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- 2targetを別callで配信した後に全件coverageが成立し、重複再読を要求しない。
- 全体はbudget超過だが個別配信なら収まる場合を検証する。
- 別review・snapshot変更・未配信part・中断・並行callを含め、誤ったacceptと正規完了不能の双方を拒否する。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
