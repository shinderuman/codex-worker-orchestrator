# Task: Decision rate-limit resume coherence

## Original instruction

````text
さっきの「意味のある停止」というのが何なのか知らないが、そういうのを改善したほうがいいと思うなら随時タスクに積んでいくように
今後も作業中に改善要素を見つけたら随時タスクに積むように
022の前での再評価タスクでも改めてタスクに積むべきものがなかったか精査するように
````

````text
その失敗を再現しないようにするタスクはあるのか
````

````text
そっちの話じゃねえよ
この件に限らず改善タスクは積んでいけって言ってるだろ
````

## Amendments

none

## Resolved references

- 2026-09-05、task `8c2b5597-84a1-4fe1-be86-b1c591ea14c1`はSol decision受領後の`worker-decision`中にZ.ai 5h rate limitへ停止した
- resume checkpointは`phase=worker-decision`、`stop_kind=rate-limited`、decision本文保持、task statusは`rate-limited`、`pending-decision=true`だった
- `glm-parent-action resume`はmodel呼出前に`lifecycle inconsistency for task status rate-limited: stopped task status does not match pending decision, parent review, and resume checkpoint`で拒否された
- canonical handoffは`consistent:false`、`allowed_actions:[]`を返したため、standalone resume、pending marker削除、resetでは迂回していない
- 一次原因は`BeginParentDecision`がworker結果確定まで`pending-decision`を保持し、rate-limit checkpoint保存もrate-limit時には同markerを保持する一方、`state.stoppedActionPlan`が全停止phaseで`pending=true`を一律矛盾と判定する契約不整合である

## Purpose

Sol decision継続中にrate limitまたはprovider停止が発生しても、decisionと同一worker sessionを保持した正規resumeを可能にし、異なるpending stateの誤受理は防ぐ。

## External feasibility

status: not-applicable

## Contract

- stopped taskのpending decision可否をcheckpoint phase・decision identity・task statusと対応付け、`worker-decision`継続に必要な正当stateだけをresume可能にする
- rate limit、provider unavailable、明示interruptの各停止経路で同一decision・同一session・同一checkpointを保持する
- handoffとparent-action admissionが同じstateを`consistent:true`、`required_action:resume`として返す
- worker結果確定後は既存どおりpending decisionを解消し、別phaseのpending marker、欠損decision、stop kind不一致はfail closedする
- 現在停止中taskをreset・新規session化せず、修正runtimeで同じcheckpointから再開できるmigration不要な修復経路を成立させる

## Must not

- stopped stateの`pending-decision=true`を無条件許可しない
- state fileの手動削除、reset、standalone `glm-worker --resume`を正常復旧経路にしない
- decision本文やsessionを会話・時刻・唯一のtaskから推測しない
- 現在の停止中taskを再起動・再実装しない

## Acceptance criteria

- `worker-decision`のmodel callがrate limitへ停止し、`glm-parent-action resume`相当のadmissionから同一checkpointで継続するintegration testがある
- provider unavailable・interruptの同等経路と、通常worker/reviewer停止の既存resumeを壊さない
- decision checkpoint欠損、phase不一致、stop kind/status不一致、無関係なpending markerはmodel呼出0回でfail closedする
- current stopped task `8c2b5597-84a1-4fe1-be86-b1c591ea14c1`を保持したままsource/runtime修正後にcanonical handoffがresumeを許可し、実resumeできる
- independent reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- GLM worker/reviewerにGit remote write authorityを付与しない
- Sol decisionは親Codexが確定し、GLMは保存済みdecisionから継続する

## Dependencies

none

## Review findings

- current production runtimeではdecision継続中rate-limitの正当stateをparent-action admission自身が拒否し、auto-resumeが必ず失敗する

## Current boundary

現在のACTIVE taskを復旧するためのblocking defectである。現taskのcheckpointを保持し、修正を最優先で適用してから同じtaskをresumeする。
