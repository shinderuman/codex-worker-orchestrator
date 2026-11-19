# Task: publicationが一意の最新runを順序に依存せず選ぶ

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B07. One fresh run-history inventory per publication decision — #1647
> - **Owner/problem:** `controller/publication_final_validation.go` and `publication_post_install_smoke.go` each scan/parse entire quality-run history per required form/probe, with work approximately V×R or P×R.
> - **Historical rationale:** final Go validation and installed smoke evidence introduced separate latest-run lookups with distinct acceptance predicates.
> - **Implement/minimum boundary:** bounded **read-only per-decision** run-record inventory consumed by distinct validation/smoke selectors; use `qualitygate.Read` and `VerifyTerminalPass`; refresh at every existing final revalidation boundary, never cross-decision cache.
> - **Preserve/risk:** exact HEAD/index/worktree, required form/probe identity, installed digest, newest failure overriding older PASS, equal-time ambiguity and malformed unrelated-record fail-closed handling.
> - **Acceptance:** instrument read count (one inventory scan per decision) and adversarial freshness/malformed/rebind tests without removing any actual validation run or smoke probe.

## Purpose

publicationが一意の最新runを順序に依存せず選ぶ。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

**最新のvalidation/smokeの選択がrun ID順に依存する（B07）。** `controller/publication_final_validation.go::latestFinalValidationRun` と `publication_post_install_smoke.go::latestSmokeProbeRun` は走査途中の同時刻を即座にerrorにする。時刻列「1,1,2」は両者エラー、「2,1,1」は両者成功。実際の最新時刻はどちらも一意。

validationとsmokeはそれぞれform/probeごとに履歴を全読する。各publication判断で履歴を一度だけ取得し、その判断内で個々のselectorが参照する。

## Must not

実装時の現行入口で確認し、旧schemaの推定変換や二重の判定元を導入しない。

## Acceptance criteria

最大時刻を確定した後、その時刻に複数候補がある場合だけ曖昧と判定する。一意の最新runは古い同順位の有無や列挙順で変わらない。最新FAIL優先、最新同時刻の曖昧性拒否、再検証ごとのfresh read、validationとsmoke固有の不在判定を維持する。

履歴の走査回数を計測し、同じ時刻列の全順列、欠損、破損、最新FAILで結果を検証する。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
