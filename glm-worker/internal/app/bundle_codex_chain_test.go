package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type chainSecondSpec struct {
	metaOffset       time.Duration
	cwd              string
	duplicateOfFirst bool
	continuedCounter bool
	malformed        bool
}

func TestParentUsageCanonicalTwoFileChainSplitsTaskWindow(t *testing.T) {
	for _, boundary := range []string{"fresh-first-anchor", "decreased-first-anchor"} {
		t.Run(boundary, func(t *testing.T) {
			task := newAnalysisTerminalTask(t)
			firstRel := analysisRolloutRel()
			secondRel := "sessions/2026/08/31/rollout-resumed-" + codexTestParentThreadID + ".jsonl"
			splitAt := task.start.Add(10 * time.Minute)
			firstLines := []string{
				chainTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500, 1500),
				analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
				parentUsageToolCallLine(t, task.start.Add(time.Minute), "chain-first-exec"),
				chainTokenCountLine(t, task.start.Add(5*time.Minute), 1500, 700, 300, 200, 2300, 800),
			}
			boundaryAnchor := chainTokenCountLine(t, splitAt.Add(time.Minute), 400, 200, 100, 50, 500, 500)
			if boundary == "decreased-first-anchor" {
				boundaryAnchor = parentUsageTokenCountLine(t, splitAt.Add(time.Minute), 400, 200, 100, 50, 500)
			}
			secondLines := []string{
				boundaryAnchor,
				parentUsageToolOutputLine(t, splitAt.Add(2*time.Minute), "chain-second-exec", "0123456789"),
				parentUsageCompactedLine(t, splitAt.Add(3*time.Minute)),
				chainTokenCountLine(t, splitAt.Add(10*time.Minute), 1200, 600, 260, 130, 1200, 700),
				analysisTurnLine(t, task.completeAt, codexRolloutTaskCompleteType, analysisOwningTurnID),
			}
			writeChainRollout(t, task.codexHome, firstRel, codexTestParentThreadID, task.start.Add(-3*time.Hour), "/repo", "Codex Desktop", firstLines)
			writeChainRollout(t, task.codexHome, secondRel, codexTestParentThreadID, splitAt, "/repo", "Codex Desktop", secondLines)

			report := runParentUsageReport(t, task.cfg)
			parent := report.ParentSession
			if parent.Status != codexStatusIncluded || parent.RolloutSource != firstRel || len(parent.RolloutChain) != 2 {
				t.Fatalf("parent session = %#v", parent)
			}
			if parent.RolloutChain[0].Source != firstRel || parent.RolloutChain[1].Source != secondRel {
				t.Fatalf("rollout chain order = %#v", parent.RolloutChain)
			}
			if parent.RolloutChain[0].FirstEventAt != task.start.Add(-3*time.Hour).Format(time.RFC3339Nano) ||
				parent.RolloutChain[1].LastEventAt != task.completeAt.Format(time.RFC3339Nano) {
				t.Fatalf("rollout chain bounds = %#v", parent.RolloutChain)
			}

			execution := report.Intervals.TaskExecution
			if execution.Status != analysisStatusAvailable {
				t.Fatalf("execution interval = %#v", execution)
			}
			tokens := execution.Tokens
			if tokens.Status != analysisStatusAvailable ||
				tokens.InputTokens != 1700 || tokens.CachedInputTokens != 800 ||
				tokens.OutputTokens != 320 || tokens.ReasoningTokens != 170 ||
				tokens.TotalTokens != 2000 {
				t.Fatalf("execution tokens = %#v", tokens)
			}
			if tokens.BaselineSource != parentUsageSourceLocator(firstRel, 2) ||
				tokens.EndSource != parentUsageSourceLocator(secondRel, 5) {
				t.Fatalf("execution token locators = %#v", tokens)
			}
			activity := execution.Activity
			if activity.Status != analysisStatusCounted ||
				activity.ModelTurns != 1 || activity.ToolCalls != 1 ||
				activity.ToolResults != 1 || activity.Compactions != 1 ||
				activity.ToolOutputBytes != 10 {
				t.Fatalf("execution activity = %#v", activity)
			}
			if activity.Source != firstRel+";"+secondRel {
				t.Fatalf("execution activity source = %q", activity.Source)
			}

			index := runAnalysisBundle(t, task.cfg, "")
			if index.ParentSession.Status != codexStatusIncluded ||
				len(index.ParentSession.RolloutSources) != 2 ||
				index.ParentSession.RolloutSources[0] != firstRel ||
				index.ParentSession.RolloutSources[1] != secondRel {
				t.Fatalf("index parent session = %#v", index.ParentSession)
			}
			if index.TokenDelta.Status != tokens.Status || index.TokenDelta.InputTokens != tokens.InputTokens ||
				index.TokenDelta.CachedInputTokens != tokens.CachedInputTokens {
				t.Fatalf("index token delta = %#v want parent usage %#v", index.TokenDelta, tokens)
			}
		})
	}
}

