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
	workerExhaustiveSearchPhase    = "worker-exhaustive-search"
	reviewerExhaustiveSearchPhase  = "reviewer-exhaustive-search"
	exhaustiveSearchComplete       = "full-corpus-proof"
	exhaustiveSearchRequiredMarker = "EXHAUSTIVE_SEARCH_REQUIRED: true"
	exhaustiveSearchManifestDir    = "exhaustive-search"
)

func (w *Workflow) exhaustiveSearchContext(request, activeTaskPath string, role state.SessionRole, seq int) (string, error) {
	required, query, err := exhaustiveSearchContract(w.config.RepoRoot, request, activeTaskPath)
	if err != nil {
		return "", err
	}
	if !required {
		return "", nil
	}
	if strings.TrimSpace(query) == "" {
		return "", &WorkerError{Message: fmt.Sprintf("exhaustive search proof requires a non-marker query before %s dispatch", role)}
	}
	report, err := reposearch.ExhaustiveSearch(context.Background(), w.config.RepoRoot, query, reposearch.ExhaustiveOptions{})
	if err != nil {
		return "", &WorkerError{Message: fmt.Sprintf("exhaustive search proof failed before %s dispatch: %v", role, err)}
	}
	manifestPath, err := w.writeExhaustiveSearchManifest(role, seq, report)
	if err != nil {
		return "", &WorkerError{Message: fmt.Sprintf("exhaustive search proof manifest failed before %s dispatch: %v", role, err)}
	}
	phase := workerExhaustiveSearchPhase
	if role == state.ReviewerRole {
		phase = reviewerExhaustiveSearchPhase
	}
	w.recordExhaustiveSearchOutcome(phase, role, seq, report)
	return renderExhaustiveSearchProof(role, report, manifestPath), nil
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

func exhaustiveSearchContract(repoRoot, request, activeTaskPath string) (bool, string, error) {
	query := stripExhaustiveRequirementMarker(request)
	if activeTaskPath == "" {
		return hasExhaustiveRequirement(request), query, nil
	}
	path := filepath.Join(repoRoot, filepath.FromSlash(activeTaskPath))
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, query, nil
	}
	if err != nil {
		return false, query, fmt.Errorf("read exhaustive requirement source %s: %w", activeTaskPath, err)
	}
	return hasExhaustiveRequirement(taskExhaustiveRequirementText(string(content))), query, nil
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
	for _, line := range strings.Split(text, "\n") {
		if exhaustiveRequirementLine(line) {
			return true
		}
	}
	return false
}

func stripExhaustiveRequirementMarker(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !exhaustiveRequirementLine(line) {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func exhaustiveRequirementLine(line string) bool {
	line = strings.TrimSpace(line)
	line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
	return line == exhaustiveSearchRequiredMarker
}

func (w *Workflow) writeExhaustiveSearchManifest(role state.SessionRole, seq int, report reposearch.ExhaustiveReport) (string, error) {
	taskID, err := w.state.TaskID()
	if err != nil {
		return "", err
	}
	relative := filepath.Join("artifacts", taskID, exhaustiveSearchManifestDir, fmt.Sprintf("%s-%d.txt", role, seq))
	var manifest strings.Builder
	manifest.WriteString("MODE: full-corpus-deterministic\n")
	manifest.WriteString("ROLE: ")
	manifest.WriteString(string(role))
	manifest.WriteByte('\n')
	manifest.WriteString("PREDICATE: ")
	manifest.WriteString(report.Predicate)
	manifest.WriteByte('\n')
	manifest.WriteString(fmt.Sprintf("MATCH_COUNT: %d\n", len(report.Matches)))
	for _, match := range report.Matches {
		manifest.WriteString("MATCH: ")
		manifest.WriteString(match.Path)
		manifest.WriteByte('\n')
	}
	if err := w.state.Write(relative, strings.TrimSuffix(manifest.String(), "\n")); err != nil {
		return "", err
	}
	return w.state.Path(relative), nil
}

func renderExhaustiveSearchProof(role state.SessionRole, report reposearch.ExhaustiveReport, manifestPath string) string {
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
	block.WriteString("MATCH_MANIFEST: ")
	block.WriteString(manifestPath)
	block.WriteByte('\n')
	block.WriteString("MATCH_LIST_INLINE: none\n")
	block.WriteString("BM25_TOP_N_AUTHORITY: none\n")
	if role == state.ReviewerRole {
		block.WriteString("WORKER_EXHAUSTIVE_PROOF_AUTHORITY: none\n")
	}
	block.WriteString("COMPLETENESS_SCOPE: predicate-and-searchable-corpus-only\n")
	block.WriteString("REQUIREMENT: read MATCH_MANIFEST and inspect every MATCH; if this predicate is insufficient for the requested semantics, use another deterministic full-corpus mechanism before claiming exhaustive completion. BM25 top-N alone is never exhaustive evidence.\n")
	block.WriteString("END_EXHAUSTIVE_SEARCH_PROOF")
	return block.String()
}

func (w *Workflow) recordExhaustiveSearchOutcome(phase string, role state.SessionRole, seq int, report reposearch.ExhaustiveReport) {
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
		SearchPaths: paths,
	}); err != nil {
		state.WarnTaskEventSkip("exhaustive search proof追記", err)
	}
}
