# Task: Automation response structured inspection

## Original instruction

````text
今後も作業中に改善要素を見つけたら随時タスクに積むように
````

## Amendments

none

## Resolved references

- 2026-09-04のGLM rate-limit auto-resume予約で、Codex app automation createは`isError:false`とexact automation IDを返して成功していたが、親orchestrationがresponse全体へのcase-insensitive `error` substring検査を行い、field名`isError`をfailureと誤判定した
- 誤判定によってACTIVE化前のPAUSED placeholderを残し、別turnで同じIDをupdate/verifyする追加round tripが発生した
- 現行`codex/instructions/glm-auto-resume.md`は「返り値全体を文字列として検査」「`error`を含む場合は失敗」と記述しており、structured `isError:false`応答との両立条件が不足している

## Purpose

Codex app automation応答をfield semanticsで判定し、成功応答内のfield名や否定値をfailure語として誤検出することで生じる停止・追加Sol turn・半端なplaceholderを防ぐ。

## External feasibility

status: not-applicable

## Contract

- repositoryが管理するautomation instructionで、top-level `isError`、content text内のmachine payload、成功message、exact automation ID、期待mode/statusをstructuredに検査する契約を定義する
- `isError:false`、`errorCount:0`等の否定・zero値やfield名だけをraw substringでfailure扱いしない
- `isError:true`、明示的なinvalid/failed response、期待ID/mode/status欠損、malformed/ambiguous responseはfail closedを維持する
- create成功後のupdate/verify/cleanupを同一orchestrationで継続し、成功応答の誤判定でPAUSED placeholderを残さない
- GLM auto-resume以外のrepository-managed automation instructionにも同じ曖昧な文字列判定があれば、同じstructured判定へ揃える

## Must not

- response validationを省略しない
- 任意の非空responseを成功扱いしない
- automation IDをname、時刻、会話memoryから推測しない
- Codex app automationの外部response schemaをrepository runtimeの恒久APIとして複製しない

## Acceptance criteria

- `isError:false`を含む正常create/update responseが成功候補として扱われる
- `isError:true`、明示failure、ID欠損、期待mode/status不一致は失敗する
- 新規createのPAUSED placeholderからACTIVE update、verify、失敗時cleanupまでの既存transaction契約を維持する
- relevant instruction validation、independent reviewer、Sol review、current snapshot validation、commit/install判断を完了する

## Historical invariants

- automation tool responseだけで予約成功とせず、保存実体のverifyを最終根拠にする
- deterministic scheduler stage間で不要なparent returnを増やさない

## Dependencies

none

## Review findings

none

## Current boundary

現在のrate-limited telemetry taskを再起動せず、その完了後に独立taskとして扱う。
