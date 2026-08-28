# Task: task事前分割とsemantic milestoneを運用評価

## Original instruction

````text
## Task 010: task事前分割 / semantic milestoneの運用評価

Task 009のデータを使い、

-巨大taskの事前分割
-意味milestone checkpoint
- resume boundary

が品質を落とさずcall長大化を抑えるか評価する。

このTask management再設計自体の導入後データも使う。

hard capは証拠なしに導入しない。
````

## Amendments

- 2026-08-23 telemetry baseline:

````text
codex-configの保存済みtelemetryではworkerがmodel時間の約81%を占め、task累積400 turn超のoutlierが複数存在する。一方、task難易度と分割有無の対応は現在のtelemetryだけでは確定できない。

Task 009でoutlierを再現可能に特定した後、該当taskのtask責務・milestone・resume境界と照合すること。turn数だけを原因として分割せず、分割による追加review callと品質結果を併記すること。

分割によってGLM消費が減ってもCodex側touchpoint・review量が増える可能性がある。採否はCodex ReductionとQuality Deltaを上位に置き、GLM消費削減だけで採用しないこと。
````

## Purpose

worker call長大化を機械的切断ではなく責務境界で抑える。

## Contract

- task file粒度とobserved call dataを対応付ける
- quality/call cost/追加call数を併記する

## Must not

- hard cap、無条件rotation、品質証拠なしの分割強制を行わない

## Acceptance criteria

- 分割/milestone/resumeの比較と採否条件
- session rotationとは別論点で結論
- 独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- Task 009完了済みのworker outlier report、session aging観測

## Dependencies

none

## External feasibility

status: not-applicable

## Review findings

- 2026-08-28 execution recovery: 評価artifactの主要集計とoutlier対応は再現済みだが、Task 007後追いcommit `705656a` / `168d3d7`の責務説明を一括表記した軽微な誤帰属を訂正すること。production/source diffはなく、GLM環境でfull testを再実行するとtest fixture内の`git init` / `git config` / `git remote`をauthority guardが検出するため、回復callは既存証拠の静的検証とartifact訂正に限定し、同commandを再実行しないこと。
- 2026-08-28 runtime blocker: 誤帰属は訂正済みartifactで解消し、主要集計も静的再計算で一致した。ただしno-testの静的回復callでもGit authority guardがmodel実行中の許可外subcommandを検出してworker terminal resultを拒否し、独立reviewerへ到達できない状態が3回再現した。Task 010の開始前境界に従い、current mainの実運用成立性懸念として修正せず停止し、修正担当・対応方針をユーザー判断へ戻す。

## Current boundary

ACTIVE。Task 009の再現可能outlier reportは利用可能。評価artifactではworker時間比率79.2%、task-level outlier 3件、task-management再設計前後のexplicit-fix turns中央値59→0を再現し、既存責務粒度基準と現行resume境界を維持、hard cap・強制分割・強制milestoneは不採用という結論候補まで作成済み。production/source変更はない。`glm-worker --help`誤起動はPR #26由来の専用entrypointで解消したが、Git authority guardがworker terminal resultを3回拒否し独立reviewerへ到達できないruntime blockerは残るため、修正担当・対応方針のユーザー判断待ち。Task 010は未完了のまま保持する。
