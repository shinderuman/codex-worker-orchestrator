package bundleanalysis

import (
	"strings"
	"testing"
	"time"
)

func TestParentUsageIntermediateMemberWithoutCounterAnchor(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_000, 0).UTC()
	line := func(lines ...string) []byte {
		return []byte(strings.Join(lines, "\n") + "\n")
	}
	observed := ObserveParentUsage([]ParentUsageEvidence{
		{Source: "first.jsonl", Content: line(parentUsageTokenCountLine(t, start.Add(-time.Second), 2, 1, 1, 0, 2))},
		{Source: "middle.jsonl", Content: line(
			parentUsageToolCallLine(t, start.Add(time.Second), "tool-in-gap"),
			parentUsageToolOutputLine(t, start.Add(2*time.Second), "tool-in-gap", "result"),
		)},
		{Source: "last.jsonl", Content: line(parentUsageTokenCountLine(t, start.Add(3*time.Second), 13, 6, 2, 1, 13))},
	}, start, start.Add(4*time.Second))

	tokens := observed.Tokens
	if tokens.Status != analysisStatusUnknown || tokens.Reason != parentUsageReasonUnobservedMember || tokens.TotalTokens != 0 || tokens.InputTokens != 0 {
		t.Fatalf("unanchored middle file counted as known consumption: %#v", tokens)
	}
	if len(tokens.UnknownFields) != 5 {
		t.Fatalf("missing field evidence: %#v", tokens.UnknownFields)
	}
	for _, field := range tokens.UnknownFields {
		if field.Reason != parentUsageReasonUnobservedMember || field.Source != "middle.jsonl" {
			t.Fatalf("missing gap provenance: %#v", tokens.UnknownFields)
		}
	}
	if observed.Activity.ToolCalls != 1 || observed.Activity.ToolResults != 1 {
		t.Fatalf("unanchored file activity lost: %#v", observed.Activity)
	}
}

func TestParentUsageUnobservedMemberBeforeEndBound(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_000, 0).UTC()
	observed := ParentUsageEvidence{Source: "observed.jsonl", Content: []byte(strings.Join([]string{
		parentUsageTokenCountLine(t, start.Add(-time.Second), 2, 1, 1, 0, 2),
		parentUsageTokenCountLine(t, start.Add(time.Second), 13, 6, 2, 1, 13),
	}, "\n") + "\n")}
	for _, delta := range []time.Duration{2 * time.Second, 5 * time.Second} {
		tail := ParentUsageEvidence{Source: "tail.jsonl", Content: []byte(parentUsageToolCallLine(t, start.Add(delta), "tool-without-counter") + "\n")}
		report := ObserveParentUsage([]ParentUsageEvidence{observed, tail}, start, start.Add(4*time.Second))
		if delta < 4*time.Second {
			if report.Tokens.Status != analysisStatusUnknown || report.Tokens.Reason != parentUsageReasonUnobservedMember ||
				report.Tokens.TotalTokens != 0 || report.Activity.ToolCalls != 1 {
				t.Fatalf("unobserved member within the window: %#v", report)
			}
			continue
		}
		if report.Tokens.Status != analysisStatusAvailable || report.Tokens.TotalTokens != 11 || report.Activity.ToolCalls != 0 {
			t.Fatalf("member after the window altered known usage: %#v", report)
		}
	}
}

func TestParentUsageSameAnchorStillReportsUnobservedMember(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_000, 0).UTC()
	first := ParentUsageEvidence{Source: "first.jsonl", Content: []byte(parentUsageTokenCountLine(t, start.Add(-time.Second), 2, 1, 1, 0, 2) + "\n")}
	tail := ParentUsageEvidence{Source: "tail.jsonl", Content: []byte(parentUsageToolCallLine(t, start.Add(time.Second), "tool-without-counter") + "\n")}
	noGap := ObserveParentUsage([]ParentUsageEvidence{first}, start, start.Add(2*time.Second))
	if noGap.Tokens.Status != analysisStatusNoObservation {
		t.Fatalf("single anchor without gap: %#v", noGap.Tokens)
	}
	withGap := ObserveParentUsage([]ParentUsageEvidence{first, tail}, start, start.Add(2*time.Second))
	if withGap.Tokens.Status != analysisStatusUnknown || withGap.Tokens.Reason != parentUsageReasonUnobservedMember || len(withGap.Tokens.UnknownFields) != 5 {
		t.Fatalf("single anchor hid unobserved member: %#v", withGap.Tokens)
	}
	for _, field := range withGap.Tokens.UnknownFields {
		if field.Source != tail.Source {
			t.Fatalf("gap provenance lost: %#v", field)
		}
	}
}

func TestParentUsageCounterContinuityAndSingleFile(t *testing.T) {
	t.Parallel()
	start := time.Unix(2_000, 0).UTC()
	cases := []struct {
		name          string
		files         [][]string
		continuations []bool
		wantStatus    string
		wantTotal     int64
	}{
		{
			name: "single file counter delta",
			files: [][]string{{
				parentUsageTokenCountLine(t, start.Add(-time.Second), 2, 1, 1, 0, 2),
				parentUsageTokenCountLine(t, start.Add(time.Second), 13, 6, 2, 1, 13),
			}},
			continuations: []bool{false},
			wantStatus:    analysisStatusAvailable,
			wantTotal:     11,
		},
		{
			name: "counter continuation across files",
			files: [][]string{
				{parentUsageTokenCountLine(t, start.Add(-time.Second), 2, 1, 1, 0, 2)},
				{
					parentUsageTokenCountLine(t, start.Add(time.Second), 2, 1, 1, 0, 2),
					parentUsageTokenCountLine(t, start.Add(2*time.Second), 13, 6, 2, 1, 13),
				},
			},
			continuations: []bool{false, true},
			wantStatus:    analysisStatusAvailable,
			wantTotal:     11,
		},
		{
			name: "counter restart across files",
			files: [][]string{
				{parentUsageTokenCountLine(t, start.Add(-time.Second), 2, 1, 1, 0, 2)},
				{parentUsageTokenCountLine(t, start.Add(2*time.Second), 13, 6, 2, 1, 13)},
			},
			continuations: []bool{false, false},
			wantStatus:    analysisStatusAvailable,
			wantTotal:     13,
		},
		{
			name: "counter reset within file",
			files: [][]string{{
				parentUsageTokenCountLine(t, start.Add(-time.Second), 12, 6, 3, 1, 18),
				parentUsageTokenCountLine(t, start.Add(time.Second), 2, 1, 1, 0, 2),
			}},
			continuations: []bool{false},
			wantStatus:    analysisStatusCounterReset,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scan := bundleRolloutScan{turnIndex: map[string]int{}}
			for i, lines := range tc.files {
				if err := scanCodexRolloutReader(&scan, strings.NewReader(strings.Join(lines, "\n")+"\n"), tc.name, tc.continuations[i], start, start.Add(4*time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			got := parentUsageAnchoredTokens(scan, start, start.Add(4*time.Second), "rollout")
			if got.Status != tc.wantStatus || got.TotalTokens != tc.wantTotal || len(got.UnknownFields) != 0 {
				t.Fatalf("counter coverage = %#v, want %s total %d", got, tc.wantStatus, tc.wantTotal)
			}
		})
	}
}
