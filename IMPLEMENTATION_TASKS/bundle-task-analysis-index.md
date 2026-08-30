# Task: bundleにtask単位の分析用索引を付ける

## Original instruction

````text
6だけActiveの次のタスクとして積んでコミットしておいてくれ
````

## Amendments

none

## Resolved references

- 「6」は2026-08-31のbundle調査で親Codexが提示した次の改善候補だけを指す。

````text
6. **bundleにtask単位の分析用索引を付ける**

   親rolloutは約32.4MBですが、今回のtask開始以降は約1.77MBです。検証runも23件中17件がtask開始前のものです。現状も帰属不明の表示はありますが、分析時に毎回切り分けが必要です。

   原本を保持したまま、対象区間・検証runとの対応・待機回数・入力とcachedの増分・再試行理由をまとめると、次回以降の改善調査そのものを短縮できます。
````

- 観測元はtask `314db004-148f-4b7d-b0e4-4a6dff90aa3a`のbundle。調査時のローカル保存先は`/Users/shinderumanm/.glm-worker/exports/4b1083bd6f6e13220f3e0d653377d694f010b8951c788559f19840a14a0df6d0/314db004-148f-4b7d-b0e4-4a6dff90aa3a.zip`。
- bundle内の`task/task-stats.json`、`task/events/`、`task/telemetry/`、`codex-parent/rollouts/`、`current-state/diagnostics/quality-gate-runs/`とmanifestを照合した観測である。時刻の重なりだけをtask所有の証明とはしない。
- 当時の候補①待機再入、②テスト反復、③finalize-checkのcwd、④規則適用ラウンド、⑤task diff欠落の実装修正は本taskへ含めない。今回のcommit指示はtask登録への指示であり、本taskの実装開始を意味しない。

## External feasibility

status: not-applicable

## Purpose

bundleを分析するたびに親Codexが行っているtask区間と証跡の切り分け・集計を機械化し、原本へ遡れるcompactな分析入口を提供する。

## Contract

- 既存bundleをcanonicalな分析成果物として使い、原本を保持したままtask単位の索引を同梱する。
- 対象task・親session・分析区間・参照するarchive内の証跡位置と帰属根拠を示し、task外または帰属不明の証跡を区別する。
- 検証runとの対応、待機回数、親入力tokensとcached入力tokensの増分、証跡から確認できる再試行理由をまとめる。数値の対象区間と集計単位を明示する。
- 既存のtask identity・event・telemetry・usageを再利用し、観測重複やtaskをまたぐ累積値を当該taskへ二重計上しない。欠損や曖昧な帰属・理由は不明として明示する。
- bundle生成はrepository/task lifecycleと原証跡に対してread-onlyを維持する。索引を生成するための追加model callや検証suite再実行は行わない。

## Must not

- 索引のために原本を削除・上書き・切り詰めない。
- 時刻だけから検証runのtask所有や再試行理由を断定しない。
- cached入力を通常入力と重複加算したり、token観測量を料金・実課金量と同一視したりしない。
- task独自の永続DB・daemon・第二のlifecycle state machineを追加しない。
- 他の改善候補の実装や通常のworker/reviewer・validation方針変更へ範囲を広げない。

## Acceptance criteria

- 索引から対象区間と関連証跡を辿れ、task外または帰属不明の証跡と区別できる。
- 待機回数・入力/cached増分・検証run対応・確認可能な再試行理由を、定義した集計単位に従って原本と照合できる。
- 複数taskを含む親rollout、過去の検証run、usageの重複記録・欠損を含む代表例で、対象範囲と不明値の扱いを検証する。
- 索引追加前後で原本の内容と既存task/lifecycle stateが変わらず、追加model callを必要としない。
- 実bundleまたは同等の保存済み証跡で、従来の手動切り分けを索引から行えることを確認する。

## Historical invariants

- bundleの明示identityに基づく証跡関連付けと、missing/unattributedの区別を維持する。
- 原本は証拠の正とし、分析索引をtask要求・validation・semantic acceptanceのauthorityへ昇格させない。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。分析用索引の要求を登録した段階であり、schema・配置・集計実装は未確定。
