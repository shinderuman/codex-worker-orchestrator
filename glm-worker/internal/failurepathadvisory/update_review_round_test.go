package failurepathadvisory

import (
	"path/filepath"
	"testing"
)

func TestAppendRecordPersistsSeparateReviewRounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), RegistryFile)
	first := observedRecord("task-round")
	first.ReviewNumber = 1
	second := observedRecord("task-round")
	second.ReviewNumber = 2
	if _, err := AppendRecord(path, first); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendRecord(path, second); err != nil {
		t.Fatal(err)
	}
	duplicate := second
	duplicate.Detail = "must not replace the canonical round record"
	if _, err := AppendRecord(path, duplicate); err != nil {
		t.Fatal(err)
	}
	registry, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Records) != 2 {
		t.Fatalf("records = %+v", registry.Records)
	}
	if registry.Records[0].ReviewNumber != 1 || registry.Records[1].ReviewNumber != 2 {
		t.Fatalf("review-scoped records = %+v", registry.Records)
	}
	if registry.Records[1].Detail != second.Detail {
		t.Fatalf("same-review duplicate replaced persisted record: %+v", registry.Records[1])
	}
}
