package workflow

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const artifactPromptMarker = "REPORT_ARTIFACT_DIR:"

func withArtifactContext(prompt string, artifactDir string) string {
	if strings.Contains(prompt, artifactPromptMarker) {
		return prompt
	}
	return fmt.Sprintf(`%s

REPORT_ARTIFACT_DIR: %s
結果へ収まらない正確な一覧・レポート・生成物だけをこのディレクトリへ保存してください。リポジトリへ追加しないでください。ARTIFACTSに記載できるのはこのREPORT_ARTIFACT_DIR配下の実在する通常ファイルだけです。WORKER_REPORT・過去artifact・入力に含まれる他taskのartifact pathは参照証拠であり、ARTIFACTSへコピーしないでください。大容量成果物が不要ならARTIFACTSは空にしてください。
`, strings.TrimRight(prompt, "\n"), artifactDir)
}

func activeTaskPromptBlock(activeTaskPath string) string {
	return renderActiveTaskPromptContract(newActiveTaskPromptContract(activeTaskPath, activeTaskAudienceWorker))
}

func reviewerActiveTaskBlock(activeTaskPath string) string {
	return renderActiveTaskPromptContract(newActiveTaskPromptContract(activeTaskPath, activeTaskAudienceReviewer))
}

func newTaskPrompt(request string, activeTaskPath string) string {
	return fmt.Sprintf(`MODE: NEW_TASK

USER_REQUEST:
%s

%s`, request, activeTaskPromptBlock(activeTaskPath))
}

func decisionPrompt(request string, decision string, activeTaskPath string) string {
	return fmt.Sprintf(`MODE: CONTINUE_WITH_SOL_DECISION

ORIGINAL_USER_REQUEST:
%s

SOL_DECISION:
%s

%s直前の同一タスクの調査文脈を利用し、この判断に従って作業を継続してください。
`, request, decision, activeTaskPromptBlock(activeTaskPath))
}

func explicitFixPrompt(request string, decision string, previousReview string, instruction string, activeTaskPath string) string {
	return fmt.Sprintf(`MODE: APPLY_REVIEW_FIX

ORIGINAL_USER_REQUEST:
%s

PREVIOUS_SOL_DECISION:
%s

PREVIOUS_REVIEW:
%s

REVIEW_FEEDBACK:
%s

%s同一タスクの既存文脈を利用し、指摘範囲を修正してください。
`, request, decision, previousReview, instruction, activeTaskPromptBlock(activeTaskPath))
}

func reviewerPrompt(request string, decision string, workerReport string, reviewNumber int, baseline string, reviewNavigation string, activeTaskPath string) string {
	return fmt.Sprintf(`REVIEW_MODE: INDEPENDENT_REVIEW

USER_REQUEST:
%s

SOL_DECISION:
%s

WORKER_REPORT:
%s

REVIEW_NUMBER: %d

PRE_TASK_BASELINE:
%s

%s

%s現在のworking treeを実際に独立確認して判定してください。
REVIEW_DIFF_FIRST_NAVIGATIONのCHANGED_PATHを起点にactual git diffを最初に確認し、diffから影響範囲を拡張してください。
INDEPENDENT_SEARCHがperformedの場合も候補はnavigation-onlyであり、worker search結果やworker reportをauthorityとして採用せず、現在のコードで独立検証してください。
過去sessionの記憶より現在のコードを優先してください。
PRE_TASK_BASELINEのファイルはworker開始前の状態です。既存未コミット変更と今回変更を区別する必要がある場合に参照してください。
`, request, decision, workerReport, reviewNumber, baseline, reviewNavigation, reviewerActiveTaskBlock(activeTaskPath))
}

func automaticFixPrompt(request string, decision string, reviewReport string, activeTaskPath string) string {
	return fmt.Sprintf(`MODE: APPLY_REVIEW_FIX

ORIGINAL_USER_REQUEST:
%s

PREVIOUS_SOL_DECISION:
%s

INDEPENDENT_REVIEW:
%s

%s独立reviewerの指摘を修正してください。
新しい要求を追加せず、元要求・既存Sol判断・レビュー指摘の範囲だけを変更してください。
修正後に必要なテスト・lint・build・自己レビューまで行ってください。
`, request, decision, reviewReport, activeTaskPromptBlock(activeTaskPath))
}

func reportOnlyFixPrompt(request string, decision string, reviewReport string, activeTaskPath string) string {
	return fmt.Sprintf(`MODE: APPLY_REVIEW_FIX

ORIGINAL_USER_REQUEST:
%s

PREVIOUS_SOL_DECISION:
%s

INDEPENDENT_REVIEW:
%s

%s独立reviewerはコードとdiffを正しいと確認し、報告へ圧縮された意味情報だけを不足と指摘しています。
実装・working tree変更・追加調査・test/lint/build/self-reviewをやり直さず、現在の作業結果とdiffに基づいて報告だけを再出力してください。
`, request, decision, reviewReport, activeTaskPromptBlock(activeTaskPath))
}

func resultCorrectionPrompt(reason string) string {
	return fmt.Sprintf(`直前の作業結果の内容は有効ですが、結果の意味検証に不合格でした。
作業・調査・テストをやり直さず、違反を修正した同じ内容の結果を再出力してください。
各fieldのvalueは空にできず、改行を含められません。複数事項は同じvalue内でセミコロン区切りにしてください。
結果全体は6 KiB・1 field 1536 bytes以内です。STATUSに応じた必須fieldを省略しないでください。
大容量成果物の内容は再掲せず、ARTIFACTSには既に保存済みの絶対パスだけを指定してください。

違反内容:
%s
`, reason)
}

func resumePrompt(checkpoint state.ResumeCheckpoint) string {
	originalPrompt := checkpoint.OriginalPrompt
	if originalPrompt == "" {
		originalPrompt = checkpoint.Prompt
	}

	reason := "Z.ai GLM Coding Planの5時間利用上限"
	reasonCode := "plan-limit"
	if checkpoint.ProviderUnavailable {
		reason = "一時的なprovider障害"
		reasonCode = "provider-unavailable"
	}

	return fmt.Sprintf(`RESUME_REASON: %s
前回のClaude Code呼び出しは%sで中断しました。

同じタスク・同じsessionの中断箇所から作業を再開してください。
現在のworking treeには前回の途中変更が残っている可能性があります。破棄せず、session文脈と照合して続行してください。
最初から調査・実装をやり直さず、未完了部分だけを進めて所定のPACKETまで完了してください。

前回の指示:
%s
`, reasonCode, reason, originalPrompt)
}

func riskFloorReemitPrompt() string {
	return `直前の独立reviewはHIGH RISK最終確認が必要な経路です。reviewerがPASSを返しましたが、wrapper risk floorがこれを却下しました。
reviewerの自然言語判断だけではこの経路のriskを降格できません。
実装・調査・テストをやり直さず、直前のreview結果の内容を保ったまま、結果だけを再出力してください。
許容されるSTATUSは NEEDS_SOL_REVIEW (RISK: HIGH) だけです。PASS・FIX_REQUIRED・その他は許可されません。
TARGETSにはnoneを指定できません。Solが読むべき最小対象をfile:symbol/行範囲で指定してください。
NEEDS_SOL_REVIEWの必須field(SUMMARY, REQUIREMENT_COVERAGE, INVARIANTS, TEST_EVIDENCE, ISSUES, RESIDUAL_RISK, TARGETS, ARTIFACTS, SOL_QUESTION)を省略しないでください。
`
}
