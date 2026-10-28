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

2026-10-29 ユーザー追加指示（原文）:

````text
[https://github.com/shinderuman/codex-worker-orchestrator/blob/main/IMPLEMENTATION_TASKS/initial-glm-startup-stage-timing.md](https://github.com/shinderuman/codex-worker-orchestrator/blob/main/IMPLEMENTATION_TASKS/initial-glm-startup-stage-timing.md)
[https://github.com/shinderuman/codex-worker-orchestrator/blob/main/IMPLEMENTATION_TASKS/initial-glm-startup-latency-remediation.md](https://github.com/shinderuman/codex-worker-orchestrator/blob/main/IMPLEMENTATION_TASKS/initial-glm-startup-latency-remediation.md)

これにその内容も確認するように修正してPRを作れ
それ以外の異常に時間がかかっているやつもやれ
と言うかなんでそれをAuditで確認できなかったの？
初期化の指摘も俺が見つけたよね？
その後から時間の確認するようにしたよね？
じゃあお前のAuditってなんなの？
言われたことしかできないの？
むしろ言ったことすらできてないの？
````

## Resolved references

- 「最初のGLM呼び出し」: 親が `glm-parent-action start --execution-unit single` を起動してから、最初の実モデル要求が発生するまで。`glm-worker`のtask telemetry記録開始と実際のprovider API送信は同義としない。
- 前回Dogfood Bundleではコマンド実行から初回モデル呼び出し開始の記録まで約381秒、後続decision/fixは約4秒。Taskがactiveになるまでの約378秒が主な未説明区間。これらは調査の入口であり、個別の関数・APIが原因と確定した証拠ではない。
- 初回起動経路の調査入口: `glm-worker/internal/parentactioncmd/start_single.go`, `parentactioncmd.go`, `parent_wait.go`, `glm-worker/internal/app/execution.go`, `glm-worker/internal/workflow/workflow.go` とController admission/activation、`glm-worker/internal/runner/`。実装時はcurrent Gitの実経路を正とする。
- 親の初回待機の診断であり、既存 `IMPLEMENTATION_TASKS/analysis-model-call-event-classification.md` のモデル呼出件数分類、`IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の総合効率評価、`IMPLEMENTATION_TASKS/bundle-host-resource-observability.md` のCPU/メモリ観測とは別責務。

- 2026-10-29追加の「その内容」は、初回GLM起動だけでなく、Task終盤の親完了待ち・最終Go検証・配置後smoke・Publication・terminal後のCodex再確認を含む長時間区間。初回起動の既知遅延だけを計測して全体問題を解決済みとしない。過去Dogfoodの監査記録/Bundleには、親の完了待ちが約20分に達し、Goテストとsmoke以外の時間内訳が不明な事例がある。これは原因ではなく追加計測の根拠であり、具体数値・SHA・区間の正本はraw Bundle/telemetryに置く。
- 「それ以外の異常に時間がかかっているやつ」は事前に工程名を決め打ちせず、実行のend-to-end critical pathから高寄与の長時間待機・繰返し・再試行・追加検証を発見すること。既存のPublication段階往復評価、Codex総合効率評価、Git subprocess削減、Publication後の任意再探索抑制とは計測責務を区別し、重複した実装ownerを作らない。
## Purpose

`glm-parent-action start` から最初のGLM実リクエストまでの大きな待機時間に加え、**通常Taskの開始から最終完了・Publication terminal・ユーザーへの完了報告まで**を区間別に測り、モデル・親Codex・machine command・Git・検証・待機のどこが長時間化したかを再現可能な実行証拠から発見できるようにする。初回startだけ測って、Review後・Publication中・成功後の長い空白を見逃さない。原因を推測して最適化するのは後続Taskへ分離する。

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

- 同じTask/source identityで**end-to-endのstage timeline**を構成する。初回起動後のWorker/Reviewer各call、親Codexへのhandoff・decision待ち、追加rule/auto-fix/retry、final validation（通常Go/race等）、install/smoke、候補promote、Publication prepare/push/remote observe/acknowledge、Runtime complete、terminal出力後の親ツール再確認/最終ユーザー報告まで、実際に通った境界だけを観測する。プロトコルに存在しないstageを新設したり、各Taskで無条件に全stageが走ったと仮定しない。
- Publicationの「コマンド内部実処理時間」と「親Codexが次コマンドを呼ぶまでのmodel turn/tool往復」「Git remote通信」「CI実行/待ち」「成功後の追加探索」を区別する。各stageの開始・終了・結果・親/子対応、モデル呼出数、重複実行やretry原因、ツール起動回数を既存記録から相関し、同時進行/非同期サンプル時間をwall-clockへ単純加算しない。
- 親モデル応答やPublication以降がBundleの収集境界外なら「未観測/partial」と明示する。Task lifecycleとparent transcriptの被覆開始/終端を別々に記録し、final stageを推測で補完しない。必要な時刻証拠は既存parent rollout・command telemetry・Git/CI/Bundlesとのexact locatorで照合し、重複収集を避ける。
- Task全体elapsedと各stageのcritical pathを突き合わせ、**計測できていない時間・長時間の無動作区間**を明示する。ベースラインが得られるstageは同条件で比較し、比較不能でも実測の長い空白は調査候補として記録する。initialization以外に異常な高コストstageがあれば、検証PASSや最終Task Completeを理由に検出結果から除外しない。閾値を思いつきで固定せず、絶対時間、同条件比較、Task wallへの寄与、再現頻度を分けて示す。
- 追加する計測は既存の正規入口とcanonical ownerで最小限にする。既存のGo/quality test、sample collector、git subprocess、wrapperを計測目的だけで再実行せず、採取自体のwall/CPU/I/O overheadも記録する。安全性・source/Task identity・実際のmodel call分類を維持する。
## Must not

- 観測前に推測でControllerやGLM呼出処理を最適化しない。独立した後続の修正Taskを先取りしない。
- Quality/authority/review/admissionの省略、Timeout延長、無許可live実行、secretを含むdebug dump、別model呼び出し、手製command wrapper、汎用profiler frameworkを追加しない。
- 新しい計測eventを実際のモデル呼出として計上せず、既存の実行記録やUsageの意味を変えない。

- 初回GLM呼出の時刻だけ計ってTask全体やPublicationの計測を完了したと報告しない。Goテスト・smoke・Controller確定・親Codex待ちの所要時間を一括して「Publication処理時間」と命名しない。
- Bundle/parent transcriptの未収録期間をゼロ時間としたり、CI開始より前の待機をCI待ちだと推測したり、並行した采集時間をend-to-endの追加遅延と誤認しない。
## Acceptance criteria

- `start`のコマンド入口から初回実モデル要求の最も近い観測可能な境界まで、連続したstage timingsが1つの実行identityで相関でき、全体elapsedと内訳が概ね突合できる。暗黙の長い空白区間を残さない。
- 少なくとも成功・pre-model失敗・中断/再開の関連パスをテストし、未実行stage/欠測/Clock origin差を正しく表現する。後続decision/fixは別区分となる。
- 計測による余分なモデル呼出・Guard回避・顕著な起動時間増がないことを確認し、機微データを出力しない。必要な正規validationと独立Reviewを実施する。
- 採取できた実行traceの正確なlocatorsを次の修正Taskが利用できるよう保存する。未採取なら必要な観測条件を明示する。

- 少なくとも**初回startからTask complete/terminal/最終報告まで**の観測可能な実行stageを、各stage実行時間・親待機・未計測gap・overlap・source identity付きで一覧化できる。約20分の親完了待ちのような高コスト区間で、最終Go検証、install/smoke、Controller内部のPublication、親の再入、remote/CI待ちを混同せず、各実測値またはunknownを示せる。
- 初回起動以外の異常な遅延（レビュー待ち、検証再試行、source/lock待ち、繰返しGit、完成後の余計な再照会、計測自体の負荷など）を**ユーザーの指摘を待たずに**抽出できることを、少なくとも一つのreal Task traceと欠測例で示す。テストがPASSであることだけで性能分析をPASSにしない。
## Historical invariants

- 記録の意味を正確に保ち、通常Lifecycle、1 Task 1 Commit、source binding、親Codexの最終意味判断、Current schema、後方互換性禁止を維持する。

## Dependencies

none
