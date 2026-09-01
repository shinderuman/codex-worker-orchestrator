package app

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBundleEvidenceProjectionSerializesIdenticallyAcrossSurfaces(t *testing.T) {
	task := bundleTask{ID: "task-123", Status: "rate-limited", Current: true}
	summary := bundleEvidenceSummary{
		missing:            []string{"missing.jsonl"},
		unattributed:       []string{"unattributed.jsonl"},
		unreadable:         []string{"unreadable.jsonl"},
		evidenceStatus:     "incomplete",
		coverage:           bundleCoveragePartial,
		coverageReasons:    []string{"evidence-missing", "evidence-unreadable"},
		inFlightModelCalls: 2,
	}
	sessionIDs := []string{"session-a", "session-b"}
	projection := summary.projection(task, sessionIDs)

	output := bundleOutput{
		bundleEvidenceProjection: projection,
		ArchivePath:              "/tmp/task-123.zip",
	}
	manifest := bundleManifest{
		Format:                   bundleFormat,
		bundleEvidenceProjection: projection,
		CurrentTask:              true,
		Included:                 []string{"manifest.json"},
		CollectionIndex:          bundleCollectionEntryPath,
		AnalysisIndex:            bundleAnalysisEntryPath,
		CreatedAt:                "2026-09-01T00:00:00Z",
	}

	outputJSON := marshalBundleProjectionMap(t, output)
	manifestJSON := marshalBundleProjectionMap(t, manifest)
	for _, key := range []string{
		"task_id",
		"task_status",
		"evidence_status",
		"coverage",
		"coverage_reasons",
		"coverage_scope",
		"claude_session_ids",
		"in_flight_model_calls",
		"missing",
		"unattributed",
		"unreadable",
	} {
		if !reflect.DeepEqual(outputJSON[key], manifestJSON[key]) {
			t.Fatalf("shared field %q differs: output=%#v manifest=%#v", key, outputJSON[key], manifestJSON[key])
		}
	}

	if outputJSON["archive_path"] != "/tmp/task-123.zip" {
		t.Fatalf("archive_path = %#v", outputJSON["archive_path"])
	}
	if _, ok := manifestJSON["archive_path"]; ok {
		t.Fatal("manifest unexpectedly contains archive_path")
	}
	for _, key := range []string{"format", "current_task", "included", "collection_index", "analysis_index", "created_at"} {
		if _, ok := outputJSON[key]; ok {
			t.Fatalf("output unexpectedly contains manifest-only field %q", key)
		}
		if _, ok := manifestJSON[key]; !ok {
			t.Fatalf("manifest is missing surface-specific field %q", key)
		}
	}
}

func marshalBundleProjectionMap(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
