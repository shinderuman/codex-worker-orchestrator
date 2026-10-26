# Task: 初回GLM起動トレースに基づく待機時間の根本修正と計測の整理

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

先行Taskで仕込んだログの実測値を用いて、`glm-parent-action start`から最初のGLMリクエストまでに生じる不要な待ち時間の主因を特定・修正する。局所的な関数の高速化ではなく正規Workflowの初回待機時間と全体信頼性を改善し、恒久的に不要な計測は削除する。

## External feasibility

status: not-applicable

## Contract

- `IMPLEMENTATION_TASKS/initial-glm-startup-stage-timing.md`で導入したphase timingのcurrent実装とsource-bound live traceを読み、startコマンド入口から最初のモデル呼出までのend-to-end遅延を分解する。実行前・Controller/lock/admission/materialize・Git snapshot・quality preflight・Task起動・context/search・runner/CLI・provider待ちを混同しない。
- 初回startの複数回と後続decision/fixを、可能な範囲で同一環境/同一source/同条件で比較する。初期cache、Docker/Go、ロック競合、toolchain、path/scanning、モデル5h制限等は証拠がある場合だけ原因に採用し、欠測をunknownのまま保持する。
- 遅延に寄与するcanonical ownerを修正し、不要な再走査・重複検証・同期待機・重い準備処理を根本から削減する。source/Task authority、quality、Review、resumption、安全境界を保持し、外側からのwrapperやfallbackで覆い隠さない。明確な原因が複数の独立責務に跨る場合は、実証できる同一責務の改善を完遂し、独立する内容を具体的なTaskへ分離する。
- 改善前後のstage別時間と、コマンド入口→初回モデル要求境界までのwall-clock/elapsedを比較する。GLMリクエスト実送信を観測できない場合は代理境界を明示する。モデル待ち・実行後の時間を初回pre-call短縮値に混ぜず、早い開始と全体所要時間/品質を両立させる。
- 計測ログが修正後も回帰検出や運用に必要か判断し、不要なら計測の実装・専用artifact/露出面をこのTask内で除去する。恒久的に必要な場合だけ既存観測ownerへ最小限残し、増えたログ/CPU/I/O/保守負荷の合理性を示す。修正前後の証拠は既存Bundle/Gitで残し、ログ消去によって検証根拠を失わせない。
- 先行ログの実データが取得できなければ、正規のstartで必要な追加観測を行う。確認不能なまま推測修正で完了とせず、制約を明示する。ユーザーの許可を要するlive呼び出しを無断で実行しない。

## Must not

- 特定stageの微小な改善を初回6分問題の解消と宣言しない。診断用ログの存在だけで完了にしない。
- Quality Gate、独立Review、Controller admission、Task/source binding、guard、検証範囲を緩和しない。単にsleep/timeout/skipを変更するだけで待機を隠さない。
- 独立TaskのSystem-One再設計や総合効率Checkpointを無制限に取り込まない。証拠なしに原因を断定しない。
- 観測が不要になっても既存の正式なモデル呼出/失敗evidenceを消去しない。旧ログ向け互換経路を残さない。

## Acceptance criteria

- 初回startの待ち時間をsource-boundなstage計測から説明し、原因を根拠付きで特定する。原因未特定なら完了扱いせず、観測上の制約を明記する。
- 実装後、同条件で初回startのpre-call待ち時間の短縮を数値で示し、後続decision/fix、Task品質、停止・再開・復旧の回帰がないことを確認する。比較不能なら改善達成と報告しない。
- 既存の正規Quality Gate、関連test、独立Reviewerと親Codexのsemantic評価を成立させる。不要になったログの削除または残す理由が差分・証拠から確認できる。

## Historical invariants

- 親Codexのsemantic authority、通常Lifecycle、1 Task 1 Commit、source/telemetryの意味と機密性、後方互換性禁止を維持する。

## Dependencies

- `IMPLEMENTATION_TASKS/initial-glm-startup-stage-timing.md`
