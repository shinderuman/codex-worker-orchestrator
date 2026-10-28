# 実行区間の計測

`glm-worker --stage-timeline [task-id]` は既存のTask telemetry・lifecycle・event log・quality gate run recordと親actionのattemptを相関する読み取り専用入口である。Task IDを省略した場合は現在のTaskを参照する。実行の追加やモデル呼出は行わない。

`entries` は実際に観測した区間とexact locatorを返す。親command、workerの処理、Claude process、CLI init、最初のmodel activityを区別する。provider API送信時刻はCLIのinitやtelemetry開始で代用せずunknownとする。親commandとworkerの相関はattempt ID、runnerの相関はmodel call IDを使い、時間の重なりだけでは帰属しない。

`wall` は観測された最初と最後の境界間の時間であり、未収録の最終ユーザー報告までの時間を推測しない。`measured_union_ms` は重複区間を一度だけ数える。`overlap_ms` は重複分であり追加遅延ではない。`gaps` は観測区間間の空白、`inner_unmeasured_gaps` は親command内部で詳細の計測がない区間を表す。両者を単純加算しない。`slowest` は絶対時間順の調査候補であり、固定閾値による異常判定や原因確定ではない。

`unobserved_stages` は記録がない区間であり、未実行を意味しない。実際に処理を省略した場合だけ区間の`outcome`を`not-run`とする。途中中断したspanは開始記録を残し、終端と所要時間は欠測のままにする。`coverage` と各`source`の状態は、欠損・破損・収集境界外を示す。親Codexのturn・最終報告・CI実行は、それぞれの既存transcript/CI証跡で照合する。

`tool_totals` とretry情報は既存event log/telemetryから取得する。Git push・remote observeはPublicationの既存ownerで別々に計測し、検証runはsource SHA/digestとrun IDへ束縛する。`overhead`は観測書込のwallとI/O回数を示す。CPU、最終summary append、親attempt ledger書込等の未計測分はunknownとして明記する。

区間記録は既存telemetryとbundleのruntime state収集に含まれる。Task生成前のarchiveと親attempt記録は有限件数の診断資料であり、完全な長期履歴ではない。計測失敗は通常Workflowの成否を変更しない。初回startの実観測には、この実装を配置した後の通常Task開始が必要であり、過去traceやmockから起動遅延の原因を断定しない。
