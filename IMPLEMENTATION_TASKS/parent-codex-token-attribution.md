# Task: 親Codex token attribution

## Original instruction

````text
じゃあ022より前のタスクとして全部積んで対応してくれるか
なおCodexのトークン消費は減らしたいがGLMのトークン消費節約の優先度はそこまでではない
GLMのトークン消費を節約するためにCodexのトークンが増えるみたいなのは本末転倒
````

## Amendments

none

## Resolved references

- 「全部」のうち最優先対象は、2026-09-03の親Codexによるtelemetry調査で提案した、GLM task単位の親Codex usageを追加AI callなしで取得するread-only reportである
- 既存bundleは親Codex rolloutを関連付けるが、analysis projectionはinput / cached input中心で、output / reasoning / total、compaction、model/tool turn、tool output量を十分に公開していない

## Purpose

Direct Codex対Codex + glm-workerの最上位Evalを、GLM token proxyではなく親Codexの実消費量で判断可能にする。

## External feasibility

status: not-applicable

## Contract

- 既存bundleのparent Codex identity、session/archived-session探索、rollout parserを再利用し、task ID単位のread-only machine JSON projectionを追加する
- input、cached input、output、reasoning output、total tokenを、GLM execution区間とparent finalization区間を区別して返す
- model turn、tool call/result、compaction、model-visible tool output bytesをboundedなcount/categoryとして返し、raw prompt本文をstdoutへ出さない
- counter reset、複数rollout候補、時刻境界不明、欠損fieldでは推測合算せず、区間またはfield単位でunknown理由とexact source locatorを返す
- 既存bundle analysisと解析ロジックを共有し、別parser、task専用DB、daemonを作らない
- command自身はmodel call、repository mutation、bundle作成を行わない

## Must not

- GLM token削減をCodex token削減の代替指標にしない
- raw prompt、raw response、raw shell command、tool result全文をsummary stdoutへ再投影しない
- cached tokenを無料またはゼロとして扱わない
- attribution不能な複数threadを便宜的に合算しない

## Acceptance criteria

- fixtureでtoken各field、区間、compaction、turn/tool count、output bytes、reset/unknownを検証する
- current telemetry taskに対して追加AI callなしで単一JSON objectを返し、bundle内analysisと矛盾しない
- missing/ambiguous identityは成功値へ縮退せずmachine-readableなunknown/errorになる
- CLI stdout single-object contract、既存bundle、既存statsにregressionがない
- 独立reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- 最上位EvalはDirect Codex対orchestratedのCodex ReductionとQuality Delta
- parent identityとrollout bundlingは既存責務を再利用する

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。親Codex tokenを最優先指標として実装する。
