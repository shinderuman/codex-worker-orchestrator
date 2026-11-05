package qualitygate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func darwinTestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func darwinTestWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestChangedDarwinPackagesOnlyForCandidateDarwinSource(t *testing.T) {
	root := t.TempDir()
	darwinTestGit(t, root, "init")
	darwinTestGit(t, root, "config", "user.email", "test@example.com")
	darwinTestGit(t, root, "config", "user.name", "Test")
	base := filepath.Join(root, "glm-worker", "internal", "qualitygate")
	darwinTestWrite(t, filepath.Join(base, "sampler_darwin.go"), "package qualitygate\nconst sample = 1\n")
	darwinTestWrite(t, filepath.Join(base, "sampler_darwin_test.go"), "//go:build darwin\n\npackage qualitygate\nimport \"testing\"\nfunc TestDarwinSysctlDecoding(t *testing.T) {}\n")
	darwinTestGit(t, root, "add", ".")
	darwinTestGit(t, root, "commit", "-m", "baseline")
	darwinTestWrite(t, filepath.Join(base, "sampler_darwin.go"), "package qualitygate\nconst sample = 2\n")
	darwinTestWrite(t, filepath.Join(base, "sampler_linux.go"), "package qualitygate\nconst linux = 1\n")
	darwinTestGit(t, root, "add", ".")
	darwinTestGit(t, root, "commit", "-m", "change Darwin source")
	req, err := ChangedDarwinPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []DarwinPackageRequirement{{Package: "./internal/qualitygate", Tests: []string{"TestDarwinSysctlDecoding"}}}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("Darwin acceptance = %#v, want %#v", req, want)
	}
	committed, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	atCommit, err := ChangedDarwinPackagesAtCommit(root, strings.TrimSpace(string(committed)))
	if err != nil || !reflect.DeepEqual(atCommit, want) {
		t.Fatalf("committed Darwin acceptance = %#v %v", atCommit, err)
	}
	darwinTestWrite(t, filepath.Join(base, "sampler_linux.go"), "package qualitygate\nconst linux = 2\n")
	darwinTestGit(t, root, "add", ".")
	darwinTestGit(t, root, "commit", "-m", "only Linux")
	req, err = ChangedDarwinPackages(root)
	if err != nil || len(req) != 0 {
		t.Fatalf("unrelated Task requires Darwin validation: %#v %v", req, err)
	}
	committed, err = exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	atCommit, err = ChangedDarwinPackagesAtCommit(root, strings.TrimSpace(string(committed)))
	if err != nil || len(atCommit) != 0 {
		t.Fatalf("unrelated committed source requires Darwin acceptance: %#v %v", atCommit, err)
	}
}

func darwinAcceptanceEvent(t *testing.T, action, pkg, test string) []byte {
	t.Helper()
	out, err := json.Marshal(map[string]string{"Action": action, "Package": pkg, "Test": test})
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
}

func TestDarwinAcceptanceRequiresPackageAndTestPass(t *testing.T) {
	required := []DarwinPackageRequirement{{Package: "./internal/qualitygate", Tests: []string{"TestDarwinSysctlDecoding"}}}
	pkg := "github.com/example/glm-worker/internal/qualitygate"
	success := append(darwinAcceptanceEvent(t, "pass", pkg, "TestDarwinSysctlDecoding"), darwinAcceptanceEvent(t, "pass", pkg, "")...)
	if err := VerifyDarwinAcceptance(success, required); err != nil {
		t.Fatal(err)
	}
	for name, log := range map[string][]byte{
		"missing test":    darwinAcceptanceEvent(t, "pass", pkg, ""),
		"skipped test":    append(darwinAcceptanceEvent(t, "skip", pkg, "TestDarwinSysctlDecoding"), darwinAcceptanceEvent(t, "pass", pkg, "")...),
		"missing package": darwinAcceptanceEvent(t, "pass", pkg, "TestDarwinSysctlDecoding"),
		"compile failure": []byte("FAIL\n"),
		"wrong package":   append(darwinAcceptanceEvent(t, "pass", "github.com/example/internal/other", "TestDarwinSysctlDecoding"), darwinAcceptanceEvent(t, "pass", "github.com/example/internal/other", "")...),
	} {
		t.Run(name, func(t *testing.T) {
			if err := VerifyDarwinAcceptance(log, required); err == nil {
				t.Fatal("incomplete Darwin acceptance passed")
			}
		})
	}
	if err := VerifyDarwinAcceptance(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDarwinSupplementArgumentsIncludeOnlyRequiredPackages(t *testing.T) {
	original := DarwinSupplementArguments("go-test")
	if !reflect.DeepEqual(original, []string{"test", "-json", "-timeout=45m", "./internal/observationexec"}) {
		t.Fatalf("normal supplement changed: %#v", original)
	}
	selected := DarwinSupplementArguments("go-test", "./internal/qualitygate")
	if strings.Join(selected, " ") != "test -json -timeout=45m -count=1 ./internal/observationexec ./internal/qualitygate" {
		t.Fatalf("Darwin acceptance supplement: %#v", selected)
	}
}

func TestDockerSupplementRejectsDarwinAcceptanceEvidenceGaps(t *testing.T) {
	r := completeDockerExecution(t)
	r.DarwinPackages = []DarwinPackageRequirement{{Package: "./internal/qualitygate", Tests: []string{"TestDarwinSysctlDecoding"}}}
	args := DarwinSupplementArguments(r.Form, "./internal/qualitygate")
	r.Execution.NativeSupplement.Arguments = args
	r.Execution.NativeSupplement.Toolchain.Command = "go " + strings.Join(args, " ")
	if err := VerifyExecution(r); err == nil {
		t.Fatal("missing Darwin test and package were accepted")
	}
	file, err := os.OpenFile(r.Execution.NativeSupplement.Log, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	pkg := "github.com/example/glm-worker/internal/qualitygate"
	_, err = file.Write(append(darwinAcceptanceEvent(t, "pass", pkg, "TestDarwinSysctlDecoding"), darwinAcceptanceEvent(t, "pass", pkg, "")...))
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyExecution(r); err != nil {
		t.Fatalf("source-bound complete supplement refused: %v", err)
	}
	r.Execution.NativeSupplement.Arguments = DarwinSupplementArguments(r.Form)
	if err := VerifyExecution(r); err == nil {
		t.Fatal("wrong Darwin supplement command was accepted")
	}
}

func TestDarwinGoTestFunctionNames(t *testing.T) {
	for name, expected := range map[string]bool{
		"TestDarwinSysctlDecoding": true,
		"TestMain":                false,
		"Testhelper":              false,
		"Test":                    false,
		"Test_Example":            true,
	} {
		if got := goTestFunctionName(name); got != expected {
			t.Errorf("%s: expected %t, got %t", name, expected, got)
		}
	}
}
