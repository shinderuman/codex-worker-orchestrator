# Task: EVAL責務を評価仕様へ収束する

## Original instruction

````text
TASK

EVAL.mdの責務を整理し、Eval網羅性を維持したまま冗長な実装詳細・運用手順・障害経緯の再掲を削減する。

CURRENT STATE

- `ab28e218` のMarkdown context削減では `EVAL.md` は未変更。
- 現在の `EVAL.md` にはEval contractに加えて、実装仕様、automation具体手順、SQLite/validator等の実装詳細、escaped incidentの長い経緯、test/scenario内部構造の説明が混在している。
- EVALは通常runtimeで全文readしないため、目的はruntime context削減そのものではなくMarkdown責務分離とappend-only肥大化防止。

DO

- `EVAL.md` 全体を監査する。
- Evalとして必要な以下はlosslessに維持する。
  - positive/negative case
  - precondition / expected behavior / invariant
  - escaped bugと対応するregression case
  - 外部挙動として意味を持つliteral・stable identifier
- 以下はEVALから削除・圧縮・適切な既存surfaceへ委譲する。
  - production内部実装の詳細
  - 関数名・validator名・SQLite field等のtest/実装詳細
  - automation等の具体的実行手順
  - incidentの時系列説明・reviewを逃した理由等の長い原因分析
  - 他instruction/scenario/testに正本があるcontractの長文再掲
- scenario名等でescaped bugとの対応を保持できる場合、原因説明本文は短縮する。
- EVALを「scenario / condition / expected result / invariant」中心の評価仕様へ寄せる。
- EVALを参照するtest、grep、exact prose pinがあれば同時に監査し、不要な長文固定を外す。

DO NOT

- Eval caseそのものを減らしてcoverageを弱めない。
- safety/recovery/fail-closed contractを削らない。
- EVALの内容をREADMEやinstructionへ重複移動するだけにしない。
- 新しい巨大Markdownや一般Markdown parserを追加しない。
- 単なる行数削減を完了条件にしない。

ACCEPTANCE

- 必要なpositive/negative/regression caseが維持されている。
- escaped bugとの対応関係を失っていない。
- 実装詳細・運用手順・chronological incident説明・他surfaceとの重複が明確に減っている。
- prose exact-pinによるappend-only圧力を増やしていない。
- 関連testが通る。
- 変更前後のEVAL.md byte数と、削除・維持した情報カテゴリを報告する。

EXECUTION

- 現在のACTIVE taskを中断しない。
- 本件は追加taskとして保持し、Plan/依存関係に従う適切な処理境界で実施する。
- 完了後も既存ACTIVE task/Planを継続する。
````

## Amendments

none

## Resolved references

- `ab28e218`はMarkdown runtime context削減のfinal commit `ab28e21`を指す。
- 「現在のACTIVE task」は要求受領時の`IMPLEMENTATION_TASKS/install-smoke-loop-cost-reduction.md`を指し、本task追加だけを理由に中断・ACTIVE切替しない。

## External feasibility

status: not-applicable

## Purpose

Eval caseとescaped regression対応を維持しつつ、EVALをscenario・condition・expected result・invariant中心の評価仕様へ収束させ、実装詳細や運用経緯のappend-only再掲を減らす。

## Contract

- EVAL全体のcase、precondition、expected behavior、invariant、escaped regression、外部literal/stable identifierを棚卸しして維持する。
- production内部実装、automation手順、incident時系列、review原因、他surface正本の長文再掲を削除または短縮し、別Markdownへ重複移動しない。
- EVAL参照test・grep・exact prose pinを監査し、case coverageではなく冗長な説明文を固定するpinを外す。
- before/after byte数と、維持・削除・圧縮した情報カテゴリを再現可能に報告する。

## Must not

- positive/negative/regression case、safety/recovery/fail-closed契約、escaped bug対応を減らさない。
- READMEやinstructionへの重複移動、新しい巨大Markdown、一般Markdown parserを追加しない。
- 行数やbyte数の減少だけを完了根拠にしない。
- 本task追加を理由に受領時ACTIVE taskを中断しない。

## Acceptance criteria

- EVALの必要caseとescaped regression対応が維持され、scenario・condition・expected result・invariantを追跡できる。
- 実装詳細、運用手順、chronological incident説明、他surface重複が明確に減る。
- prose exact-pinによるappend-only圧力を増やさず、不要な既存pinを削減する。
- before/after bytesと情報カテゴリ差分を報告し、関連test・通常quality gate・独立reviewを通す。
- 親Codexが最終採否し、通常task lifecycleでcommit/installする。

## Historical invariants

- EVALは通常runtimeで全文readしないため、本taskの主目的はruntime context削減ではなく責務分離とappend-only肥大化防止である。
- `ab28e21`のMarkdown runtime context削減で維持したlossless requirement、Plan index、History cold path、必要quality gateを弱めない。

## Dependencies

none

## Review findings

none

## Current boundary

install smoke loop cost削減を中断せずNEXTへ追加済み。既存のcommit authorization、commentlint空行fixより低い優先度で、ACTIVE完了後のPlan順に従う。
