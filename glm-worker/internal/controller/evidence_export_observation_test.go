package controller

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEvidenceExportObservationAndFileChangeDetection(t *testing.T) {
	t.Parallel()
	before := RepositoryControllerHead{ControllerGeneration: 3, LiveAttemptID: "a1", LiveLeaseID: "l1"}
	same := evidenceExportControllerObservation(before, before)
	if same.AuthorityChangedDuringExport {
		t.Fatalf("stable head reported authority change: %#v", same)
	}
	after := before
	after.ControllerGeneration = 4
	after.LiveLeaseID = "l2"
	changed := evidenceExportControllerObservation(before, after)
	if !changed.AuthorityChangedDuringExport || changed.GenerationBefore != 3 || changed.GenerationAfter != 4 {
		t.Fatalf("changed head observation = %#v", changed)
	}

	path := filepath.Join(t.TempDir(), "changing.jsonl")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"b\""), 0o600); err != nil {
		t.Fatal(err)
	}
	grown, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !evidenceExportFileChanged(info, grown) {
		t.Fatal("file growth was not detected as a change")
	}
	records, trailing := evidenceExportJSONLStats("live/task/x.jsonl", []byte("{\"a\":1}\n{\"b\""))
	if records != 1 || trailing != true {
		t.Fatalf("jsonl stats records=%d trailing=%t", records, trailing)
	}
	completeRecords, completeTrailing := evidenceExportJSONLStats("live/task/x.jsonl", []byte("{\"a\":1}\n"))
	if completeRecords != 1 || completeTrailing {
		t.Fatalf("complete jsonl stats records=%d trailing=%t", completeRecords, completeTrailing)
	}
}
