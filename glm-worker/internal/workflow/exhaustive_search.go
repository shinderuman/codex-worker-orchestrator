package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	workerExhaustiveSearchPhase   = "worker-exhaustive-search"
	reviewerExhaustiveSearchPhase = "reviewer-exhaustive-search"
	exhaustiveSearchComplete      = "full-corpus-proof"
)

func (w *Workflow) exhaustiveSearchContext(request, activeTaskPath string, role state.SessionRole, seq int) (string, error) {
	required, err := exhaustiveSearchRequired(w.config.RepoRoot, request, activeTaskPath)
	if err != nil {
		return "", err
	}
	if !required {
		return "", nil
	}
	report, err := reposearch.ExhaustiveSearch(context.Background(), w.config.RepoRoot, request, reposearch.ExhaustiveOptions{})
	if err != nil {
		return "", &WorkerError{Message: fmt.Sprintf("exhaustive search proof failed before %s dispatch: %v", role, err)}
	}
	phase := workerExhaustiveSearchPhase
	if role == state.ReviewerRole {
		phase = reviewerExhaustiveSearchPhase
	}
	w.recordExhaustiveSearchOutcome(phase, role, seq, request, report)
	return renderExhaustiveSearchProof(role, report), nil
}

func (w *Workflow) executeWorkerCheckpointWithExhaustiveContext(request, activeTaskPath string, checkpoint state.ResumeCheckpoint, pocStage bool) error {
	contextBlock, err := w.exhaustiveSearchContext(request, activeTaskPath, state.WorkerRole, 1)
	if err != nil {
		return err
	}
	checkpoint.Prompt += contextBlock
	checkpoint.OriginalPrompt = checkpoint.Prompt
	return w.executeWorkerCheckpoint(request, checkpoint, pocStage)
}

func exhaustiveSearchRequired(repoRoot, request, activeTaskPath string) (bool, error) {
	if hasExhaustiveRequirement(request) {
		return true, nil
	}
	if activeTaskPath == "" {
		return false, nil
	}
	path := filepath.Join(repoRoot, filepath.FromSlash(activeTaskPath))
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read exhaustive requirement source %s: %w", activeTaskPath, err)
	}
	return hasExhaustiveRequirement(taskExhaustiveRequirementText(string(content))), nil
}

func taskExhaustiveRequirementText(content string) string {
	allowed := map[string]bool{
		"Original instruction": true,
		"Amendments":           true,
		"Contract":             true,
		"Acceptance criteria":  true,
	}
	var result strings.Builder
	include := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## ") {
			include = allowed[strings.TrimSpace(strings.TrimPrefix(line, "## "))]
			continue
		}
		if include {
			result.WriteString(line)
			result.WriteByte('\n')
		}
	}
	return result.String()
}

func hasExhaustiveRequirement(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"exhaustive", "full corpus", "full-corpus", "網羅", "全候補", "漏れなく"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func renderExhaustiveSearchProof(role state.SessionRole, report reposearch.ExhaustiveReport) string {
	var block strings.Builder
	block.WriteString("\n\nEXHAUSTIVE_SEARCH_PROOF:\n")
	block.WriteString("MODE: full-corpus-deterministic\n")
	block.WriteString("ROLE: ")
	block.WriteString(string(role))
	block.WriteByte('\n')
	block.WriteString("CORPUS_SCOPE: repo-search-searchable-corpus\n")
	block.WriteString("PREDICATE: ")
	block.WriteString(report.Predicate)
	block.WriteByte('\n')
	block.WriteString(fmt.Sprintf("ENUMERATED_FILES: %d\n", report.EnumeratedFiles))
	block.WriteString(fmt.Sprintf("SCANNED_FILES: %d\n", report.ScannedFiles))
	block.WriteString(fmt.Sprintf("SKIPPED_FILES: %d\n", report.SkippedFiles))
	block.WriteString(fmt.Sprintf("MATCH_COUNT: %d\n", len(report.Matches)))
	for _, match := range report.Matches {
		block.WriteString("MATCH: ")
		block.WriteString(match.Path)
		block.WriteByte('\n')
	}
	block.WriteString("BM25_TOP_N_AUTHORITY: none\n")
	if role == state.ReviewerRole {
		block.WriteString("WORKER_EXHAUSTIVE_PROOF_AUTHORITY: none\n")
	}
	block.WriteString("COMPLETENESS_SCOPE: predicate-and-searchable-corpus-only\n")
	block.WriteString("REQUIREMENT: inspect every MATCH; if this predicate is insufficient for the requested semantics, use another deterministic full-corpus mechanism before claiming exhaustive completion. BM25 top-N alone is never exhaustive evidence.\n")
	block.WriteString("END_EXHAUSTIVE_SEARCH_PROOF")
	return block.String()
}

func (w *Workflow) recordExhaustiveSearchOutcome(phase string, role state.SessionRole, seq int, query string, report reposearch.ExhaustiveReport) {
	taskID, err := w.state.TaskID()
	if err != nil {
		return
	}
	paths := make([]string, 0, len(report.Matches))
	for _, match := range report.Matches {
		paths = append(paths, match.Path)
	}
	if err := w.state.AppendTaskEvent(state.TaskEventRecord{
		TaskID:      taskID,
		Role:        string(role),
		Phase:       phase,
		Seq:         seq,
		Timestamp:   w.now().UTC(),
		Kind:        "exhaustive-search",
		Subtype:     exhaustiveSearchComplete,
		SearchQuery: query,
		SearchPaths: paths,
	}); err != nil {
		state.WarnTaskEventSkip("exhaustive search proof追記", err)
	}
}
