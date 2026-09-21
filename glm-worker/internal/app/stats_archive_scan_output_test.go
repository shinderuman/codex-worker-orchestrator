package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStatsArchiveScanCoverageDoesNotChangeAggregates(t *testing.T) {
	fixture := newTelemetryCompactFixture(t)

	baselineNormal := executeStatsArchiveScanCommand(t, fixture, []string{"--stats", "current"})
	baselineCompact := executeStatsArchiveScanCommand(t, fixture, []string{"--stats", "current", "--compact"})
	baselineScan, err := fixture.st.ScanTaskStatsArchives()
	if err != nil {
		t.Fatal(err)
	}

	writeUnsupportedStatsArchive(t, fixture, "unsupported-revision-a", 2)
	writeUnsupportedStatsArchive(t, fixture, "unsupported-revision-b", 3)

	afterNormal := executeStatsArchiveScanCommand(t, fixture, []string{"--stats", "current"})
	afterCompact := executeStatsArchiveScanCommand(t, fixture, []string{"--stats", "current", "--compact"})

	assertStatsArchiveScanDelta(t, afterNormal, baselineScan.FilesConsidered+2, baselineScan.FilesAccepted, baselineScan.UnsupportedSchemaOrRevisionSkipped+2)
	assertStatsArchiveScanDelta(t, afterCompact, baselineScan.FilesConsidered+2, baselineScan.FilesAccepted, baselineScan.UnsupportedSchemaOrRevisionSkipped+2)

	if !reflect.DeepEqual(withoutStatsArchiveScan(baselineNormal), withoutStatsArchiveScan(afterNormal)) {
		t.Fatalf("normal stats aggregate changed after unsupported archives: before=%#v after=%#v", baselineNormal, afterNormal)
	}
	if !reflect.DeepEqual(withoutStatsArchiveScan(baselineCompact), withoutStatsArchiveScan(afterCompact)) {
		t.Fatalf("compact stats aggregate changed after unsupported archives: before=%#v after=%#v", baselineCompact, afterCompact)
	}

	callOutliersCompact := executeStatsArchiveScanCommand(t, fixture, []string{"--call-outliers", "current", "--compact"})
	if !reflect.DeepEqual(afterCompact, callOutliersCompact) {
		t.Fatalf("shared compact summary diverged: stats=%#v call-outliers=%#v", afterCompact, callOutliersCompact)
	}
}

func executeStatsArchiveScanCommand(t *testing.T, fixture telemetryCompactFixture, args []string) map[string]any {
	t.Helper()
	cmd, err := ParseCommand(args)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Execute(cmd, fixture.cfg, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	return decodeSingleLineJSON(t, out.String())
}

func writeUnsupportedStatsArchive(t *testing.T, fixture telemetryCompactFixture, taskID string, schemaRevision int) {
	t.Helper()
	path := fixture.st.TaskStatsArchivePath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(fmt.Sprintf(`{"version":3,"schema_revision":%d,"task_id":%q,"model_calls":999999}`, schemaRevision, taskID))
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertStatsArchiveScanDelta(t *testing.T, decoded map[string]any, considered, accepted, skipped int) {
	t.Helper()
	scan, ok := decoded["task_stats_archive_scan"].(map[string]any)
	if !ok {
		t.Fatalf("task_stats_archive_scan missing: %#v", decoded)
	}
	if got := int(scan["files_considered"].(float64)); got != considered {
		t.Fatalf("files considered = %d, want %d", got, considered)
	}
	if got := int(scan["files_accepted"].(float64)); got != accepted {
		t.Fatalf("files accepted = %d, want %d", got, accepted)
	}
	if got := int(scan["unsupported_schema_or_revision_skipped"].(float64)); got != skipped {
		t.Fatalf("unsupported skipped = %d, want %d", got, skipped)
	}
}

func withoutStatsArchiveScan(decoded map[string]any) map[string]any {
	copy := make(map[string]any, len(decoded)-1)
	for key, value := range decoded {
		if key != "task_stats_archive_scan" {
			copy[key] = value
		}
	}
	return copy
}
