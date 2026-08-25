# Task: Greptile日次reviewをLuna Lowの機械dispatchへ縮小する

## Original instruction

`````text
GreptileのタスクもLuna Lowとかでスケジューラ設定して、差分をこのセッションに渡すとかそういう設計にしたほうがCodexのトークンを節約できそうな気がした
ただし今やってるスケジューラと大きく違うのは今やってるスケジューラはGLM-Workerコマンドの1機能だがGreptileはこのリポジトリの開発ルールだということ
その方針で良ければルールを調整してほしい
`````

## Amendments

none

## Resolved references

- 「Greptileのタスク」は現在`codex-config`プロジェクトの専用taskをtargetにしている日次heartbeat automationを指す。
- 「このセッション」は現在の親実装Codex taskを指す。将来親実装taskが変わる場合はscheduler promptの送信先を明示更新し、会話要約から推測しない。
- 本変更はrepository固有の開発orchestration ruleとCodex Desktop automation/task構成だけを対象とし、`glm-worker` production機能へGreptileを追加しない。

## Purpose

Greptile日次reviewの機械的な起動・CLI実行・JSON整理をLuna Lowの専用taskへ限定し、findingの最終採否だけを親Codexへ戻すことで、Sol品質判断を維持しながらscheduled run側のCodex消費を減らす。

## Contract

- `codex-config`プロジェクトに所属するGreptile専用Codex taskをLuna Lowで用意し、既存日次heartbeat automationのtargetをそのtaskだけへ切り替える。
- 専用taskは既存remote checkpoint/main precondition、Greptile Standard CLI実行、JSON/status検証、findingのcompactな正規化、親実装taskへの1回送信、正常review時だけのcheckpoint fast-forwardを担当する。
- 専用taskはfindingを最終採用せず、repository task file作成・Plan変更・production修正を行わない。親Codexが`Greptile finding vs Git現物`、既存reviewとの重複、最上位Quality Deltaへの価値を判断して必要なtask化を行う。
- finding 0件、skip/defer、failureの既存checkpoint semantics、1日1回、未処理runをqueue化せず最後の成功地点からcatch upする契約を維持する。
- Greptile専用taskから親へ渡すpayloadは、review range、run/status、findingごとの原文を失わない要旨・path/line/severity、CLI failure分類、checkpoint更新有無に限定し、diff全文やrepository全体を送らない。
- scheduler promptとrepositoryの恒久ruleを同時に同期し、Greptileが補助reviewerで最終判断者ではないことを維持する。

## Must not

- `glm-worker`のGo source、state、review lifecycle、provider abstractionへGreptileを統合しない。
- Luna Lowへfindingの最終採否、設計判断、production修正、task/Plan編集を任せない。
- Greptile CLI reviewを移行確認のためだけに実行してcreditを消費しない。
- 既存checkpoint ref、remote main、24時間、skip/defer/catch-up、継続停止判断の契約を弱めない。
- 旧Greptile taskと新taskへschedulerを二重登録しない。

## Acceptance criteria

- Greptile専用taskが`codex-config` project所属・Luna Lowで作成されている。
- 日次automationが新専用taskだけをtargetとし、旧taskはscheduler対象外になっている。
- scheduler promptが機械実行・正規化・親送信だけへ縮小され、finding採否・task化は親Codex責務としてrepository ruleにも固定されている。
- automation TOML/保存状態でID、ACTIVE、RRULE、target threadを確認し、重複Greptile automationがない。
- 移行時にGreptile CLI reviewを実行せずcreditを消費していない。
- 旧Greptile taskを不要ならarchiveし、親task・GLM Resume・Codex 5h wakeのscheduler ownershipへ干渉していない。
- parent review後に必要なら個別commitし、pushしない。

## Historical invariants

- 最上位目的は、Sol High相当の品質を可能な限り維持しながらCodex/Sol実消費を大幅に削減すること。
- Greptileはrepository固有の補助reviewerであり、`glm-worker`本体の機能ではない。
- review checkpoint authorityはremote `refs/heads/codex/greptile-reviewed`、review終端はremote `refs/heads/main`である。

## Dependencies

none

## Review findings

none

## Current boundary

要求をtracked化した。次は既存Greptile automation/task実体とrepository ruleを確認し、Greptile CLIを実行せずLuna Low専用taskへのtarget移行を行う。
