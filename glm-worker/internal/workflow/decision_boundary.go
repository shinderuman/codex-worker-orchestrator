package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type semanticDecisionAxis string

const (
	decisionAxisResponsibility      semanticDecisionAxis = "responsibility"
	decisionAxisDependencyDirection semanticDecisionAxis = "dependency-direction"
	decisionAxisPublicSurface       semanticDecisionAxis = "public-surface"
	decisionAxisCompatibility       semanticDecisionAxis = "compatibility"
	decisionAxisValidationError     semanticDecisionAxis = "validation-error-semantics"

	solDecisionAuthorityHeading = "Sol decision authority"
	solDecisionBoundaryMarker   = "SOL_DECISION_BOUNDARY:"
)

var semanticDecisionAxisOrder = []semanticDecisionAxis{
	decisionAxisResponsibility,
	decisionAxisDependencyDirection,
	decisionAxisPublicSurface,
	decisionAxisCompatibility,
	decisionAxisValidationError,
}

var semanticDecisionAxes = map[string]semanticDecisionAxis{
	string(decisionAxisResponsibility):      decisionAxisResponsibility,
	string(decisionAxisDependencyDirection): decisionAxisDependencyDirection,
	string(decisionAxisPublicSurface):       decisionAxisPublicSurface,
	string(decisionAxisCompatibility):       decisionAxisCompatibility,
	string(decisionAxisValidationError):     decisionAxisValidationError,
}

type semanticDecisionAuthority struct {
	fixed map[semanticDecisionAxis]string
}

func parseSemanticDecisionAuthority(content string) (semanticDecisionAuthority, error) {
	authority := semanticDecisionAuthority{fixed: make(map[semanticDecisionAxis]string)}
	inSection := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if inSection {
				break
			}
			inSection = heading == solDecisionAuthorityHeading
			continue
		}
		if !inSection {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			return semanticDecisionAuthority{}, fmt.Errorf("%s sectionの行 %qは`- axis: value`形式である必要があります", solDecisionAuthorityHeading, trimmed)
		}
		entry := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		name, value, ok := strings.Cut(entry, ":")
		if !ok {
			return semanticDecisionAuthority{}, fmt.Errorf("%s sectionの行 %qにaxis value区切りがありません", solDecisionAuthorityHeading, trimmed)
		}
		axis, ok := semanticDecisionAxes[strings.TrimSpace(name)]
		if !ok {
			return semanticDecisionAuthority{}, fmt.Errorf("%s sectionにunknown axis %qがあります", solDecisionAuthorityHeading, strings.TrimSpace(name))
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return semanticDecisionAuthority{}, fmt.Errorf("%s sectionの%s axis valueが空です", solDecisionAuthorityHeading, axis)
		}
		if _, exists := authority.fixed[axis]; exists {
			return semanticDecisionAuthority{}, fmt.Errorf("%s sectionの%s axisが重複しています", solDecisionAuthorityHeading, axis)
		}
		authority.fixed[axis] = value
	}
	return authority, nil
}

func loadSemanticDecisionAuthority(repoRoot, activeTaskPath string) (semanticDecisionAuthority, error) {
	if activeTaskPath == "" {
		return semanticDecisionAuthority{fixed: make(map[semanticDecisionAxis]string)}, nil
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(activeTaskPath)))
	if err != nil {
		return semanticDecisionAuthority{}, fmt.Errorf("read Sol decision authority from %s: %w", activeTaskPath, err)
	}
	return parseSemanticDecisionAuthority(string(data))
}

func (a semanticDecisionAuthority) unresolved() []semanticDecisionAxis {
	result := make([]semanticDecisionAxis, 0, len(semanticDecisionAxisOrder))
	for _, axis := range semanticDecisionAxisOrder {
		if _, ok := a.fixed[axis]; !ok {
			result = append(result, axis)
		}
	}
	return result
}

func decisionBoundaryContextBlock(activeTaskPath string, authority semanticDecisionAuthority) string {
	if activeTaskPath == "" {
		return ""
	}
	var block strings.Builder
	block.WriteString("\n\n")
	block.WriteString(solDecisionBoundaryMarker)
	block.WriteString("\nAUTHORITY_SOURCE: ")
	block.WriteString(activeTaskPath)
	block.WriteString(" / ## ")
	block.WriteString(solDecisionAuthorityHeading)
	block.WriteString("\nFIXED_AXES:\n")
	if len(authority.fixed) == 0 {
		block.WriteString("- none\n")
	} else {
		for _, axis := range semanticDecisionAxisOrder {
			value, ok := authority.fixed[axis]
			if !ok {
				continue
			}
			block.WriteString("- ")
			block.WriteString(string(axis))
			block.WriteString(": ")
			block.WriteString(value)
			block.WriteString("\n")
		}
	}
	unresolved := authority.unresolved()
	block.WriteString("UNRESOLVED_AXES: ")
	if len(unresolved) == 0 {
		block.WriteString("none")
	} else {
		names := make([]string, 0, len(unresolved))
		for _, axis := range unresolved {
			names = append(names, string(axis))
		}
		block.WriteString(strings.Join(names, ","))
	}
	block.WriteString("\nAUTHORITY_RULES:\n")
	block.WriteString("- requested outcome、ACTIVE状態、`互換性を狭めない強化`、`明白な仕様準拠`だけではUNRESOLVED axisを確定済みにしない。\n")
	block.WriteString("- 実装にUNRESOLVED axisの意味選択が必要なら、その意味変更を編集する前にNEEDS_SOL_DECISIONで停止する。\n")
	block.WriteString("- type/package/interface追加は、それ自体では意味責務新設とは扱わない。既存またはFIXED responsibility内の明白な実装詳細は自律実装できる。\n")
	block.WriteString("- validation/error behaviorの追加・拒否条件強化・error意味変更はvalidation-error-semanticsがFIXEDでない限り自律強化しない。\n")
	return block.String()
}

func applyDecisionBoundaryContext(checkpoint state.ResumeCheckpoint, block string) state.ResumeCheckpoint {
	if block == "" || checkpoint.DecisionBoundaryApplied {
		return checkpoint
	}
	checkpoint.Prompt = strings.TrimRight(checkpoint.Prompt, "\n") + block
	if checkpoint.OriginalPrompt != "" {
		checkpoint.OriginalPrompt = strings.TrimRight(checkpoint.OriginalPrompt, "\n") + block
	}
	checkpoint.DecisionBoundaryApplied = true
	return checkpoint
}

func (w *Workflow) activateDecisionBoundaryContext(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, error) {
	if checkpoint.Role != state.WorkerRole || checkpoint.ReportOnly || checkpoint.DecisionBoundaryApplied {
		return checkpoint, nil
	}
	activeTaskPath := w.readActiveTaskState()
	if activeTaskPath == "" || !activeTaskFileExists(w.config.RepoRoot, activeTaskPath) {
		return checkpoint, nil
	}
	authority, err := loadSemanticDecisionAuthority(w.config.RepoRoot, activeTaskPath)
	if err != nil {
		return checkpoint, err
	}
	return applyDecisionBoundaryContext(checkpoint, decisionBoundaryContextBlock(activeTaskPath, authority)), nil
}
