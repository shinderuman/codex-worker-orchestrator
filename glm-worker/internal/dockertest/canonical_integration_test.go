//go:build dockerintegration

package dockertest

import (
	"bytes"
	"context"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCanonicalDockerExecutionAttestsActualPlatform(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	archive, _, err := sourceArchive(context.Background(), root, temporary, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	copied := t.TempDir()
	cmd := exec.Command("tar", "-xf", archive, "-C", copied)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("candidate source extraction: %v %s", err, data)
	}
	copiedSnapshot, err := state.CaptureGitSnapshot(copied)
	if err != nil || copiedSnapshot.Head != snapshot.Head || copiedSnapshot.IndexDigest != snapshot.IndexDigest || copiedSnapshot.WorktreeDigest != snapshot.WorktreeDigest {
		t.Fatalf("candidate source binding differs: %v", err)
	}
	root = copied
	var log bytes.Buffer
	toolchain, evidence, err := RunCanonical(context.Background(), root, []string{"test", "-json", "-count=1", "-run", "^Test(DockerPassRequiresBothPlatformsAndMatchingSource|RealHarnesslintIsolatedFixerFeedsLiveLeaseMutation|SettingsMergeLockSerializesSameTarget|ManagedSettings.*|MergeFiles.*|Run)$", "./internal/qualitygate", "./internal/settingsmerge", "./internal/mergejsoncmd", "./internal/workflow"}, &log, &log)
	if err != nil {
		t.Fatalf("Docker execution: %v\n%s", err, log.String())
	}
	if toolchain == nil || !toolchain.MatchesRequiredGoVersion() || toolchain.GOOS != "linux" || toolchain.GOARCH != "arm64" {
		t.Fatalf("actual toolchain: %#v", toolchain)
	}
	if evidence == nil || evidence.Status != "pass" || evidence.SourceChanged == nil || *evidence.SourceChanged || evidence.ImageID == "" || evidence.ArchiveSHA256 == "" || evidence.PhasesMS["cleanup"] < 0 {
		t.Fatalf("Docker evidence: %#v", evidence)
	}
	if !bytes.Contains(log.Bytes(), []byte(`"Action":"pass"`)) {
		t.Fatalf("actual test output missing: %s", log.String())
	}
	t.Logf("platform=%s/%s go=%s total_ms=%d preparation_ms=%d test_ms=%d image=%s", toolchain.GOOS, toolchain.GOARCH, toolchain.GoVersion, evidence.TotalMS, evidence.PreparationMS, evidence.TestMS, evidence.ImageID)
}
