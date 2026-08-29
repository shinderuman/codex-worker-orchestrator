package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type testImpactEventsScan struct {
	Status          string               `json:"status"`
	Dir             string               `json:"dir"`
	Files           int                  `json:"files"`
	SkippedLines    int                  `json:"skipped_lines,omitempty"`
	IgnoredFiles    []string             `json:"ignored_files,omitempty"`
	UnreadableTasks []telemetryTaskError `json:"unreadable_tasks,omitempty"`

	logs []state.TaskEvents
}

type testImpactOutput struct {
	Events    testImpactEventsScan   `json:"events"`
	Telemetry telemetryScan          `json:"telemetry"`
	Rounds    modelRoutingRoundsScan `json:"rounds"`
	RepoRoot  string                 `json:"repo_root"`
	Report    state.TestImpactReport `json:"report"`
}

func printTestImpact(st *state.StateStore, stdout io.Writer) error {
	events, err := scanTaskEventLogs(st)
	if err != nil {
		return err
	}
	scan, err := scanTelemetryTaskLogs(st)
	if err != nil {
		return err
	}
	rounds, tasks := attachModelRoutingConvergenceDeltas(st, scan.logs)
	return writeJSON(stdout, testImpactOutput{
		Events:    *events,
		Telemetry: *scan,
		Rounds:    rounds,
		RepoRoot:  st.ReadOr("repo-root", ""),
		Report:    state.BuildTestImpactReport(events.logs, testImpactReviews(tasks)),
	})
}

func scanTaskEventLogs(st *state.StateStore) (*testImpactEventsScan, error) {
	dir := st.Path("events")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("events dirを読めません: %w", err)
	}

	scan := &testImpactEventsScan{Status: "ok", Dir: dir}
	considered := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		considered++
		taskID := strings.TrimSuffix(name, ".jsonl")
		if !state.ValidGeneratedUUID(taskID) {
			scan.IgnoredFiles = append(scan.IgnoredFiles, name)
			continue
		}
		records, skipped, readErr := scanLogRecords(st.TaskEventLogPath(taskID), state.ParseTaskEventLine)
		if readErr == nil {
			scan.Files++
			scan.SkippedLines += skipped
			scan.logs = append(scan.logs, state.TaskEvents{TaskID: taskID, Records: records})
			continue
		}
		scan.Status = statusPartial
		scan.UnreadableTasks = append(scan.UnreadableTasks, telemetryTaskError{
			TaskID: taskID,
			Error:  readErr.Error(),
		})
	}
	if considered == 0 {
		scan.Status = statusNone
	}
	return scan, nil
}

func testImpactReviews(tasks []state.TaskCallLogs) map[string]state.TestImpactReviewSummary {
	reviews := make(map[string]state.TestImpactReviewSummary, len(tasks))
	for _, task := range tasks {
		reviews[task.TaskID] = state.TestImpactReviewFromCallOutcomes(task.QualityOutcomes)
	}
	return reviews
}
