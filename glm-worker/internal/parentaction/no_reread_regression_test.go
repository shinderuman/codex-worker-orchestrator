package parentaction

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

type noRereadRegression struct {
	Version                  int      `json:"version"`
	NormalActionFamily       []string `json:"normal_action_family"`
	NormalSequence           []string `json:"normal_sequence"`
	ForbiddenFreshStageReads []string `json:"forbidden_fresh_stage_reads"`
	ReadExceptions           []string `json:"read_exceptions"`
	SemanticOwner            string   `json:"semantic_owner"`
	TransportOwner           string   `json:"transport_owner"`
	StandardEditBoundary     string   `json:"standard_edit_boundary"`
	ExtraModelCalls          int      `json:"extra_model_calls"`
}

func TestStagedParentActionNoRereadRegressionTracksPayloadActionFamily(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "tests", "staged-parent-action-no-reread.json"))
	if err != nil {
		t.Fatal(err)
	}
	var regression noRereadRegression
	if err := json.Unmarshal(data, &regression); err != nil {
		t.Fatal(err)
	}
	if regression.Version != 1 {
		t.Fatalf("regression version = %d want 1", regression.Version)
	}

	wantActions := make([]string, 0, len(payloadActions))
	for action := range payloadActions {
		wantActions = append(wantActions, string(action))
	}
	sort.Strings(wantActions)
	gotActions := append([]string(nil), regression.NormalActionFamily...)
	sort.Strings(gotActions)
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("normal action family = %#v want %#v", gotActions, wantActions)
	}

	if want := []string{"prepare", "validate", "apply_patch", "action"}; !reflect.DeepEqual(regression.NormalSequence, want) {
		t.Fatalf("normal sequence = %#v want %#v", regression.NormalSequence, want)
	}
	for _, forbidden := range []string{
		"sed -n .glm-worker-parent-actions/...",
		"cat .glm-worker-parent-actions/...",
		"read-tool .glm-worker-parent-actions/...",
	} {
		if !containsRegressionValue(regression.ForbiddenFreshStageReads, forbidden) {
			t.Errorf("formal no-reread regression missing %q", forbidden)
		}
	}
	if want := []string{"debug", "recovery"}; !reflect.DeepEqual(regression.ReadExceptions, want) {
		t.Fatalf("read exceptions = %#v want %#v", regression.ReadExceptions, want)
	}
	if regression.SemanticOwner != "parent" || regression.TransportOwner != "machine" {
		t.Fatalf("authority owners = semantic:%q transport:%q", regression.SemanticOwner, regression.TransportOwner)
	}
	if regression.StandardEditBoundary != "apply_patch" {
		t.Fatalf("standard edit boundary = %q", regression.StandardEditBoundary)
	}
	if regression.ExtraModelCalls != 0 {
		t.Fatalf("extra model calls = %d want 0", regression.ExtraModelCalls)
	}
}

func containsRegressionValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
