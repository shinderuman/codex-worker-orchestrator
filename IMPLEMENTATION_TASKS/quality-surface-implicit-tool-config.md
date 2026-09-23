# Task: 品質toolの暗黙設定入力と保護scopeの整合

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
ShellCheckが自動読込する.shellcheckrcはquality surface digestに含まれず、SC2086をdisableして判定をPASSへ変えても親承認境界が検知しない。toolの実際の設定入力と保護scopeを揃える。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F13。実装ownerは `glm-worker/internal/workflow/selfprotection.go の IsQualitySurface、quality_gate.go の captureQualitySurfaceDigest、harnesslint/external.go のexternal tool呼出し`。
- 再現資料: `review-evidence/quality_surface_audit_test.go.txt`。対象test: `TestAuditShellcheckConfigurationIsProtected`.

## Purpose

通常worker自身による品質判定基準の弱体化を防ぐ。

## External feasibility

status: not-applicable

## Contract

- 実際に呼ぶexternal toolの設定探索範囲を調べ、許可する入力を保護scopeへ含めるか暗黙探索を無効化する。採用方針は親が判断する。
- 設定の新規追加・変更・削除・階層配置が判定に与える影響を扱い、worker開始時と結果受理前の境界を整合させる。
- repository固有quality policyはharness ownerに置き、generic workerへtool別の手順を重複実装しない。

## Must not

- .shellcheckrcだけを足して全tool保護済みと主張しない。
- 新たなLLM guardや品質検査の省略を導入しない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- .shellcheckrc追加でSC2086 failureがPASSへ変わる再現を、設定無効化または親承認待ちによって防ぐ。
- 許容する正規設定変更は明示承認後に利用でき、無関係なfile変更では不要な停止を生まない。
- 実tool挙動とproduction digest/approval経路を合成して確認し、path文字列のunit testだけで済ませない。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
