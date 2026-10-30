package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestInstalledCompletionRuntimeRequiresExactCleanSource(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"same", "different-binary-source", "modified-build", "different-checkout"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			const source = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			revision, head, modified := source, source, false
			switch scenario {
			case "different-binary-source":
				revision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			case "modified-build":
				modified = true
			case "different-checkout":
				head = "cccccccccccccccccccccccccccccccccccccccc"
			}
			output, err := json.Marshal(struct {
				Build statusRuntimeBuild `json:"runtime_build"`
			}{statusRuntimeBuild{VCSRevision: &revision, VCSModified: &modified, RepositoryHead: &head, Relationship: runtimeBuildSame}})
			if err != nil {
				t.Fatal(err)
			}
			script := []byte("#!/bin/sh\n[ \"$1\" = --status ] || exit 2\nprintf '%s\\n' '" + string(output) + "'\n")
			path := filepath.Join(root, "glm-worker")
			if err := execfile.Write(path, script, 0o755); err != nil {
				t.Fatal(err)
			}
			identity, err := installedCompletionRuntime(path, root, source)
			if scenario != "same" {
				if err == nil {
					t.Fatal("installed/source mismatch was accepted")
				}
				return
			}
			sum := sha256.Sum256(script)
			if err != nil || identity.SourceOID != source || identity.BinarySHA256 != hex.EncodeToString(sum[:]) {
				t.Fatalf("installed identity: %#v %v", identity, err)
			}
		})
	}
}
