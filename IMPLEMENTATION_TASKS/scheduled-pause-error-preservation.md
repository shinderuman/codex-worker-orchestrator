# Task: scheduled pause と副次エラーの優先順位

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1564#discussion_r4534533519

** Major** · **Stability & Availability** · **Quick win**

**Preserve scheduled pauses before classifying structured output.**

If the process emits a retry-exhausted result before the pause stops it, `terminalRunError` replaces the pause with `StructuredOutputError`. Workflow then clears the resume checkpoint instead of saving the scheduled stop.

<details>
<summary>Suggested fix</summary>
<dl>
<dd>
<br>

```diff
 func terminalRunError(result RunResult, parsed claudeJSONResult, parseErr, runErr error) error {
+	var paused *ScheduledPauseError
+	if errors.As(runErr, &paused) {
+		return runErr
+	}
 	if parseErr == nil && parsed.Subtype == subtypeStructuredOutputRetryExhausted {
```

</dd>
</dl>
</details>

出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1564#pullrequestreview-5854509791

**Preserve the scheduled pause when output writing fails.**

If the temporary filesystem fills during a call, the output write can replace `ScheduledPauseError`. The workflow then misses the scheduled stop and can clear the resume checkpoint.

<details>
<summary>Suggested fix</summary>
<dl>
<dd>
<br>

```diff
 	if err := writeResultOutput(outputPath, result.Response, streamResultSummary(parsed, parseErr), stderrPath); err != nil {
+		var scheduled *ScheduledPauseError
+		if errors.As(runErr, &scheduled) {
+			return result, errors.Join(runErr, err)
+		}
 		return result, err
 	}
```

</dd>
</dl>
</details>

</dd>
</dl>
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1564#discussion_r4534533519) — `glm-worker/internal/runner/peak_pause.go`
- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1564#pullrequestreview-5854509791) — `glm-worker/internal/runner/runner.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

retry-exhausted構造化出力と結果file書込み失敗がScheduledPauseErrorを隠す。

## External feasibility

status: not-applicable

## Contract

- scheduled pauseのtyped identityをterminal分類とwrite failureの双方で保存する。副次エラーも診断可能にし、既存非pauseの失敗を変更しない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- pause+retry-exhausted、pause+write failureでerrors.Asが成立し、workflowがcheckpointを保存してscheduled stopを保持する。非pause失敗の既存分類、source/task binding、取消と実process回収を維持する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
