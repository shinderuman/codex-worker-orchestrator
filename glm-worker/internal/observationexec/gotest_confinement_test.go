package observationexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const confinementProbeTestSource = `package example

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestConfinedWrites(t *testing.T) {
	if err := os.WriteFile("allowed-local.txt", []byte("ok"), 0o600); err != nil {
		t.Fatalf("隔離copy内の書込が拒否されました: %%v", err)
	}
	if err := os.WriteFile(filepath.Join(os.TempDir(), "allowed-tmp.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("bounded tempへの書込が拒否されました: %%v", err)
	}
	if err := os.WriteFile(%q, []byte("escaped"), 0o600); err == nil {
		t.Fatal("絶対path経由で元repo sentinelへ書けました")
	}
	if err := os.WriteFile(%q, []byte("escaped"), 0o600); err == nil {
		t.Fatal("絶対path経由で許可root外へ書けました")
	}
	if err := os.Symlink(%q, "escape-link"); err != nil {
		t.Fatalf("symlink作成が失敗しました: %%v", err)
	}
	if err := os.WriteFile("escape-link", []byte("escaped"), 0o600); err == nil {
		t.Fatal("symlink経由で許可root外へ書けました")
	}
	command := exec.Command("/bin/sh", "-c", fmt.Sprintf("printf escaped > %%s", %q))
	if err := command.Run(); err == nil {
		t.Fatal("子process経由の書込が成功しました")
	}
}

func TestConfinedNetworkDenied(t *testing.T) {
	if listener, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
		_ = listener.Close()
		t.Fatal("network bindが許可されました")
	}
	if _, err := net.Dial("tcp", "127.0.0.1:1"); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("outbound接続がsandbox拒否されていません: %%v", err)
	}
	child := exec.Command(os.Args[0], "-test.run=TestConfinedNetworkChildProbe")
	if err := child.Run(); err != nil {
		t.Fatalf("子process経由のnetwork bindが許可されました: %%v", err)
	}
}

func TestConfinedNetworkChildProbe(t *testing.T) {
	if listener, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
		_ = listener.Close()
		t.Fatal("子processでnetwork bindが許可されました")
	}
}
`

func TestConfinementAdmissionDeclaresPlatformSupport(t *testing.T) {
	err := ConfinementAdmission()
	if runtime.GOOS == "darwin" {
		if err != nil {
			t.Fatalf("macOSではconfinement admissionが成立するべきです: %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), confinementSupportCondition) {
		t.Fatalf("非対応platformのadmission拒否が支持条件を明示していません: %v", err)
	}
}

func TestRunIsolatedGoTestConfinementUnavailablePreventsTargetStart(t *testing.T) {
	moduleDir := writeIsolatedModule(t, "package example\n\nimport \"os\"\n\nfunc TestTargetStarted(t *testing.T) {\n\t_ = os.WriteFile(\"target-started.txt\", []byte(\"ran\"), 0o600)\n}\n")
	original := observationConfinementAdmission
	defer func() { observationConfinementAdmission = original }()
	observationConfinementAdmission = func() error { return errors.New("confinement unavailable in test") }

	outcome := RunIsolatedGoTest(context.Background(), GoTestInput{
		ModuleDir: moduleDir, ArtifactDir: t.TempDir(), ExecutionID: "confinement-off-0001", DeadlineMS: 60000,
	})
	if outcome.Status != StatusFail || outcome.ExitSource != exitSourceConfinement {
		t.Fatalf("outcome = %+v want fail/confinement", outcome)
	}
	if outcome.LogPath != "" {
		t.Fatalf("confinement失敗時にlog artifactが作られています: %s", outcome.LogPath)
	}
	if _, err := os.Stat(filepath.Join(moduleDir, "target-started.txt")); err == nil {
		t.Fatal("confinement利用不能時にtargetが起動しました")
	}
	if _, err := os.Stat(filepath.Join(moduleDir, "allowed-local.txt")); err == nil {
		t.Fatal("confinement利用不能時に隔離copyが実行されています")
	}
}

func TestRunIsolatedGoTestConfinementInitFailureIsTypedWithoutTargetStart(t *testing.T) {
	probeRoot := t.TempDir()
	preflightErr := ConfinementPreflight(probeRoot)
	if preflightErr == nil {
		t.Skip("この環境ではconfinement初期化が成功するため初期化失敗経路を検証できません")
	}
	moduleDir := writeIsolatedModule(t, "package example\n\nimport \"os\"\n\nfunc TestTargetStarted(t *testing.T) {\n\t_ = os.WriteFile(\"target-started.txt\", []byte(\"ran\"), 0o600)\n}\n")
	outcome := RunIsolatedGoTest(context.Background(), GoTestInput{
		ModuleDir: moduleDir, ArtifactDir: t.TempDir(), ExecutionID: "confinement-init-0001", DeadlineMS: 60000,
	})
	if outcome.Status != StatusFail || outcome.ExitSource != exitSourceConfinement {
		t.Fatalf("outcome = %+v want fail/confinement", outcome)
	}
	if outcome.LogPath != "" {
		t.Fatalf("confinement初期化失敗時にlog artifactが作られています: %s", outcome.LogPath)
	}
	if fileExists(filepath.Join(moduleDir, "target-started.txt")) {
		t.Fatal("confinement初期化失敗後にtargetが起動しました")
	}
}

func TestRunIsolatedGoTestConfinementBlocksWritesOutsideAllowedRoots(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec confinementはdarwin限定です")
	}
	if testing.Short() {
		t.Skip("隔離go testの実行をskipします")
	}
	if err := ConfinementPreflight(t.TempDir()); err != nil {
		t.Skipf("この実行環境ではconfinement初期化が拒否されるため実書込検証は親validation環境で実行します: %v", err)
	}
	moduleDir := writeIsolatedModule(t, "package example\n\nimport \"testing\"\n\nfunc TestPlaceholder(t *testing.T) {}\n")
	sentinel := filepath.Join(moduleDir, "sentinel-repo.txt")
	if err := os.WriteFile(sentinel, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideDir := t.TempDir()
	outsideDirect := filepath.Join(outsideDir, "outside-direct.txt")
	outsideChild := filepath.Join(outsideDir, "outside-child.txt")
	testSource := fmt.Sprintf(confinementProbeTestSource, sentinel, outsideDirect, outsideDirect, outsideChild)
	writeIsolatedModuleFile(t, moduleDir, "example_test.go", testSource)

	outcome := RunIsolatedGoTest(context.Background(), GoTestInput{
		ModuleDir: moduleDir, ArtifactDir: t.TempDir(), ExecutionID: "confinement-run-001", DeadlineMS: 300000,
	})
	if outcome.Status != StatusPass {
		t.Fatalf("confinement下の許可内実行が失敗しました: %+v log=%s", outcome, readOutcomeLog(t, outcome.LogPath))
	}
	sentinelContent, err := os.ReadFile(sentinel)
	if err != nil || string(sentinelContent) != "original" {
		t.Fatalf("元repo sentinelが変化しています: %q err=%v", sentinelContent, err)
	}
	for _, outside := range []string{outsideDirect, outsideChild} {
		if fileExists(outside) {
			t.Fatalf("許可root外のfile %sが作成されています", outside)
		}
	}
	if !fileExists(filepath.Join(moduleDir, "sentinel-repo.txt")) {
		t.Fatal("sentinel自体が消えています")
	}
}

func readOutcomeLog(t *testing.T, path string) string {
	t.Helper()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeIsolatedModuleFile(t *testing.T, moduleDir string, name string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(moduleDir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
