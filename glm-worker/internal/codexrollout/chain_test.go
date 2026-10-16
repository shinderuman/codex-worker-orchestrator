package codexrollout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func chainUsage(input, cached, output, reasoning, total int64) map[string]any {
	return map[string]any{
		"input_tokens":            input,
		"cached_input_tokens":     cached,
		"output_tokens":           output,
		"reasoning_output_tokens": reasoning,
		"total_tokens":            total,
	}
}

func chainUsageWithout(usage map[string]any, field string) map[string]any {
	reduced := make(map[string]any, len(usage))
	for key, value := range usage {
		reduced[key] = value
	}
	delete(reduced, field)
	return reduced
}

func chainCounterLine(t *testing.T, at time.Time, usage map[string]any, lastTotal int64) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"timestamp": at.UTC().Format(time.RFC3339Nano),
		"type":      "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"total_token_usage": usage,
				"last_token_usage":  map[string]any{"total_tokens": lastTotal},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

func chainEventlessLine(t *testing.T, at time.Time) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"timestamp": at.UTC().Format(time.RFC3339Nano),
		"type":      "response_item",
		"payload":   map[string]any{"type": "message"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

func chainWrite(t *testing.T, dir, name, lines string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func chainMember(path, rel string, first time.Time, cwd string) Rollout {
	return Rollout{
		AbsolutePath:   path,
		HomeRelative:   rel,
		FirstTimestamp: first,
		Cwd:            cwd,
		Originator:     "Codex Desktop",
		SourceRaw:      `"vscode"`,
	}
}

func TestResolveChainClassifiesCounterBoundaries(t *testing.T) {
	open := time.Date(2026, 10, 13, 2, 43, 25, 0, time.UTC)
	segmentOpen := chainUsage(32280, 22400, 122, 0, 32402)
	segmentClose := chainUsage(78648681, 77080320, 365757, 213383, 79014438)
	restartUsage := chainUsage(296380, 284032, 2124, 1293, 298504)
	previousLines := chainCounterLine(t, open.Add(time.Minute), segmentOpen, 32402) +
		chainCounterLine(t, open.Add(10*time.Hour), segmentClose, 298504)
	cases := []struct {
		name             string
		previousLines    string
		currentLines     string
		currentCwd       string
		currentFirst     time.Time
		wantReason       string
		wantContinuation bool
	}{
		{
			name:             "carried cumulative counter continues",
			previousLines:    previousLines,
			currentLines:     chainCounterLine(t, open.Add(11*time.Hour), segmentClose, 298504) + chainCounterLine(t, open.Add(11*time.Hour+time.Minute), chainUsage(78648781, 77080420, 365857, 213483, 79014538), 301000),
			currentFirst:     open.Add(11 * time.Hour),
			wantContinuation: true,
		},
		{
			name:             "continuation is not counted as restart when last usage equals cumulative usage",
			previousLines:    previousLines,
			currentLines:     chainCounterLine(t, open.Add(11*time.Hour), segmentClose, 79014438),
			currentFirst:     open.Add(11 * time.Hour),
			wantContinuation: true,
		},
		{
			name:          "self-contained restart within first counter event",
			previousLines: previousLines,
			currentLines:  chainCounterLine(t, open.Add(11*time.Hour), restartUsage, 298504),
			currentFirst:  open.Add(11 * time.Hour),
		},
		{
			name:          "self-contained restart by lower total",
			previousLines: previousLines,
			currentLines:  chainCounterLine(t, open.Add(11*time.Hour), chainUsage(100, 50, 10, 5, 110), 90),
			currentFirst:  open.Add(11 * time.Hour),
		},
		{
			name:          "forward jump across boundary",
			previousLines: previousLines,
			currentLines:  chainCounterLine(t, open.Add(11*time.Hour), chainUsage(78648682, 77080321, 365758, 213384, 79014439), 298505),
			currentFirst:  open.Add(11 * time.Hour),
			wantReason:    "advances beyond the previous segment token counter",
		},
		{
			name:          "partial field match across boundary",
			previousLines: previousLines,
			currentLines:  chainCounterLine(t, open.Add(11*time.Hour), chainUsage(78648682, 77080320, 365757, 213383, 79014438), 298504),
			currentFirst:  open.Add(11 * time.Hour),
			wantReason:    "neither restarts nor exactly continues the token counter",
		},
		{
			name:          "missing field on current carry",
			previousLines: previousLines,
			currentLines:  chainCounterLine(t, open.Add(11*time.Hour), chainUsageWithout(segmentClose, "reasoning_output_tokens"), 298504),
			currentFirst:  open.Add(11 * time.Hour),
			wantReason:    "neither restarts nor exactly continues the token counter",
		},
		{
			name: "missing field on previous last anchor",
			previousLines: chainCounterLine(t, open.Add(time.Minute), segmentOpen, 32402) +
				chainCounterLine(t, open.Add(10*time.Hour), chainUsageWithout(segmentClose, "cached_input_tokens"), 298504),
			currentLines: chainCounterLine(t, open.Add(11*time.Hour), segmentClose, 298504),
			currentFirst: open.Add(11 * time.Hour),
			wantReason:   "neither restarts nor exactly continues the token counter",
		},
		{
			name:          "missing continuation field is not rescued by last usage",
			previousLines: previousLines,
			currentLines:  chainCounterLine(t, open.Add(11*time.Hour), chainUsageWithout(segmentClose, "reasoning_output_tokens"), 79014438),
			currentFirst:  open.Add(11 * time.Hour),
			wantReason:    "neither restarts nor exactly continues the token counter",
		},
		{
			name:          "terminal anchor missing total is not replaced by an older anchor",
			previousLines: previousLines + chainCounterLine(t, open.Add(10*time.Hour+time.Minute), chainUsageWithout(segmentClose, "total_tokens"), 298504),
			currentLines:  chainCounterLine(t, open.Add(11*time.Hour), segmentClose, 298504),
			currentFirst:  open.Add(11 * time.Hour),
			wantReason:    "no usable token counter anchor",
		},
		{
			name:          "current segment without counter anchor",
			previousLines: previousLines,
			currentLines:  chainEventlessLine(t, open.Add(11*time.Hour)),
			currentFirst:  open.Add(11 * time.Hour),
			wantReason:    "no usable token counter anchor",
		},
		{
			name:          "identity mismatch between segments",
			previousLines: previousLines,
			currentLines:  chainCounterLine(t, open.Add(11*time.Hour), restartUsage, 298504),
			currentCwd:    "/other-repo",
			currentFirst:  open.Add(11 * time.Hour),
			wantReason:    "disagree on session identity",
		},
		{
			name:          "overlapping segment ranges",
			previousLines: previousLines,
			currentLines:  chainCounterLine(t, open.Add(11*time.Hour), restartUsage, 298504),
			currentFirst:  open.Add(5 * time.Hour),
			wantReason:    "overlapping event ranges",
		},
		{
			name:          "duplicate identical content",
			previousLines: previousLines,
			currentLines:  previousLines,
			currentFirst:  open.Add(11 * time.Hour),
			wantReason:    "duplicate identical content",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cwd := "/repo"
			if tc.currentCwd != "" {
				cwd = tc.currentCwd
			}
			previous := chainMember(chainWrite(t, dir, "previous.jsonl", tc.previousLines), "sessions/previous.jsonl", open, "/repo")
			matches := []Rollout{previous}
			if tc.currentLines != "" {
				current := chainMember(chainWrite(t, dir, "current.jsonl", tc.currentLines), "sessions/current.jsonl", tc.currentFirst, cwd)
				matches = append(matches, current)
			}
			chain, reason := ResolveChain(matches)
			if tc.wantReason != "" {
				if reason == "" || !strings.Contains(reason, tc.wantReason) {
					t.Fatalf("reason = %q, want substring %q", reason, tc.wantReason)
				}
				return
			}
			if reason != "" {
				t.Fatalf("reason = %q, want acceptance", reason)
			}
			if len(chain) != len(matches) {
				t.Fatalf("chain length = %d, want %d", len(chain), len(matches))
			}
			if chain[0].CounterContinuation {
				t.Fatalf("first member must not carry a continuation boundary")
			}
			if len(chain) > 1 && chain[1].CounterContinuation != tc.wantContinuation {
				t.Fatalf("continuation = %v, want %v", chain[1].CounterContinuation, tc.wantContinuation)
			}
		})
	}
}
