package controller

import (
	"path/filepath"
	"testing"
)

func TestCanonicalAuthorityActiveDistinguishesPristineAndActivatedStores(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	cfg := controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions"))

	active, err := CanonicalAuthorityActive(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("missing controller store was reported as canonical authority")
	}
	if _, err := Open(cfg); err != nil {
		t.Fatal(err)
	}
	active, err = CanonicalAuthorityActive(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("pristine controller store was reported as canonical authority")
	}
}
