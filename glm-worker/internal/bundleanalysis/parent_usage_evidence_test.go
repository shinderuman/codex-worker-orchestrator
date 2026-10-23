package bundleanalysis

import (
	"fmt"
	"testing"
	"time"
)

func TestParentUsageEvidenceUsesCanonicalCounterAnchors(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	row := func(at time.Time, input int) string {
		return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":0,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":%d}}}}`, at.Format(time.RFC3339Nano), input, input+10) + "\n"
	}
	evidence := ParentUsageEvidence{Source: "parent/session.jsonl", Content: []byte(row(start.Add(-time.Second), 100) + row(start.Add(time.Second), 400))}
	observed := ObserveParentUsage([]ParentUsageEvidence{evidence}, start, start.Add(2*time.Second))
	if observed.Tokens.InputTokens != 300 || len(observed.Sources) != 1 {
		t.Fatalf("counter delta: %#v", observed)
	}
	unknown := ObserveParentUsage([]ParentUsageEvidence{{Source: evidence.Source, Content: []byte(row(start.Add(time.Second), 400))}}, start, start.Add(2*time.Second))
	if unknown.Tokens.Status == analysisStatusAvailable {
		t.Fatalf("missing baseline was counted as observed: %#v", unknown)
	}
}
