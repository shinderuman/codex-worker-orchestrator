# Task: sol_questionのstatus contract不整合を修正する

## Original instruction

````text
優先順位はお前が決めろ

# Codex修正指示：sol_questionのstatus contract不整合を修正する

`0d635d9..202bc92` のレビューで、`202bc92` のCodex-facing machine JSON化に1件不整合が見つかった。

現在のACTIVE taskを進める前に、review follow-upとして独立して修正すること。

## 問題

`packet.Result` のstatus contractでは、NEEDS_SOL_REVIEWの必須text fieldに `sol_question` を含め、

- 空不可
- 改行不可
- 1536 bytes以内

を全契約text fieldの共通制約としている。

しかし現在の `ValidateReviewerResult` は通常のreviewer fieldを `validateFields()` へ渡す一方、`sol_question` だけ `TrimSpace(...) == ""` で個別検査している。

そのためNEEDS_SOL_REVIEWで、

- 改行を含む `sol_question`
- `MaxFieldBytes` を超える `sol_question`

がfield単位では受理される。

また `sol_question` だけ、

- validation
- MachineJSON
- human Display

の各所でstatus判定をspecial-caseしており、Task 006で導入したstatus別contract field集合が単一の正になっていない。

PASS / FIX_REQUIREDへ混入した `sol_question` を無視してmachine outputへ出さない現在のstatus境界は正しいので維持すること。

## 修正

NEEDS_SOL_REVIEW時の `sol_question` を、他の必須text fieldと同じcontract field定義・validation経路へ統合する。

status別contract field集合を正として、

- PASS / FIX_REQUIRED: reviewer共通fieldのみ
- NEEDS_SOL_REVIEW: reviewer共通field + sol_question

となる最小構造にする。

可能なら `MachineJSON()`、`Display()`、validatorが同じstatus別field集合を参照し、`sol_question` の個別special-caseを減らすこと。

generic schema frameworkや新しい抽象化を作らない。

## テスト

少なくとも以下を追加する。

1. NEEDS_SOL_REVIEWの `sol_question` に改行があればconstraint error
2. `sol_question` が `MaxFieldBytes` を超えればconstraint error
3. `MaxFieldBytes` ちょうどは受理
4. 空・空白のみは従来どおり拒否
5. PASS / FIX_REQUIREDへ混入した `sol_question` は従来どおり契約外fieldとしてmachine JSON / Displayから除外し、不要なvalidation対象にもならない
6. status別contract field集合とvalidator / MachineJSON / Displayの受理集合が再びずれないことを直接検証する
7. 関連testに加え、既存の全test / race / vet / build / gofmtを通す

## 原因記録

今回もCodex/GLMだけの見落としとして記録しない。

原因は少なくとも以下。

- 元の親Task 006指示は `sol_question` をfield audit対象に含め、status別contract・schema/validator acceptance一致を要求したが、全status text fieldを同一validation sourceへ収束させるところまで具体化していなかった
- 実装では `sol_question` のstatus-scopingをspecial-caseしたため、共通 `contractField` の外に残った
- reviewer / Sol gateはPASS/FIX_REQUIREDへのnoise除去とmachine JSON出力を確認した一方、status tableに書かれた「全契約text fieldの共通制約」とvalidator受理集合の横断照合を落とした

個別にnewline checkを1個追加するだけで終わらせず、今回すでに導入したstatus別contract field集合を単一の正として収束させること。ただしTask 007以降へscopeを広げない。

現在のworking treeに別taskの未コミット変更がある場合は混ぜない。既存task lifecycleに従ってreview follow-upを処理し、独立reviewと必要なSol品質gateを通した後、親Codexがcommitする。

GLMにcommit/pushさせない。pushしない。
````

## Amendments

none

## Resolved references

- `202bc92`はTask 006完了同期後の当時HEADで、後のfinal amendにより現在の対応commitは同内容の`202bc92`後継HEADとなった。
- 「現在のACTIVE task」は`IMPLEMENTATION_TASKS/007-machine-only-legacy-cleanup.md`を指す。

## Purpose

`sol_question`をNEEDS_SOL_REVIEW固有のcontract fieldとして共通field定義へ統合し、validator・machine output・human projectionの受理集合を一致させる。

## Contract

- status別contract field集合を単一の正とし、NEEDS_SOL_REVIEWだけreviewer共通fieldに`sol_question`を追加する
- 全contract text fieldへ空・改行・`MaxFieldBytes`制約を同じvalidation経路で適用する
- PASS / FIX_REQUIREDのnoise `sol_question`はvalidation対象外かつmachine JSON / Displayへ非出力とする
- Task 007のlegacy cleanupへscopeを広げず、保全した未コミット変更を混ぜない

## Must not

- newline checkだけの局所追加でstatus別field集合の不一致を残さない
- generic schema frameworkや新しい抽象化を導入しない
- Task 007の変更を同じcommitへ混ぜない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- newline、上限超過、上限ちょうど、空・空白だけの`sol_question`境界test
- PASS / FIX_REQUIREDのnoise非検証・非出力test
- validator / MachineJSON / Displayが同じstatus別contract field集合を参照する直接test
- 全test / race / vet / build / gofmt、独立review、必要なSol品質gate、親Codex commit
- escaped原因を親指示・derived contract・実装・review/Sol gateの各層へ分けてHistory候補として報告

## Historical invariants

- Task 006のCodex-facing machine JSON、PASS/FIX_REQUIRED noise除外、status別contract field table

## Dependencies

none

## Review findings

- NEEDS_SOL_REVIEWの`sol_question`が共通text field validation経路外に残っている

## Current boundary

Task 007を中断して先行するreview follow-up。Task 007の未コミット変更は別境界へ保全してから実装を開始する。
