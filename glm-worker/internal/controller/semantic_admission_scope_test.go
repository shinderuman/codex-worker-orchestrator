package controller

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestCurrentSemanticAdmissionRejectsForeignConfigWithoutChangingStore(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	cfg := controllerTestConfig(repo, filepath.Join(t.TempDir(), "sessions"))
	original, err := Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		repo string
		base string
	}{
		{name: "foreign repository", repo: newControllerRepo(t), base: cfg.StateBase},
		{name: "foreign state root", repo: repo, base: filepath.Join(t.TempDir(), "sessions")},
		{name: "foreign runtime base under same controller root", repo: repo, base: filepath.Join(filepath.Dir(cfg.StateBase), "other-sessions")},
	} {
		t.Run(test.name, func(t *testing.T) {
			foreign := cfg
			foreign.RepoRoot, foreign.StateBase = test.repo, test.base
			if _, err := CurrentSemanticAdmission(foreign, store); err == nil {
				t.Fatal("foreign config was admitted by the current Store")
			}
			head, err := store.LoadHead()
			if err != nil || !reflect.DeepEqual(head, original.Head) {
				t.Fatalf("foreign config changed current authority: %+v %v", head, err)
			}
			if exists, err := Exists(foreign); err != nil || exists {
				if test.repo != repo || filepath.Dir(test.base) != filepath.Dir(cfg.StateBase) {
					t.Fatalf("admission initialized a foreign controller: %t %v", exists, err)
				}
			}
		})
	}
	current, err := CurrentSemanticAdmission(cfg, store)
	if err != nil || current.Attempt.AttemptID != original.Attempt.AttemptID {
		t.Fatalf("matching config lost canonical admission: %+v %v", current, err)
	}
}
