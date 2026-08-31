# Task: bundle索引でtask実行・親完了処理・後続依頼の区間を分ける

## Original instruction

````text
2,4,5,6は起票していい
作業順は任せる
````

## Amendments

none

## Resolved references

- 「5」は2026-09-01のCodex・GLM統合分析で提示した次の案を指す。承認対象は案2・4・5・6だけであり、本taskは案5の独立責務を扱う。今回の指示は起票と順序決定で、実装開始ではない。
- 共通原証跡はbundle `70028e7d-ef53-4f70-8c7e-765d7fb78ee0.zip`（2026-09-01 05:22頃採取、SHA-256 `6aa50833970f9a44d5ed12c89f47fddd237e34c7904a22d8391c87b5abbfffe7`）。保存先は`/Users/shinderumanm/.glm-worker/exports/4b1083bd6f6e13220f3e0d653377d694f010b8951c788559f19840a14a0df6d0/70028e7d-ef53-4f70-8c7e-765d7fb78ee0.zip`。以下の原証跡pathはbundle内相対pathであり、task telemetryは`task/telemetry/70028e7d-ef53-4f70-8c7e-765d7fb78ee0.jsonl`を指す。
- 承認された案の本文を次に保存する。分析時点の観測であり、current実装との差異は着手時に確認する。
- 「親rollout」はbundle内の`codex-parent/rollouts/01a05946-fa5e-7c32-918a-3f6af8afbac8.jsonl`を指す。

````text
### 5. bundle索引でtask実行・親完了処理・後続依頼の区間を分ける

優先度: 高。Codex側の追加候補。GLM分析でも区間を手動分離しており、必要性の補強となる。

- 問題: GLM lifecycleのcompleteは05:12:39だが、索引window endは採取時の05:22:59。05:17の拒否に関する質問と05:19の起票・分析依頼も同じparent_token_deltaへ入っている。
- 証拠: `task/lifecycle/70028e7d-ef53-4f70-8c7e-765d7fb78ee0.jsonl` L3、`analysis-index.json`のwindow、親rollout L296/L348。
- 範囲: 原本を保持して、GLM実行区間、親finalization、帰属未確定の後続処理を区別する。GLM completeを親USER_REQUEST完了と同一視しない。
- 期待効果: 別依頼の処理をtask消費に混ぜた改善評価を防ぐ。採取時刻だけで過去taskの実行区間値が伸び続ける状態を避ける。
- 検証: 完了後の再bundle、親処理継続、別ユーザー要求、同じ親sessionの複数task、境界証拠欠損を含めて原本と照合する。
````

## Purpose

別依頼の処理を完了taskの消費へ混ぜず、原証跡に基づいてtask実行と親完了処理を評価できる索引にする。

## External feasibility

status: not-applicable

## Contract

- 既存lifecycle・親rollout・収集記録を用い、GLM実行区間、親finalization、帰属未確定の後続処理、採取範囲を区別する。GLM completeだけで親USER_REQUEST完了を確定しない。
- 境界の根拠と原本参照を保持し、証拠不足はunknownまたは帰属未確定として表示する。時刻の近さやユーザーメッセージの存在だけでtask所有を断定しない。
- 区間別のtoken等の集計は観測できる境界と包含関係を明示する。再採取だけで確定済み実行区間の値が伸び続けないようにし、採取範囲の更新とは分ける。
- 区間・帰属・欠損時の意味とindex schemaの採用を実装前に親Codexへ戻す。既存の証跡で不足する場合に外部producerの新fieldを仮定せず、必要なら別途成立性gateへ戻す。

## Must not

- 原本を削除・切詰め・書換えしない。GLM terminal timestampで親処理を一律に除外しない。
- 新しいlifecycle state machineやtask専用DBを追加しない。欠損証跡の推定補完を事実として集計しない。
- Direct Codex対照runなしでCodex ReductionやQuality Delta、実課金額を算出しない。CodexとGLMのtoken schemaを同じ加算式で扱わない。

## Acceptance criteria

- complete後の親処理、後続ユーザー要求、同じ親sessionの複数task、完了後の再bundle、current/archive、境界証拠欠損を含めて区間と帰属が原本に一致する。
- 同一taskを後から収集しても、確定済み実行区間へ無関係な後続消費を追加しない。未確定部分と採取範囲は別に説明できる。
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
