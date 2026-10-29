# Task: 親Codex専用Taskの正規コミット・完了経路

## Original instruction

2026-10-29 ユーザー指示（原文）:

```text
Codexのみで作業した場合にコミットとタスク完了ができねえって言ってるのか？
だったらそれを起票するのがいまのタスクの役割だろうがクソが
```

## Amendments

none

## Resolved references

- 中間評価の成果物: `review-evidence/codex-efficiency-checkpoint-2026-10-29.md`。
- 現物: `glm-worker/internal/controller/publication_parent_preparation.go:parentCompletionReview`、`independentCompletionReviewer`、`AcceptParentCompletion`。
- 2026-10-29の当該checkpointは親専用であり、GLMモデル呼出なし。handoffはconsistent:trueだがcompletion_admitted:false。通常Runtime向け完了はaccepted Runtimeと独立Reviewerを必須とする。この観測を全Codex単独作業への一般的な不可能判定に広げない。
- Git hook環境の具体的なコミット拒否は`git-hook-worktree-environment-isolation.md`が所有する。

## Purpose

親専用の評価・計画Taskを、実際の要求・成果・検証・親採否に基づいて正規に完了させる。

## External feasibility

status: not-applicable

## Contract

- 通常GLM Taskと明示された親専用Taskの実行・採否・必要証拠を区別する。適用条件と正規authorityは実装前に親Codexが確定する。
- GLMを呼ばずに、source-bound成果、実際の検証、親の採否を使ってcommit、終了metadata、publication、terminal handoffまで到達できる既存ownerへの最小接続を実装する。
- 実行途中の通常Taskが親専用を名乗ってReview/Gateを省略できないようにする。未完了・失敗・source変更・不正なmode指定は拒否する。
- 完了Task削除、Plan同期、要求のGit履歴保持、NEXTを開始しない停止境界を維持する。
- 今回のcheckpointに対しては既存評価を再実施せず、保存された成果とcurrent sourceを確認して正規終了へ接続する。

## Must not

- 架空のWorker/Reviewer成功、偽のruntime/validation、手動state補正、guard無効化で完了させない。
- 親専用Taskを完了するためだけにGLM・追加モデルを呼ばない。通常Taskの品質要件を緩和しない。
- checkpointの評価作業へ本修正を混ぜない。

## Acceptance criteria

- 親専用metadata/evaluation Taskが正規CLIから1 Task 1 Commitの終了・必要Publicationへ進み、停止指定時にNEXTを開始しない。
- 通常GLM Taskへの偽装、必要検証欠落、古いsourceによる完了を拒否する。
- 各失敗段階から同じidentityで回復し、成功済みremote操作を反復しない。
- GLM未実行と実際の親検証・採否をmachine evidenceが区別して保持する。

## Historical invariants

- 親Codexのsemantic authority、source binding、品質gate、権限分離を維持する。

## Dependencies

none
