# Task: quality gateを必要capabilityに合う実行境界で一度だけ実行する

## Original instruction

````text
sandbox内ではUnix socket作成が禁止され、app系testだけ環境由来で失敗しました。

これいつも同じことしてるけど改善して
````

## Amendments

none

## Resolved references

- 「これ」は、親Codex最終gateで`go test ./...`をsandbox内実行し、Unix socketの`bind: operation not permitted`でapp系testを多数失敗させた後、同じ全testをsandbox外でもう一度実行した反復を指す。
- 「いつも同じこと」は、このrepositoryのapp/stop endpoint等がUnix socketを必要とすることを既に把握しているにもかかわらず、親verificationでsandbox内の成立不能な全suiteを先に走らせる再発を指す。

## External feasibility

status: not-applicable

## Purpose

quality gateが必要とする既知capabilityと実行sandboxを事前に対応付け、成立不能な環境で全suiteを一度失敗させてから同じ証拠を取り直す反復をなくす。

## Contract

- 親Codex・GLM worker/reviewer・既存test instructionのquality gate起動経路を棚卸しし、Unix socket等の既知capabilityを必要とするgateがどの境界で実行されるかを一次証拠で特定する
- 既知のsandbox制約で成立しないgateは、最初から必要最小限の許可を持つ既存実行境界へ一度だけdispatchする決定論的contractへ収束する
- sandbox内で安全に成立するgateまで無条件にsandbox外へ出さず、外部実行の範囲とcommand prefixを最小化する
- capability不足による環境失敗と実装不具合を区別し、前者の既知再実行を減らしても後者をfail closedで検出する
- 同一snapshotの同一quality evidenceを環境選択ミスだけで二重取得しない
- prompt上の注意だけでなく、既存instructionのrouting、production command、またはdeterministic testのうち最小で再発を防げる境界を選ぶ

## Must not

- 全commandを理由なくsandbox外実行へ変更しない
- test failureをsandbox由来と推測してskip、成功扱い、acceptance緩和しない
- Unix socketだけのhardcodeを増殖させる汎用sandbox frameworkを作らない
- quality gate、race、vet、build、lint等の必要証拠を削減しない
- 同一suiteをsandbox内失敗後にsandbox外再実行する現行反復を運用注意だけで残さない

## Acceptance criteria

- 現在の全Go testが必要とするUnix socket capabilityを、成立不能なsandbox内で先に実行せず、適切な既存境界で最初から一度だけ実行できる
- sandbox内で成立するgateは従来どおり最小権限で実行される
- capability不足と実test failureの受理集合が明確で、実不具合を環境失敗として隠せない
- 代表的な親final gateで、改善前の「失敗1回＋再実行1回」が「有効な実行1回」になることを一次証拠で確認する
- worker/reviewer/親Codexの通常経路に同じ反復が残らないことを確認する
- 関連test・独立review・必要なSol gateを通す

## Historical invariants

- quality evidenceの削減ではなく、同じ有効証拠を不適切な環境で失敗させて再取得する無駄の削減を目的とする。
- full smoke PASS証拠再利用とmachine execution反復cost観測の既存contractを置き換えず、既知の実行環境選択ミスを起動前に防ぐ実行routingとして補完する。

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。既知capabilityに基づくquality gate実行routingを調査し、同一suiteのsandbox失敗後再実行を一度の有効実行へ収束する。
