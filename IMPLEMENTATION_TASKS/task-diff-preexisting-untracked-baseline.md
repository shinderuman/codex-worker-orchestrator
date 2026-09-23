# Task: 開始前untracked fileのtask差分保持

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
baselineは既存untracked fileのpathだけを保存し、それらをtask diffから一律除外する。開始後の内容変更・削除がChangedPathsとreviewer patchに出ないため、正規baselineと差分を保持する。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F16。実装ownerは `glm-worker/internal/state/baseline.go の CaptureGitBaseline、taskdiff/capture.go の Capture / ChangedPaths / taskCreatedPaths、workflow/reviewer_diff_evidence.go`。
- 再現資料: `review-evidence/taskdiff_audit_test.go.txt`。対象test: `TestAuditPreexistingUntrackedChangeIsInTaskDiff`.

## Purpose

reviewerへ渡すtask差分の欠落を防ぎ、品質を保ったまま必要範囲へ読取を絞る。

## External feasibility

status: not-applicable

## Contract

- 開始時のuntracked fileの必要な内容・種別・modeをtaskにbindし、変更・削除を正規baselineとの比較で検出する。保存形式と範囲・機密data保護は親が確定する。
- 開始前から無変更のfileはtask差分へ混入させず、tracked dirty baselineと新規fileの既存責務を維持する。
- Capture・ChangedPaths・reviewer evidenceのcoverageを整合させ、情報不足をavailableな空diffへ縮退させない。

## Must not

- 古いbaselineから存在しない内容を推測復元するmigrationを入れない。
- repository全体の無制限backupや別state DBを追加しない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- preexisting.goの変更・削除が正確なchanged pathsとreviewer patchに含まれる。
- 無変更untrackedを除外し、binary・symlink・mode・task途中のtracking変更・安全なpath境界を対象別に確認する。
- baseline欠損・中断・上限超過の扱いを明示し、不完全な取得を変更なしと報告しない。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
