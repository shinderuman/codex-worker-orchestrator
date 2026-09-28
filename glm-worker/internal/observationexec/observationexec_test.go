package observationexec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRequestAcceptsClosedOperations(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    Operation
	}{
		{"shadow-eval", "OPERATION: shadow-eval\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default", OperationShadowEval},
		{"shadow-eval with reference", "OPERATION: shadow-eval\nREFERENCE: labels/ref.json\nWORKING_DIR: -\nDEADLINE_MS: default", OperationShadowEval},
		{"go-test default dir", "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default", OperationGoTest},
		{"go-test-race module dir", "OPERATION: go-test-race\nREFERENCE: -\nWORKING_DIR: glm-worker\nDEADLINE_MS: 900000", OperationGoTestRace},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, err := ParseRequest([]byte(tc.payload))
			if err != nil {
				t.Fatalf("受理すべきpayloadが拒否されました: %v", err)
			}
			if request.Operation != tc.want {
				t.Fatalf("operation = %q want %q", request.Operation, tc.want)
			}
		})
	}
}

func TestParseRequestRejectsInvalidPayloads(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		fragment string
	}{
		{"unknown operation", "OPERATION: rm-rf\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default", "閉集合"},
		{"free shell payload", "OPERATION: go-test ./...; rm -rf /\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default", "閉集合"},
		{"extra slot", "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -\nEXTRA: x\nDEADLINE_MS: default", "4行形式"},
		{"missing slot", "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -", "DEADLINE_MS"},
		{"reference on go-test", "OPERATION: go-test\nREFERENCE: ref.json\nWORKING_DIR: -\nDEADLINE_MS: default", "reference slot"},
		{"working-dir on shadow-eval", "OPERATION: shadow-eval\nREFERENCE: -\nWORKING_DIR: glm-worker\nDEADLINE_MS: default", "working-dir slot"},
		{"deadline below floor", "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: 1000", "範囲"},
		{"deadline above ceiling", "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: 99999999", "範囲"},
		{"deadline not integer", "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: soon", "整数"},
		{"reference parent traversal", "OPERATION: shadow-eval\nREFERENCE: ../outside.json\nWORKING_DIR: -\nDEADLINE_MS: default", "親遷移"},
		{"working-dir absolute", "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: /etc\nDEADLINE_MS: default", "相対path"},
		{"not key value", "shadow-eval\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default", "形式"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequest([]byte(tc.payload))
			if err == nil {
				t.Fatal("拒否すべきpayloadが受理されました")
			}
			if !strings.Contains(err.Error(), tc.fragment) {
				t.Fatalf("error %qに理由 %qがありません", err.Error(), tc.fragment)
			}
		})
	}
}

func TestRequestDigestNormalizesDefaultDeadline(t *testing.T) {
	base := "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default"
	explicit := "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: 600000"
	baseRequest, err := ParseRequest([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	explicitRequest, err := ParseRequest([]byte(explicit))
	if err != nil {
		t.Fatal(err)
	}
	if baseRequest.Digest() != explicitRequest.Digest() {
		t.Fatal("同一意味parameterは同一digestである必要があります")
	}
	different, err := ParseRequest([]byte("OPERATION: go-test-race\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default"))
	if err != nil {
		t.Fatal(err)
	}
	if baseRequest.Digest() == different.Digest() {
		t.Fatal("異なるoperationが同一digestになりました")
	}
}

func TestValidateReferenceLocatorBoundedToArtifactRoot(t *testing.T) {
	artifactRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(artifactRoot, "labels"), 0o700); err != nil {
		t.Fatal(err)
	}
	reference := filepath.Join(artifactRoot, "labels", "reference.json")
	if err := os.WriteFile(reference, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := ValidateReferenceLocator(artifactRoot, "labels/reference.json")
	if err != nil {
		t.Fatalf("境界内locatorが拒否されました: %v", err)
	}
	expected, err := filepath.EvalSymlinks(reference)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != expected {
		t.Fatalf("resolved = %q want %q", resolved, expected)
	}

	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(artifactRoot, "labels", "escape.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateReferenceLocator(artifactRoot, "labels/escape.json"); err == nil {
		t.Fatal("symlinkで基準rootの外へ出るlocatorは拒否されるべきです")
	}
	if _, err := ValidateReferenceLocator(artifactRoot, "labels/missing.json"); err == nil {
		t.Fatal("存在しないlocatorは拒否されるべきです")
	}
	if _, err := ValidateReferenceLocator(artifactRoot, "labels"); err == nil {
		t.Fatal("directoryは拒否されるべきです")
	}
}

func TestValidateGoTestWorkingDirBoundedToRepository(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/root\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := ValidateGoTestWorkingDir(repoRoot, "-")
	if err != nil {
		t.Fatalf("root module省略時の既定dirが拒否されました: %v", err)
	}
	expectedRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != expectedRoot {
		t.Fatalf("resolved = %q want %q", resolved, expectedRoot)
	}

	nested := filepath.Join(repoRoot, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.com/nested\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateGoTestWorkingDir(repoRoot, "nested"); err != nil {
		t.Fatalf("境界内module dirが拒否されました: %v", err)
	}

	noModule := filepath.Join(repoRoot, "nomodule")
	if err := os.MkdirAll(noModule, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateGoTestWorkingDir(repoRoot, "nomodule"); err == nil {
		t.Fatal("go.modのないdirは拒否されるべきです")
	}
	if _, err := ValidateGoTestWorkingDir(repoRoot, "../../../etc"); err == nil {
		t.Fatal("親遷移locatorは拒否されるべきです")
	}

	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "go.mod"), []byte("module example.com/outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(repoRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateGoTestWorkingDir(repoRoot, "linked"); err == nil {
		t.Fatal("symlinkでrepositoryの外へ出るworking-dirは拒否されるべきです")
	}
}
