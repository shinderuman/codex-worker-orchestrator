package observationexec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeIsolatedModule(t *testing.T, testFile string) string {
	t.Helper()
	moduleDir := t.TempDir()
	files := map[string]string{
		"go.mod":          "module isolated.test/example\n\ngo 1.22\n",
		"example.go":      "package example\n\nfunc Value() int { return 1 }\n",
		"example.txt":     "testdata\n",
		"example_test.go": testFile,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(moduleDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return moduleDir
}

func snapshotTree(t *testing.T, root string) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return seen
}

func TestRunIsolatedGoTestPassesAndWritesOnlyArtifactLog(t *testing.T) {
	skipWhenConfinementUnavailable(t)
	if testing.Short() {
		t.Skip("隔離go testの実行をskipします")
	}
	moduleDir := writeIsolatedModule(t, "package example\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestValue(t *testing.T) {\n\tif Value() != 1 {\n\t\tt.Fatal(\"unexpected\")\n\t}\n\t_ = os.WriteFile(\"probe-output.txt\", []byte(\"written\"), 0o600)\n}\n")
	before := snapshotTree(t, moduleDir)
	artifactDir := t.TempDir()

	outcome := RunIsolatedGoTest(GoTestInput{
		ModuleDir: moduleDir, ArtifactDir: artifactDir, ExecutionID: "pass-run-0001", DeadlineMS: 120000,
	})
	if outcome.Status != StatusPass || outcome.ExitCode != 0 {
		t.Fatalf("outcome = %s/%d want pass/0: %+v", outcome.Status, outcome.ExitCode, outcome)
	}
	if outcome.LogPath == "" || !strings.HasPrefix(outcome.LogPath, artifactDir) {
		t.Fatalf("log path %qがartifact dir配下ではありません", outcome.LogPath)
	}
	if info, err := os.Stat(outcome.LogPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("log artifactが通常fileとして存在しません: %v", err)
	}
	after := snapshotTree(t, moduleDir)
	probePath := filepath.Join(moduleDir, "probe-output.txt")
	if after["probe-output.txt"] || fileExists(probePath) {
		t.Fatal("隔離copy内のtest書込が元module dirへ漏れています(production write境界違反)")
	}
	if len(after) != len(before) {
		t.Fatalf("元module dirのfile集合が変化しました: before=%d after=%d", len(before), len(after))
	}
}

func TestRunIsolatedGoTestReportsTargetFailureWithoutSuccessPromotion(t *testing.T) {
	skipWhenConfinementUnavailable(t)
	if testing.Short() {
		t.Skip("隔離go testの実行をskipします")
	}
	moduleDir := writeIsolatedModule(t, "package example\n\nimport \"testing\"\n\nfunc TestFails(t *testing.T) {\n\tt.Fatal(\"isolated failure\")\n}\n")
	artifactDir := t.TempDir()

	outcome := RunIsolatedGoTest(GoTestInput{
		ModuleDir: moduleDir, ArtifactDir: artifactDir, ExecutionID: "fail-run-0001", DeadlineMS: 120000,
	})
	if outcome.Status != StatusFail || outcome.ExitCode == 0 {
		t.Fatalf("失敗を成功へ昇格しています: %+v", outcome)
	}
	if outcome.ExitSource != exitSourceTarget {
		t.Fatalf("exit source = %q want target", outcome.ExitSource)
	}
	log, err := os.ReadFile(outcome.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "isolated failure") {
		t.Fatalf("失敗logに失敗原因がありません: %s", log)
	}
}

func TestRunIsolatedGoTestDeadlineIsBounded(t *testing.T) {
	skipWhenConfinementUnavailable(t)
	if testing.Short() {
		t.Skip("隔離go testの実行をskipします")
	}
	moduleDir := writeIsolatedModule(t, "package example\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestStalls(t *testing.T) {\n\ttime.Sleep(30 * time.Second)\n}\n")
	artifactDir := t.TempDir()
	started := time.Now()

	outcome := RunIsolatedGoTest(GoTestInput{
		ModuleDir: moduleDir, ArtifactDir: artifactDir, ExecutionID: "deadline-run-0001", DeadlineMS: 1000,
	})
	elapsed := time.Since(started)
	if outcome.Status != StatusFail || outcome.ExitSource != exitSourceDeadline {
		t.Fatalf("outcome = %+v want fail/deadline", outcome)
	}
	if elapsed > 20*time.Second {
		t.Fatalf("deadline超過後の終了が遅すぎます: %s", elapsed)
	}
}

func TestRunIsolatedGoTestRejectsSymlinkedInModuleInput(t *testing.T) {
	skipWhenConfinementUnavailable(t)
	moduleDir := writeIsolatedModule(t, "package example\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\tif Value() != 1 {\n\t\tt.Fatal(\"unexpected\")\n\t}\n}\n")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "payload.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "payload.txt"), filepath.Join(moduleDir, "linked.txt")); err != nil {
		t.Fatal(err)
	}

	outcome := RunIsolatedGoTest(GoTestInput{
		ModuleDir: moduleDir, ArtifactDir: t.TempDir(), ExecutionID: "symlink-run-0001", DeadlineMS: 60000,
	})
	if outcome.Status != StatusFail || outcome.ExitSource != exitSourceInputInvalid {
		t.Fatalf("outcome = %+v want fail/input", outcome)
	}
	if !strings.Contains(outcome.Detail, "symlink") {
		t.Fatalf("detail %qにsymlink拒否理由がありません", outcome.Detail)
	}
}

func TestRunIsolatedGoTestRejectsUnsafeExecutionID(t *testing.T) {
	moduleDir := writeIsolatedModule(t, "package example\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {}\n")
	outcome := RunIsolatedGoTest(GoTestInput{
		ModuleDir: moduleDir, ArtifactDir: t.TempDir(), ExecutionID: "../escape", DeadlineMS: 60000,
	})
	if outcome.Status != StatusFail || outcome.ExitSource != exitSourceInputInvalid {
		t.Fatalf("outcome = %+v want fail/input", outcome)
	}
}

func skipWhenConfinementUnavailable(t *testing.T) {
	t.Helper()
	if err := ConfinementPreflight(t.TempDir()); err != nil {
		t.Skipf("この実行環境ではconfinement初期化が拒否されるため実行系testは親validation環境で実行します: %v", err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