func TestParentUsageCanonicalThreeFileChainSumsEachSegment(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	firstRel := "sessions/2026/08/30/rollout-live-" + codexTestParentThreadID + ".jsonl"
	secondRel := "sessions/2026/08/31/rollout-second-" + codexTestParentThreadID + ".jsonl"
	thirdRel := "archived_sessions/rollout-third-" + codexTestParentThreadID + ".jsonl"
	firstLines := []string{
		chainTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500, 1500),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		chainTokenCountLine(t, task.start.Add(5*time.Minute), 1500, 700, 300, 200, 2300, 800),
	}
	secondLines := []string{
		chainTokenCountLine(t, task.start.Add(8*time.Minute), 700, 300, 100, 50, 700, 700),
		chainTokenCountLine(t, task.start.Add(12*time.Minute), 900, 400, 150, 70, 900, 200),
	}
	thirdLines := []string{
		chainTokenCountLine(t, task.start.Add(15*time.Minute), 300, 150, 80, 40, 300, 300),
		chainTokenCountLine(t, task.start.Add(20*time.Minute), 600, 300, 160, 80, 600, 300),
		analysisTurnLine(t, task.completeAt, codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
	writeChainRollout(t, task.codexHome, firstRel, codexTestParentThreadID, task.start.Add(-3*time.Hour), "/repo", "Codex Desktop", firstLines)
	writeChainRollout(t, task.codexHome, secondRel, codexTestParentThreadID, task.start.Add(7*time.Minute), "/repo", "Codex Desktop", secondLines)
	writeChainRollout(t, task.codexHome, thirdRel, codexTestParentThreadID, task.start.Add(14*time.Minute), "/repo", "Codex Desktop", thirdLines)

	report := runParentUsageReport(t, task.cfg)
	parent := report.ParentSession
	if parent.Status != codexStatusIncluded || len(parent.RolloutChain) != 3 ||
		parent.RolloutChain[0].Source != firstRel ||
		parent.RolloutChain[1].Source != secondRel ||
		parent.RolloutChain[2].Source != thirdRel {
		t.Fatalf("parent session = %#v", parent)
	}
	tokens := report.Intervals.TaskExecution.Tokens
	if tokens.Status != analysisStatusAvailable ||
		tokens.InputTokens != 2000 || tokens.TotalTokens != 2300 {
		t.Fatalf("execution tokens = %#v", tokens)
	}
	if tokens.BaselineSource != parentUsageSourceLocator(firstRel, 2) ||
		tokens.EndSource != parentUsageSourceLocator(thirdRel, 3) {
		t.Fatalf("execution token locators = %#v", tokens)
	}
}

func TestParentUsageRolloutChainFailuresStayAmbiguous(t *testing.T) {
	cases := []struct {
		name       string
		secondSpec chainSecondSpec
		reason     string
	}{
		{
			name:       "duplicate-content",
			secondSpec: chainSecondSpec{metaOffset: -3 * time.Hour, duplicateOfFirst: true},
			reason:     "rollout chain candidates duplicate identical content",
		},
		{
			name:       "overlapping-ranges",
			secondSpec: chainSecondSpec{metaOffset: -time.Hour},
			reason:     "rollout chain candidates have overlapping event ranges",
		},
		{
			name:       "identity-mismatch",
			secondSpec: chainSecondSpec{metaOffset: 2 * time.Hour, cwd: "/other"},
			reason:     "rollout chain candidates disagree on session identity (cwd)",
		},
		{
			name:       "counter-continues-across-boundary",
			secondSpec: chainSecondSpec{metaOffset: 2 * time.Hour, continuedCounter: true},
			reason:     "rollout chain boundary does not restart a self-contained token counter",
		},
		{
			name:       "unreadable-member",
			secondSpec: chainSecondSpec{metaOffset: 2 * time.Hour, malformed: true},
			reason:     "rollout chain candidate cannot be read",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := newAnalysisTerminalTask(t)
			firstRel := analysisRolloutRel()
			secondRel := "archived_sessions/rollout-second-" + codexTestParentThreadID + ".jsonl"
			firstLines := []string{
				chainTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500, 1500),
				analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
			}
			firstMeta := task.start.Add(-3 * time.Hour)
			writeChainRollout(t, task.codexHome, firstRel, codexTestParentThreadID, firstMeta, "/repo", "Codex Desktop", firstLines)
			if tc.secondSpec.duplicateOfFirst {
				writeChainRollout(t, task.codexHome, secondRel, codexTestParentThreadID, firstMeta, "/repo", "Codex Desktop", firstLines)
			} else {
				writeChainSecondMember(t, task.codexHome, secondRel, task.start.Add(tc.secondSpec.metaOffset), tc.secondSpec)
			}

			report := runParentUsageReport(t, task.cfg)
			parent := report.ParentSession
			if parent.Status != codexStatusAmbiguous {
				t.Fatalf("parent session = %#v", parent)
			}
			if !strings.Contains(parent.Detail, "2 rollouts share the stored parent thread ID") ||
				!strings.Contains(parent.Detail, tc.reason) {
				t.Fatalf("parent detail = %s want reason %s", parent.Detail, tc.reason)
			}
			if len(parent.RolloutChain) != 0 {
				t.Fatalf("ambiguous chain lists rollouts: %#v", parent.RolloutChain)
			}
			if report.Intervals.TaskExecution.Tokens.Status != codexStatusAmbiguous ||
				report.Intervals.TaskExecution.Activity.Status != codexStatusAmbiguous {
				t.Fatalf("execution interval = %#v", report.Intervals.TaskExecution)
			}
		})
	}
}

