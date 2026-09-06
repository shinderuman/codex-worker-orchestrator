package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

type reviewBlobRound struct {
	Version      int                     `json:"version"`
	ReviewNumber int                     `json:"review_number"`
	Files        []taskdiff.FileIdentity `json:"files"`
}

type reviewedBoundaryStatus struct {
	path     string
	reviewed bool
	round    int
}

const (
	lastReviewBlobsFile  = "last-review-blobs.json"
	reviewedBlobsFile    = "reviewed-blobs.jsonl"
	reviewedBlobsVersion = 1
)

func (w *Workflow) reviewedBoundaryContext(repoRoot string, reviewNumber int) string {
	paths, available, err := taskdiff.ChangedPaths(repoRoot, w.state)
	if err != nil || !available || len(paths) == 0 {
		return ""
	}
	identities, err := taskdiff.FileIdentities(repoRoot, paths)
	if err != nil {
		return ""
	}
	if err := w.promoteLastReviewBlobs(reviewNumber); err != nil {
		return ""
	}
	reviewed, _ := w.loadReviewedBlobRounds()
	if err := w.writeLastReviewBlobs(identities, reviewNumber); err != nil {
		return ""
	}
	return renderReviewedBoundary(identities, reviewed)
}

func (w *Workflow) loadReviewedBlobRounds() ([]reviewBlobRound, error) {
	data, err := os.ReadFile(w.state.Path(reviewedBlobsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rounds []reviewBlobRound
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var round reviewBlobRound
		if err := json.Unmarshal([]byte(line), &round); err != nil {
			return nil, fmt.Errorf("reviewed blob ledgerを読めません: %w", err)
		}
		rounds = append(rounds, round)
	}
	return rounds, nil
}

func (w *Workflow) promoteLastReviewBlobs(nextReviewNumber int) error {
	raw, err := os.ReadFile(w.state.Path(lastReviewBlobsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var previous reviewBlobRound
	if err := json.Unmarshal(raw, &previous); err != nil {
		return fmt.Errorf("前review roundのblob記録を読めません: %w", err)
	}
	if len(previous.Files) == 0 || previous.ReviewNumber >= nextReviewNumber {
		return nil
	}
	encoded, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(w.state.Path(reviewedBlobsFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = file.Write(append(encoded, '\n'))
	return err
}

func (w *Workflow) writeLastReviewBlobs(identities []taskdiff.FileIdentity, reviewNumber int) error {
	round := reviewBlobRound{Version: reviewedBlobsVersion, ReviewNumber: reviewNumber, Files: identities}
	data, err := json.Marshal(round)
	if err != nil {
		return err
	}
	return os.WriteFile(w.state.Path(lastReviewBlobsFile), append(data, '\n'), 0o600)
}

func renderReviewedBoundary(current []taskdiff.FileIdentity, reviewed []reviewBlobRound) string {
	statuses := classifyReviewedBoundary(current, reviewed)
	var builder strings.Builder
	builder.WriteString("REVIEWED_BOUNDARY:\n")
	builder.WriteString("RULE: exact head/index/worktree identity match with a recorded review round is already reviewed; mismatched or unrecorded files are the review target (fail closed)\n")
	newCount := 0
	for _, entry := range statuses {
		if entry.reviewed {
			builder.WriteString(fmt.Sprintf("%s: reviewed(round %d)\n", entry.path, entry.round))
			continue
		}
		builder.WriteString(entry.path + ": new-boundary\n")
		newCount++
	}
	builder.WriteString(fmt.Sprintf("NEW_BOUNDARY_COUNT: %d\n", newCount))
	builder.WriteString("END_REVIEWED_BOUNDARY")
	return builder.String()
}

func classifyReviewedBoundary(current []taskdiff.FileIdentity, reviewed []reviewBlobRound) []reviewedBoundaryStatus {
	statuses := make([]reviewedBoundaryStatus, 0, len(current))
	for _, identity := range current {
		statuses = append(statuses, reviewedBoundaryStatus{
			path:     identity.Path,
			reviewed: identityReviewed(identity, reviewed),
			round:    identityReviewedRound(identity, reviewed),
		})
	}
	return statuses
}

func identityReviewed(identity taskdiff.FileIdentity, reviewed []reviewBlobRound) bool {
	return identityReviewedRound(identity, reviewed) >= 0
}

func identityReviewedRound(identity taskdiff.FileIdentity, reviewed []reviewBlobRound) int {
	for _, round := range reviewed {
		for _, previous := range round.Files {
			if previous.Path == identity.Path && taskdiff.SameFileIdentity(previous, identity) {
				return round.ReviewNumber
			}
		}
	}
	return -1
}

func reviewedBoundaryForState(st *state.StateStore) []reviewBlobRound {
	data, err := os.ReadFile(st.Path(reviewedBlobsFile))
	if err != nil {
		return nil
	}
	var rounds []reviewBlobRound
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var round reviewBlobRound
		if err := json.Unmarshal([]byte(line), &round); err == nil {
			rounds = append(rounds, round)
		}
	}
	return rounds
}
