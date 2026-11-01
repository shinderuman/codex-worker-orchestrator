# Task: 未終了attemptを跨ぐstage timeline gapの分類修正

## Original instruction

````text
classifyStageTimelineGapがExitedAt zero（interrupt等の未終了attempt）でendedBeforeGapを常時成立させる問題を修正する。
````

## Amendments

none

## Resolved references

- CR-1427-1 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1427#discussion_r4409201422 · `glm-worker/internal/report/stage_timeline.go:498-517`付近
  原文: "**Do not treat open attempts as ended before the gap.** `correlateStageAttempts` can move a `\"started\"`-only record into `attributed`. This happens when the process was interrupted before `recordExit` ran and its stage logs carry the attempt ID. Such a record has a zero `ExitedAt`. For a zero `ExitedAt`, `!attempt.ExitedAt.After(start)` is always true, so `endedBeforeGap` becomes true for every gap. The in-attempt check `!end.After(attempt.ExitedAt)` is always false. As a result, a gap inside a still-running or interrupted attempt is reported as `parent-wait` whenever any later attempt exists. The interrupted path is the exact case this report diagnoses. Skip attempts with a zero `ExitedAt` in the \"ended before\" test. Alternatively, treat them as open until `generatedAt`."

## Purpose

interrupted/実行中attempt内のgapが誤ってparent-waitに分類されないようにする。

## External feasibility

status: not-applicable

## Contract

- zero `ExitedAt` attemptを`endedBeforeGap`判定から除外し、gap分類（in-attempt/unattributed/parent-wait）の意味を保つ。採用方式（除外 or generatedAtまでopen扱い）は報告readerの意図に合わせて決める。

## Must not

- timeline reportの出力schemaを変更しない。

## Acceptance criteria

- zero ExitedAt attempt＋後続attempt存在時のgap分類の正負test。

## Historical invariants

none

## Dependencies

none
