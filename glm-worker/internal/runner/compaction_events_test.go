package runner

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReduceStreamEventPreservesOnlySanitizedCompactionMetadata(t *testing.T) {
	raw := []byte(`{"type":"system","subtype":"compact_boundary","content":"Conversation compacted","compactMetadata":{"trigger":"auto","preTokens":167257,"postTokens":15406,"durationMs":94025,"cumulativeDroppedTokens":151851,"preservedSegment":{"headUuid":"secret-head","anchorUuid":"secret-anchor"},"preservedMessages":{"uuids":["secret-message"]},"summary":"secret-summary"}}`)
	record := reduceStreamEvent(raw, state.TaskEventRecord{TaskID: "task", CallID: "call"}, 1, time.Unix(1, 0).UTC())
	if record.Kind != "system" || record.Subtype != "compact_boundary" || record.Compaction == nil {
		t.Fatalf("compaction event = %#v", record)
	}
	got := record.Compaction
	if got.Trigger != "auto" || got.PreTokens != 167257 || got.PostTokens != 15406 ||
		got.DurationMS != 94025 || got.CumulativeDroppedTokens != 151851 {
		t.Fatalf("compaction metadata = %#v", got)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-head", "secret-anchor", "secret-message", "secret-summary", "preservedSegment", "preservedMessages"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("sanitized event contains %q: %s", forbidden, encoded)
		}
	}
}

func TestReduceStreamEventDropsUnknownCompactionTrigger(t *testing.T) {
	raw := []byte(`{"type":"system","subtype":"compact_boundary","compactMetadata":{"trigger":"provider-specific","preTokens":10}}`)
	record := reduceStreamEvent(raw, state.TaskEventRecord{}, 1, time.Unix(1, 0).UTC())
	if record.Compaction == nil || record.Compaction.Trigger != "" || record.Compaction.PreTokens != 10 {
		t.Fatalf("compaction metadata = %#v", record.Compaction)
	}
}
