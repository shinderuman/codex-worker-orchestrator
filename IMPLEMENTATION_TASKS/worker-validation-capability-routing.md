# Task: workerの検証実行を既存capability・委譲経路に合わせる

## Original instruction

````text
2,4,5,6は起票していい
作業順は任せる
````

## Amendments

none

## Resolved references

- 「2」は2026-09-01のCodex・GLM統合分析で提示した次の案を指す。承認対象は案2・4・5・6だけであり、本taskは案2の独立責務を扱う。今回の指示は起票と順序決定で、実装開始ではない。
- 共通原証跡はbundle `70028e7d-ef53-4f70-8c7e-765d7fb78ee0.zip`（2026-09-01 05:22頃採取、SHA-256 `6aa50833970f9a44d5ed12c89f47fddd237e34c7904a22d8391c87b5abbfffe7`）。保存先は`/Users/shinderumanm/.glm-worker/exports/4b1083bd6f6e13220f3e0d653377d694f010b8951c788559f19840a14a0df6d0/70028e7d-ef53-4f70-8c7e-765d7fb78ee0.zip`。以下の原証跡pathはbundle内相対pathであり、task telemetryは`task/telemetry/70028e7d-ef53-4f70-8c7e-765d7fb78ee0.jsonl`を指す。
- 承認された案の本文を次に保存する。分析時点の観測であり、current実装との差異は着手時に確認する。

````text
### 2. workerの検証実行を既存capability・委譲経路に合わせる

優先度: 高。Codex・GLM双方の候補。

- 問題: Go cache書込み失敗、socket bind拒否を伴うapp全体test、その後のskip再実行や他package再実行があった。最終的には既存parent validationで全体検証を通している。
- 証拠: worker transcript `claude-transcripts/5588bc3a-0414-42af-b51c-b030e1fd1025.jsonl` L258〜266、L333〜364。app全体test開始04:43:38からskip等の再実行04:48:07まで約4分29秒。ただし全時間を除去可能な損失とみなさない。
- 範囲: 既存のbuild/cache入口とparent validationの適用条件をworkerへ渡し、既知capabilityを必要とする全体検証を適切な入口へ委譲する。環境由来と未知の実装failureを区別する。
- 期待効果: taskごとの環境制約再発見と、成立しない検証の反復を減らす。
- 検証: 同じ検証義務を維持したまま、既知制約の重複失敗が消えること、対象processの終了状態がpipe末尾の成功で隠れないこと、未知のfailureは引き続き失敗として報告されることを確認する。
- 境界: test選択policy・skip一覧の追加、sandbox権限拡大、独自の共有cacheを目的にしない。
````

## Purpose

既知のcache・socket制約をtaskごとに再発見する検証反復を減らし、既存の全体検証義務を維持する。

## External feasibility

status: not-applicable

## Contract

- 既存のbuild/cache入口、working directory、typed parent validationの適用条件を調査し、現行capability内で実行できる検証と親capabilityが必要な検証の責務を整理する。
- 既知制約により成立しない検証は既存の親validation入口へ委譲する。環境由来の既知failureと未知の実装failureを区別し、未確認の失敗を環境問題へ分類しない。
- 対象processのexit statusを保持し、pipe末尾の成功や一部packageの成功で全体検証を成功扱いしない。snapshotに対応した既存validation authorityを維持する。
- 既存instructionとproduction wiringのどこで入口選択が外れたかを証拠で示す。根拠のないchecklist追加で済ませず、未確定の責務変更は実装前に親Codexへ判断を戻す。

## Must not

- test選択policy、skip一覧、sandbox権限拡大、独自の共有cacheを追加しない。
- 全体testやraceの検証義務を省略・縮退しない。失敗を無条件retryや成功扱いで隠さない。
- 案3のreviewerによる古い未検証申告とfresh PASSの整合修正へ範囲を拡張しない。約4分29秒をそのまま削減可能時間と断定しない。

## Acceptance criteria

- 既知cache/socket制約の代表caseで、不成立の同じ検証を反復せず既存の適切な入口へ到達する。
- 未知の実装failure、process非zero、委譲不能は未検証または失敗として識別され、pipe末尾の成功へ吸収されない。
- 変更前と同じ検証coverageを維持し、必要な独立reviewと現snapshotのvalidation、親Codex採否を通過する。

## Historical invariants

- 親Codexの意味判断・最終採否、独立review、snapshotに対応したvalidation authority、parent-managed metadata guard、GLMのcommit/push禁止を維持する。
- 最上位評価はCodex ReductionとQuality Deltaとし、GLM token削減だけを採用理由にしない。

## Dependencies

none。Planの優先順はhard dependencyを意味しない。

## Review findings

none

## Current boundary

未着手。2026-09-01のユーザー承認により起票した。分析時点の候補と証拠を保存しただけで、調査・実装・新しいmodel callは開始していない。
