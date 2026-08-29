# Task: exhaustive-search query persistence review

## Original instruction

````text
Task 020の独立reviewで、F10由来のexhaustive-search proof event (`glm-worker/internal/workflow/exhaustive_search.go:188-201`) がrequest由来raw queryを`SearchQuery`として保存し続けていることを確認した。Task 020のBM25 route telemetryとは独立した既存surfaceであるためTask 020では変更せず、raw queryのprivacyとexhaustive proof完全性を別taskで評価する。
````

## Amendments

none

## Resolved references

- Task 020 Sol review: BM25 worker/reviewer route eventの新規`SearchQuery`保存は停止するが、F10 exhaustive proof eventはscope外として維持した。

## Purpose

exhaustive-search proof eventにrequest由来raw queryを永続化する必要性とprivacy riskを、F10 proof契約を弱めず判断する。

## External feasibility

status: not-applicable

## Contract

- exhaustive proofの完全性に必要なquery identityと、raw query本文を保存しない代替表現の成立性を既存production evidenceから評価する
- 変更する場合は旧event読取り、exact-once event、full-corpus proofのfail-closed境界を維持する

## Must not

- F10 exhaustive scan、match完全性、truncation/race/error fail-closedを弱めない
- request本文を別field・artifact・logへ移してprivacy問題を迂回しない
- benchmark目的のmodel call、routing/model変更、production A/Bを追加しない

## Acceptance criteria

- raw query永続化の必要性とsecret exposure境界を一次証拠で確定
- Go/No-Goと、Goの場合はproof完全性を保つdeterministic replacementをtest固定
- task scopeに応じたtest/race/vet/build/gofmt、独立reviewer、必要なSol品質gate、commit

## Historical invariants

- F10 exhaustive proofはBM25 top-N navigationと独立し、全corpus scanのenumerated/scanned/skipped件数・predicate・全match pathを証拠化する
- match上限・corpus上限・repository race・scan errorはfail closedを維持する

## Dependencies

none

## Review findings

none

## Current boundary

Task 020でscope外findingとして保存。未着手。