func writeChainSecondMember(t *testing.T, home, rel string, meta time.Time, spec chainSecondSpec) {
	t.Helper()
	cwd := spec.cwd
	if cwd == "" {
		cwd = "/repo"
	}
	lines := make([]string, 0, 2)
	if spec.malformed {
		lines = append(lines, "{malformed\n")
	}
	if spec.continuedCounter {
		lines = append(lines, chainTokenCountLine(t, meta.Add(time.Minute), 17000000, 8000000, 5000, 2000, 17000000, 1000))
	} else {
		lines = append(lines, chainTokenCountLine(t, meta.Add(time.Minute), 200, 100, 50, 20, 200, 200))
	}
	writeChainRollout(t, home, rel, codexTestParentThreadID, meta, cwd, "Codex Desktop", lines)
}

func TestBundleCanonicalChainCollectsEachRolloutFile(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	firstRel := analysisRolloutRel()
	secondRel := "archived_sessions/rollout-resumed-" + codexTestParentThreadID + ".jsonl"
	splitAt := task.start.Add(10 * time.Minute)
	firstLines := []string{
		chainTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500, 1500),
		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		chainTokenCountLine(t, task.start.Add(5*time.Minute), 1500, 700, 300, 200, 2300, 800),
	}
	secondLines := []string{
		chainTokenCountLine(t, splitAt.Add(time.Minute), 400, 200, 100, 50, 500, 500),
		chainTokenCountLine(t, splitAt.Add(10*time.Minute), 1200, 600, 260, 130, 1200, 700),
		analysisTurnLine(t, task.completeAt, codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
	writeChainRollout(t, task.codexHome, firstRel, codexTestParentThreadID, task.start.Add(-3*time.Hour), "/repo", "Codex Desktop", firstLines)
	writeChainRollout(t, task.codexHome, secondRel, codexTestParentThreadID, splitAt, "/repo", "Codex Desktop", secondLines)
	writeCodexConfigToml(t, task.codexHome, true)

	index, archive := runAnalysisBundleFull(t, task.cfg, "")
	firstArchive := "codex-parent/rollouts/" + codexTestParentThreadID + ".jsonl"
	secondArchive := "codex-parent/rollouts/" + codexTestParentThreadID + "-2.jsonl"
	if _, ok := archive[firstArchive]; !ok {
		t.Fatalf("archive missing %s", firstArchive)
	}
	if _, ok := archive[secondArchive]; !ok {
		t.Fatalf("archive missing %s", secondArchive)
	}
	if !strings.Contains(string(archive[secondArchive]), "session_meta") {
		t.Fatalf("second chain member content = %s", archive[secondArchive])
	}
	if len(index.ParentSession.RolloutArchivePaths) != 2 ||
		index.ParentSession.RolloutArchivePaths[0] != firstArchive ||
		index.ParentSession.RolloutArchivePaths[1] != secondArchive {
		t.Fatalf("index rollout archive paths = %#v", index.ParentSession.RolloutArchivePaths)
	}
	var manifest bundleManifest
	if err := json.Unmarshal(archive["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	parent := findCodexEvidence(t, manifest, codexClassParentSession)
	if parent.Status != codexStatusIncluded || !parent.SpansTasks ||
		len(parent.Sources) != 2 || parent.Sources[0] != firstRel || parent.Sources[1] != secondRel ||
		len(parent.ArchivePaths) != 2 || parent.ArchivePaths[1] != secondArchive {
		t.Fatalf("parent evidence = %#v", parent)
	}
}

func writeChainRollout(t *testing.T, home, rel, threadID string, metaTimestamp time.Time, cwd, originator string, lines []string) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"timestamp": metaTimestamp.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id":         threadID,
			"cwd":        cwd,
			"originator": originator,
			"source":     "vscode",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeBundleFile(t, filepath.Join(home, filepath.FromSlash(rel)), string(encoded)+"\n"+strings.Join(lines, ""))
}

func chainTokenCountLine(t *testing.T, timestamp time.Time, input, cached, output, reasoning, total, lastTotal int64) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "event_msg", map[string]any{
		"type": "token_count",
		"info": map[string]any{
			"total_token_usage": map[string]any{
				"input_tokens":            input,
				"cached_input_tokens":     cached,
				"output_tokens":           output,
				"reasoning_output_tokens": reasoning,
				"total_tokens":            total,
			},
			"last_token_usage": map[string]any{
				"input_tokens": lastTotal,
				"total_tokens": lastTotal,
			},
		},
	})
}
