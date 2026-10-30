package dockertest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestDockerTargetFailureRetainsExitProvenance(t *testing.T) {
	root := sourceFixture(t)
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := execfile.Write(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nprintf 'target failure\n'\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var log bytes.Buffer
	d := diagnostic{PhasesMS: map[string]int64{}}
	x := execution{ctx: context.Background(), root: root, container: "fixture", snapshot: snapshot, detail: &d, stdout: &log, stderr: &log}
	code, err := x.test([]string{"./..."})
	var exit *exec.ExitError
	if code != 7 || !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit provenance lost: code=%d error=%v", code, err)
	}
	if d.SourceChanged == nil || *d.SourceChanged {
		t.Fatal("unchanged source not recorded")
	}
}

func TestDockerSuccessfulTargetCannotHideSourceDrift(t *testing.T) {
	root := sourceFixture(t)
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	t.Setenv("DOCKER_TEST_SOURCE", root)
	body := "#!/bin/sh\nprintf changed > \"$DOCKER_TEST_SOURCE/dirty.txt\"\nexit 0\n"
	if err := execfile.Write(filepath.Join(bin, "docker"), []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := diagnostic{PhasesMS: map[string]int64{}}
	x := execution{ctx: context.Background(), root: root, container: "fixture", snapshot: snapshot, detail: &d, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	code, err := x.test([]string{"./..."})
	if code != 1 || !errors.Is(err, errSourceChanged) || d.SourceChanged == nil || !*d.SourceChanged {
		t.Fatalf("source drift accepted: code=%d error=%v", code, err)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		t.Fatal("wrapper source failure classified as a target exit")
	}
}

func TestCanonicalTargetExitRequiresAttestation(t *testing.T) {
	for _, tc := range []struct {
		name, marker string
		proxy, want  int
		target       bool
	}{
		{"success", "0", 0, 0, false}, {"target failure", "7", 7, 7, true}, {"proxy mismatch", "0", 1, 1, false}, {"invalid evidence", "unknown", 0, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := sourceFixture(t)
			snapshot, err := state.CaptureGitSnapshot(root)
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			t.Setenv("MOCK_TARGET_EXIT", tc.marker)
			t.Setenv("MOCK_PROXY_EXIT", fmt.Sprint(tc.proxy))
			script := `#!/bin/sh
if [ "$5" = cat ]; then printf '%s\n' "$MOCK_TARGET_EXIT"; exit 0; fi
exit "$MOCK_PROXY_EXIT"
`
			if err := execfile.Write(filepath.Join(bin, "docker"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			d := diagnostic{PhasesMS: map[string]int64{}}
			x := execution{ctx: context.Background(), root: root, container: "fixture", snapshot: snapshot, detail: &d, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
			code, err := x.testCanonical([]string{"test", "-json", "./..."})
			var target *qualitygate.TargetExitError
			if code != tc.want || errors.As(err, &target) != tc.target || (err == nil) != (tc.name == "success") {
				t.Fatalf("code=%d target=%v error=%v", code, target, err)
			}
		})
	}
}
