# Task: timeline retention fallback

## Original instruction

````text
じゃあ022より前のタスクとして全部積んで対応してくれるか
なおCodexのトークン消費は減らしたいがGLMのトークン消費節約の優先度はそこまでではない
GLMのトークン消費を節約するためにCodexのトークンが増えるみたいなのは本末転倒
````

## Amendments

none

## Resolved references

- 2026-09-03調査で`glm-worker --timeline <task-id>`は旧taskのevent logがretention済みだと、残存telemetryがあっても全体失敗した

## Purpose

保持済みartifactだけで部分timelineを得られるようにし、親Codexによるraw filesystem探索を減らす。

## External feasibility

status: not-applicable

## Contract

- event log欠損時も残存telemetry / stateから証明可能なtimeline部分を返す
- 完全timeline、partial、unknownをmachine-readableに区別し、missing source、coverage、exact locatorを返す
- eventとtelemetryの時系列統合は既存ID/time authorityに限定し、推測したeventを生成しない
- read-only、追加AI callなし、repository mutationなしとする

## Must not

- retention済みeventをtelemetryから捏造・復元しない
- partial結果をcompleteとして返さない
- raw record全文をstdoutへ出さない

## Acceptance criteria

- eventあり、eventなし/telemetryあり、双方なし、malformedのfixtureを検証する
- 旧taskでpartial statusと利用可能なrecordsが単一JSON objectとして得られる
- current timelineとCLI error contractにregressionがない
- 独立reviewer、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- expected field欠損はunknown/errorのまま扱いwhole-document fallbackをしない

## Dependencies

none

## Review findings

none

## Current boundary

history cohort query完了後に実行する。
