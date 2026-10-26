# Task: 初回GLMリクエスト前の起動時間を区間別に計測する

## Original instruction

2026-10-27 ユーザー指示（原文）:

````text
じゃあログを仕込むタスクと仕込んだログを見て修正を行うというタスクを起票してPR作ってくれ

ログを仕込むタスクは

- [`IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md`](https://github.com/shinderuman/codex-worker-orchestrator/blob/main/IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md)

の次に、仕込んだログを見て修正を行うタスクはそのタスクの次の次に入れてくれ

仕込んだログは不要であれば修正タスクで消すようにしてくれ
````

## Amendments

2026-10-27 ユーザー確認（原文）:

````text
ログを仕込めば対応できるよな？
````

## Resolved references

- 「最初のGLM呼び出し」: 親が `glm-parent-action start --execution-unit single` を起動してから、最初の実モデル要求が発生するまで。`glm-worker`のtask telemetry記録開始と実際のprovider API送信は同義としない。
- 前回Dogfood Bundleではコマンド実行から初回モデル呼び出し開始の記録まで約381秒、後続decision/fixは約4秒。Taskがactiveになるまでの約378秒が主な未説明区間。これらは調査の入口であり、個別の関数・APIが原因と確定した証拠ではない。
- 初回起動経路の調査入口: `glm-worker/internal/parentactioncmd/start_single.go`, `parentactioncmd.go`, `parent_wait.go`, `glm-worker/internal/app/execution.go`, `glm-worker/internal/workflow/workflow.go` とController admission/activation、`glm-worker/internal/runner/`。実装時はcurrent Gitの実経路を正とする。
- 親の初回待機の診断であり、既存 `IMPLEMENTATION_TASKS/analysis-model-call-event-classification.md` のモデル呼出件数分類、`IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の総合効率評価、`IMPLEMENTATION_TASKS/bundle-host-resource-observability.md` のCPU/メモリ観測とは別責務。

## Purpose

`glm-parent-action start` から最初のGLM実リクエストまでの大きな待機時間について、モデル処理前のどの段階で時間を消費するかを再現可能な実行証拠で特定できるようにする。原因を推測して最適化するのは後続Taskへ分離する。

## External feasibility

status: not-applicable

## Contract

- 正規 `start --execution-unit single` の実入口で、Task生成前から一貫した起動attempt identityとwall-clock/elapsed timingを記録する。Task IDやmodel call IDが後で確定したら同一attemptへ結び付け、親コマンド・worker subprocess・Claude CLIを跨いでも比較可能にする。各プロセス内の経過時間はmonotonicを優先し、別プロセスの時刻はtime origin/clock domainを区別する。
- 主要な時刻境界と処理時間を、実コードのcanonical ownerで観測する。最低限、親コマンド開始、親→worker起動、Controller open/activate/admission/必要なmaterializeやlock待ち、Task初期化、Git/quality baseline取得、repository harness/Task正本解決、quality preflight、repository search/context生成、Runner setup、Claude CLI process起動、最初の実モデル呼び出し・最初のstream/init/応答観測を分離する。処理が行われなかったstageは0msの実行と偽らずnot-runにする。
- 「Claude CLIのinit」「Orchestratorがtelemetryへmodel-call startedを記録」「実provider APIへの送信」「最初の応答」は別イベントとして扱う。CLI/SDKから実送信時刻が観測できないならその境界はunknownと記録し、到達可能な最も近いbefore/after境界と観測限界を明記する。送信時刻の測定のために通信介入・TLS復号・外部proxyを新設しない。
- 異常終了、guard拒否、再入場、既存Runtimeへのattach/rotation等でも、開始済みstageと最終stageを見失わない。成功時だけログを出す設計にしない。初回startと後続decision/fixを混同せず、task/status eventをmodel-call数に加算しない。
- 出力は既存のbundle/telemetry/evidence経路で参照できる小さい構造化recordとし、phase name、開始/終了、経過時間、execution identity、失敗stageを記録する。prompt/response全文、secret、環境変数の値、個人情報、無制限raw stdoutは含めない。既存の観測ownerを優先し、第二state DB・専用daemon・持続的な大容量traceを作らない。
- 新しいログの出力やGit/stat取得の処理自体が待ち時間を増やさないようにする。既存の処理を再実行せず、記録失敗で通常Workflowの成否を改変しない。ただし失敗した計測を完全な証拠として扱わない。
- 可能ならこのTask実装後の通常の初回startをsource-bound evidenceで観測する。実環境で初回起動を観測できない場合は未観測と明示し、後続の修正Taskで観測する。synthetic mockのみで6分の原因を断定しない。

## Must not

- 観測前に推測でControllerやGLM呼出処理を最適化しない。独立した後続の修正Taskを先取りしない。
- Quality/authority/review/admissionの省略、Timeout延長、無許可live実行、secretを含むdebug dump、別model呼び出し、手製command wrapper、汎用profiler frameworkを追加しない。
- 新しい計測eventを実際のモデル呼出として計上せず、既存の実行記録やUsageの意味を変えない。

## Acceptance criteria

- `start`のコマンド入口から初回実モデル要求の最も近い観測可能な境界まで、連続したstage timingsが1つの実行identityで相関でき、全体elapsedと内訳が概ね突合できる。暗黙の長い空白区間を残さない。
- 少なくとも成功・pre-model失敗・中断/再開の関連パスをテストし、未実行stage/欠測/Clock origin差を正しく表現する。後続decision/fixは別区分となる。
- 計測による余分なモデル呼出・Guard回避・顕著な起動時間増がないことを確認し、機微データを出力しない。必要な正規validationと独立Reviewを実施する。
- 採取できた実行traceの正確なlocatorsを次の修正Taskが利用できるよう保存する。未採取なら必要な観測条件を明示する。

## Historical invariants

- 記録の意味を正確に保ち、通常Lifecycle、1 Task 1 Commit、source binding、親Codexの最終意味判断、Current schema、後方互換性禁止を維持する。

## Dependencies

none
