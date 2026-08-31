# Task: 索引からmodel再試行理由とwait形態を直接辿れるようにする

## Original instruction

````text
2,4,5,6は起票していい
作業順は任せる
````

## Amendments

none

## Resolved references

- 「6」は2026-09-01のCodex・GLM統合分析で提示した次の案を指す。承認対象は案2・4・5・6だけであり、本taskは案6の独立責務を扱う。今回の指示は起票と順序決定で、実装開始ではない。
- 共通原証跡はbundle `70028e7d-ef53-4f70-8c7e-765d7fb78ee0.zip`（2026-09-01 05:22頃採取、SHA-256 `6aa50833970f9a44d5ed12c89f47fddd237e34c7904a22d8391c87b5abbfffe7`）。保存先は`/Users/shinderumanm/.glm-worker/exports/4b1083bd6f6e13220f3e0d653377d694f010b8951c788559f19840a14a0df6d0/70028e7d-ef53-4f70-8c7e-765d7fb78ee0.zip`。以下の原証跡pathはbundle内相対pathであり、task telemetryは`task/telemetry/70028e7d-ef53-4f70-8c7e-765d7fb78ee0.jsonl`を指す。
- 承認された案の本文を次に保存する。分析時点の観測であり、current実装との差異は着手時に確認する。
- 「親rollout」はbundle内の`codex-parent/rollouts/01a05946-fa5e-7c32-918a-3f6af8afbac8.jsonl`を指す。

````text
### 6. 索引からmodel再試行理由とwait形態を直接辿れるようにする

優先度: 中。Codex・GLM双方の候補。

- 問題: 原本には`retry_of`・`retry_reason`、callごとのphase/outcome/usage、waitのyield値があるが、索引には主に件数しかない。今回もpacket修正の理由や短いwaitの反復を知るために原本を再parseした。
- 証拠: task telemetry L2のretry edge、`analysis-index.json`のretriesとparent_wait_calls、親rollout L47〜125。
- 範囲: 既存telemetryのcall ID・retry edgeとwait分類を索引へ関連付ける。原本参照を残し、理由不明と因果が観測できる場合を分ける。
- 期待効果: 今回のような改善調査で毎回行う定型parseを減らす。
- 検証: 重複record、session resume、packet correction、auto-fix、欠損retry edge、wait値欠損を含め、原本との一致を確認する。
- 境界: guardian総量・output等の追加fieldは今回一括で増やさず、必要性と帰属定義を別途評価する。旧schemaの恒久互換layerは前提にしない。
````

## Purpose

retry理由やwait形態を調べるために毎回原本を再parseする作業を減らし、観測された因果と不明な関係を索引から区別できるようにする。

## External feasibility

status: not-applicable

## Contract

- 既存telemetryのcall ID・retry_of・retry_reason・phase/outcomeと、親rolloutのwait call/return ID・yield値を索引へ関連付け、原本pathと該当recordへ辿れるようにする。
- 明示IDで観測できるretry edgeだけを因果として扱い、欠損・不明・session resumeを区別する。resume件数をそのままretry回数としない。
- wait形態は観測されたrequested yieldから判別し、値欠損はunknownとする。requested waitと実際の経過時間を同一視せず、実測値を示す場合も原本根拠を持たせる。
- 重複recordを識別して二重計上を防ぐ。usageを関連付ける場合はCodexとGLMそれぞれのschemaと包含関係を保持する。
- 索引schemaと帰属・分類の未確定な意味は実装前に親Codexへ戻す。生成は既存bundleのread-only経路に収め、原本を保持する。

## Must not

- 時刻や近接順だけでretry因果を作らない。waitの件数だけで短いpollと長時間blockingを同一分類しない。
- bundle生成時に新しいmodel call、test再実行、別DB、daemonを追加しない。
- guardian総量・output等の無関係なfieldや旧schemaの恒久互換layerを一括追加しない。案1のwait制御変更へ範囲を拡張しない。

## Acceptance criteria

- packet correction、auto-fix、session resume、重複record、欠損retry edge、wait値欠損を含め、索引が原本のID・理由・値へ一致する。
- 観測されたretry関係と不明な関係、短いwaitと長時間指定waitを、個別原本を再parseせず索引から区別できる。
- 原本保持・read-only収集を維持し、意味契約の親採用、必要なvalidationと独立reviewを通過する。

## Historical invariants

- 親Codexの意味判断・最終採否、独立review、snapshotに対応したvalidation authority、parent-managed metadata guard、GLMのcommit/push禁止を維持する。
- 最上位評価はCodex ReductionとQuality Deltaとし、GLM token削減だけを採用理由にしない。

## Dependencies

none。Planの優先順はhard dependencyを意味しない。

## Review findings

none

## Current boundary

未着手。2026-09-01のユーザー承認により起票した。分析時点の候補と証拠を保存しただけで、調査・実装・新しいmodel callは開始していない。
