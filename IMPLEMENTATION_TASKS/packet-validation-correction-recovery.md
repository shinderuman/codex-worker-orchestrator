# Task: packet validation correction recovery

## Original instruction

````text
じゃあ022より前のタスクとして全部積んで対応してくれるか
なおCodexのトークン消費は減らしたいがGLMのトークン消費節約の優先度はそこまでではない
GLMのトークン消費を節約するためにCodexのトークンが増えるみたいなのは本末転倒
````

## Amendments

none

## Resolved references

- 2026-09-03調査で、最初のpacket correction後に別の独立制約違反が判明したが、correction 1回制限により同じACTIVE taskが別GLM task IDで再dispatchされた事例を確認した
- 全telemetryではinvalid packetが25/433 callsあった

## Purpose

strict packet contractを維持しながら、機械検証可能な複数違反の逐次露出による親Codex再dispatchと再判断を減らす。

## External feasibility

status: not-applicable

## Contract

- packet validatorが同一inputから独立に検出可能なsemantic/schema/size violationを最初のcorrectionへboundedに集約する
- 最初の修正でのみ新たに表面化し、同一session/snapshot/taskで機械判定可能な違反に限り、追加correctionを許可するかを設計・実装する
- retry上限、same task/snapshot binding、no production diff mutation、structured outcomeを維持する
- 成功指標は親Codex再dispatch/turn/tokenが増えないことを第一とし、GLM token削減単独を採用理由にしない

## Must not

- packet schema、size上限、required evidence、parent metadata guardを緩和しない
- generic conversation retry、無制限correction、別snapshot/sessionへの補正を許可しない
- 追加correctionの判断に親CodexまたはGLMの追加semantic callを要求しない
- GLM tokenを減らすためにCodex側へ違反解析・prompt再構成の反復作業を移さない

## Acceptance criteria

- 複数同時違反、一回目修正後の新規機械違反、同一違反反復、snapshot変更をfixtureで検証する
- strict validatorのreject coverageを維持し、bounded correction以外はfail closedする
- parent actionが新task再dispatchを要求する代表scenarioを同一lifecycle内で収束できる
- Codex token/turn増加がないことをparent attributionまたは機械proxyで確認し、確認不能ならGLM token削減だけで採用しない
- 独立reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- packet/result semantic contract、single-object stdout、parent-managed metadata guardを維持する

## Dependencies

- `IMPLEMENTATION_TASKS/parent-codex-token-attribution.md`

## Review findings

none

## Current boundary

Codex token優先度が低い可能性を踏まえ、他の観測改善後に実行する。
