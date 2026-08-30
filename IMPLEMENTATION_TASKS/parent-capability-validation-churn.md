# Task: parent-capability既知suiteのworker再実行churnを止める

## Original instruction

````text
Codexの節約、Codexの判断、GLMの判断、無駄な行動等多角的な問題をひたすら洗え
````

## Amendments

````text
実装を開始する前に、起票したIssueにCodexにやらせられるタスクがあったらCodexにまわしてくれ
````

## Resolved references

- 既存parent-capability validation実装は、workerが自sandboxで実行不能なrequired validationをtyped requestとして親quality gateへ回し、independent reviewerより前にexact-snapshot validationできる。
- 既存contractはexpected sandbox incapability自体でworker retry churnを起こさず、毎edit後にexpensive full suiteを走らせないことを要求している。
- completed commentlint dogfoodではworkerが早い段階で`internal/app`のUnix socket bind不許可と`internal/workflow`のGit mutation containmentを特定した後も、後続worker roundで`go test ./...`を複数回再実行した。
- transcript上、同じ既知capability failureを含むfull suiteを長時間繰り返し、最終parent-owned full test/raceは別のfixed quality gateで成立している。
- 既存typed parent validation machinery自体は有効なので撤去しない。escaped gapはworker-side validation behavior / instruction / routingである。

## External feasibility

status: not-applicable

## Purpose

worker sandboxで成立不能と機械的に判明したrequired suiteを後続worker roundが繰り返さず、targeted worker validationとparent-owned exact-snapshot validationへ責務を分離してGLM時間・turn数を削減する。

## Contract

- parent-capability validation requestとexisting quality-gate authorityを正として再利用する。
- worker sandboxで実行不能なvalidation formがtyped parent obligationとして成立した後、同じtask/snapshot系列の後続worker callへそのsuiteを再試行させない機械境界を検討・実装する。
- workerが実行可能なfocused package/test、lint、build等まで一律禁止しない。
- snapshot変更後に親validationが必要ならexisting exact-snapshot semanticsで新しいparent gateを要求する。
- sandbox capabilityをfree-form `unverified`、error prose、自然言語分類から推測しない。
- parent validation失敗でworker fixが必要な場合は、failure evidenceをworkerへ返す既存pathを維持する。
- normal taskでparent-only obligationが無い場合のvalidation自由度とmodel call数を悪化させない。

## Must not

- worker sandbox権限を広げてfull suiteを通すことで解決しない。
- `go test ./...`という文字列だけをgeneric禁止するshell parserを作らない。
- every-edit full parent gateへ置換しない。
- reviewer authorityをparent gateで代替しない。
- packet proseをregexしてcapability stateを作らない。

## Acceptance criteria

- regression taskでworkerがparent-capability validation obligationを持つsnapshotを作った後、後続auto-fix/rule-activation/explicit-fix worker callが同じsandbox-incapable full suiteを再実行しない。
- workerが実行可能なfocused testsは継続できる。
- parent exact-snapshot quality gateは必要な時点で1回実行でき、failureは通常fix pathへ返る。
- snapshot mutationで必要なvalidation evidenceがstaleになる既存semanticsを維持する。
- retained commentlint型scenarioで重複full-suite wall time/worker tool churnがmaterially減る。
- full validationと独立reviewを通す。

## Historical invariants

- parent-only capabilityをworkerへ移譲しない。
- parent quality gateはsemantic reviewではない。
- validation evidenceはexact snapshotにbindする。

## Dependencies

none

## Review findings

none

## Current boundary

既存parent-capability validation production wiringは維持し、escaped worker retry-churnだけを修復する。