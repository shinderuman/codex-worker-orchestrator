# Task: MODEL_IDLEのmodel activity契約を修復する

## Original instruction

````text
2026-08-23 user instruction:

# Codex修正指示：MODEL\_IDLEのmodel activity契約を修復する

`9837850..0d635d9` のレビューで、`3828347` のMODEL\_IDLE修正に1件不整合が見つかった。

今回の問題はworkerが独自に作ったものではない。前回の親指示がmodel activityとして `assistant thinking/text/tool_use` を列挙した際、既存runnerがmodel activityとして扱っている `system/thinking_tokens` を落としていた。さらにtask Contractで「assistant thinking/text/tool\_useだけ」へ狭まったため、その契約に沿った実装が現在の不整合を作っている。

## 問題

runnerでは `system/thinking_tokens` を高頻度model activityとして扱っている。

- event logには保存しない
- ただしmodelが活動しているためidle基準時刻は更新する

一方、現在のwatch側はmodel activityを `assistant thinking/text/tool_use` のみに限定し、live statusのgenericな `LastEventAt` も無視する。

そのため、長時間thinking中に `system/thinking_tokens` が継続していても `MODEL_IDLE` が増え続ける。

前回修正の目的だった「`tool_progress` ではMODEL\_IDLEをリセットしない」は維持すること。

## 修正契約

runnerとwatchで「model activity」の意味を一致させる。

最低限、以下をmodel activityとして扱う。

- `assistant/thinking`
- `assistant/text`
- `assistant/tool_use`
- `system/thinking_tokens`

以下はMODEL\_IDLEをリセットしない。

- `system/tool_progress`
- task notification
- user tool result
- background notification
- resultその他の非model activity

`thinking_tokens` をevent logへ保存する方式には戻さない。

live snapshotにgenericな `LastEventAt` しかないため区別できないので、model activity専用時刻を保持できる最小の構造へ修正すること。generic `LastEventAt` の既存用途は壊さない。

新しいgeneric frameworkやevent分類基盤へ拡張しない。今回必要なproducer/consumer間の契約一致だけを直す。

## テスト

少なくとも以下を追加・更新する。

1. `system/thinking_tokens` がmodel activity時刻を進める
2. `thinking_tokens` は従来どおりevent logへ保存されない
3. `tool_progress` はgenericなactivity時刻を進めてもmodel activity時刻は進めない
4. watchで、
   - assistant activity
   - thinking\_tokens
   - tool\_progress\
     の順に発生した場合、MODEL\_IDLEはthinking\_tokensからの経過時間になり、assistantからでもtool\_progressからでもないこと
5. 既存のPTY、task corpus、watch、runnerを含む関連testを通す

旧live snapshotに新フィールドが存在しないケースのためだけにmigration frameworkや複雑なfallbackを追加しない。

## 原因記録

HISTORY等のescaped-defect記録も、今回の原因をCodex/GLMだけの見落としとして書かないこと。

以下を明示する。

- 前回の親レビュー指示が既存 `system/thinking_tokens` をmodel activity列挙から落とした
- Derived Contractが「少なくとも」を「だけ」へ狭めた
- runnerとwatchをまたぐmodel-activity受理集合の整合確認がreviewで不足していた
- 対策は個別event名の追加だけではなく、producer/consumer間でmodel activityの意味契約を一致させること

現在のACTIVE taskに未コミット変更がある場合、その変更をこの修正へ混ぜない。既存のtask lifecycleとsingle-writer規則を守り、独立したreview-followupとして処理する。

GLMにcommit/pushさせない。pushは行わない。
````

## Amendments

none

## Resolved references

- `9837850..0d635d9`はwatch verbose導入からworkflow telemetry評価完了までのreview範囲。
- `3828347`はMODEL_IDLE、PTY test、task corpus conformanceのreview follow-up修正commit。
- 現在のACTIVEは`IMPLEMENTATION_TASKS/parent-review-outcome-telemetry.md`で、task ID `25f6a48e-e20c-4678-8908-5d3c1707a942`のreviewer-1がrate-limited状態。既存working tree・session・checkpointを保持し、本taskを混在起動しない。

## Purpose

live status producerとwatch consumerが共有するmodel activity受理集合を修復し、長時間thinkingを停止と誤認せず、tool progressではMODEL_IDLEを誤ってリセットしない観測契約を成立させる。

## Contract

- model activity専用時刻をlive snapshotへ最小追加し、generic `LastEventAt`の既存意味と用途を維持する
- `assistant/thinking`、`assistant/text`、`assistant/tool_use`、`system/thinking_tokens`だけでmodel activity専用時刻を進める
- `system/tool_progress`、task notification、user tool result、background notification、resultその他の非model activityではmodel activity専用時刻を進めない
- `system/thinking_tokens`は高頻度抑止を維持し、event JSONLへ保存しないままlive snapshotのmodel activity時刻だけを更新する
- watchのMODEL_IDLEはmodel activity専用時刻を正とし、generic event時刻や表示時刻から推測しない
- 旧live snapshotの新field欠落はunknown/非表示等の単純な安全側挙動とし、migration frameworkや意味推定を追加しない
- HISTORY原因記録は親指示の列挙漏れ、Derived Contractの狭窄、runner/watch受理集合の横断review不足を共同原因として記録する

## Must not

- `tool_progress`でMODEL_IDLEをリセットしない既存目的を後退させない
- `thinking_tokens`をevent logへ再保存しない
- generic event分類framework、migration framework、複雑なfallback、新しいobservability surfaceへ拡張しない
- `--watch`単体またはverbose以外の既存表示・retention・event schemaを不要に変更しない
- 現在ACTIVEのparent review outcome telemetry変更と同じcommit・review・worker sessionへ混ぜない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- `system/thinking_tokens`がmodel activity専用時刻を進めるproduction-path test
- `system/thinking_tokens`が従来どおりevent logへ保存されないtest
- `system/tool_progress`がgeneric activity時刻だけを進め、model activity専用時刻を進めないtest
- assistant activity → thinking_tokens → tool_progressの順で、watch MODEL_IDLEがthinking_tokens基準になるproducer/consumer統合test
- task notification、user tool result、background notification、resultがmodel activityを進めないtable testまたは同等の受理集合test
- 旧live snapshotに専用時刻がない場合の単純な安全側挙動をtest固定し、migrationを追加しない
- PTY、task corpus、watch、runnerを含む関連test、全test/race/vet/build/gofmt
- 独立reviewer、risk/contractに応じて必要なSol品質gate、親Codex commit、本配置、installed/source一致、必要なproduction smoke
- HISTORYへ原因層とproducer/consumer contract修復を記録

## Historical invariants

- `9837850`でverbose watchのlive snapshotを追加し、command/tool本文をevent JSONLへ保存しない境界を固定済み
- `3828347`でtool_progress等の非model eventがMODEL_IDLEをリセットしない方向へ修正済み。この目的は維持する
- escaped原因はworker単独ではなく、親指示・requirement preservation・production wiring・cross-cutting reviewの合成として扱う

## Dependencies

none

## Review findings

- external review: `system/thinking_tokens`がmodel activity専用時刻へ反映されず、長時間thinking中にMODEL_IDLEが増え続ける

## Current boundary

未着手。現在ACTIVEのrate-limited reviewer完了・独立commit後に最優先で開始する。
