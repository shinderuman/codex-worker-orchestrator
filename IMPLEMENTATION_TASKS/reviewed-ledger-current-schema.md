# Task: review済みledgerのcurrent schema境界

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexがレビューで採用した独立finding:

````text
reviewed blob ledgerはVersion fieldを持つが、loadReviewedBlobRoundsとpromoteLastReviewBlobsがversionを検証しない。version 0や999を現行のreview証拠として受理できる。非現行schemaをreview済み判定へ使用せず、旧形式のmigrationやpromotionを行わない。
````

## Amendments

none

## Resolved references

- `glm-worker/internal/workflow/reviewer_boundary.go`の2つのreaderと`codex/glm-worker/prompts/REVIEWER.md`の`REVIEWED_BOUNDARY`契約が対象。
- 今回はレビューとfinding登録まで。実装修正は開始しない。

## Purpose

review範囲の削減を、現行schemaの検証可能な証拠だけに基づかせる。

## Contract

- JSONL ledgerと直前roundのJSONの双方でcurrent schema/versionを検証する。
- 不正・非現行の記録をreviewedへ昇格させない。安全なreject/skip/rebuild方針を一貫させる。
- 現行schemaでidentityが一致したfileだけを既検証範囲として扱う。

## Must not

- old versionのmigration、補完、promotion、aliasを実装しない。
- 非現行記録しかないfileをreview対象から除外しない。
- 広域のschema frameworkや新規state DBを追加しない。

## Acceptance criteria

- version欠落・0・非現行versionがreview済み判定の根拠にならない。
- current versionの正常なidentity一致判定は成立し、欠損・不一致は未検証扱いとなる。
- JSONL読込と直前roundのledger追記経路を双方検証する。

## Historical invariants

- 後方互換性を持たせない。
- Quality Deltaを悪化させるreview省略を削減策にしない。

## Dependencies

none
