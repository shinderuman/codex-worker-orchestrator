package controller

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

func TestRequestBackedDarwinAcceptanceUsesCanonicalRequestBody(t *testing.T) {
	repo := newControllerRepo(t)
	cfg := controllerTestConfig(repo, filepath.Join(t.TempDir(), "sessions"))
	const request = "# Request\n\n## Darwin acceptance\n\n- `./internal/qualitygate:TestDarwinSysctlDecoding`\n"
	admission, err := ActivateRequest(cfg, request)
	if err != nil {
		t.Fatal(err)
	}
	if admission.Attempt.SemanticTaskRef.RequestID == "" || admission.Attempt.SemanticTaskRef.TaskPath != "" {
		t.Fatalf("request-backed admission did not select request authority: %#v", admission.Attempt.SemanticTaskRef)
	}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tests, err := store.taskDarwinAcceptance(admission)
	if err != nil {
		t.Fatal(err)
	}
	want := []qualitygate.DarwinTest{{Package: "./internal/qualitygate", Name: "TestDarwinSysctlDecoding"}}
	if !reflect.DeepEqual(tests, want) {
		t.Fatalf("request-backed Darwin acceptance = %#v, want %#v", tests, want)
	}
	forms := []FinalValidationRequest{{Form: "go-test"}, {Form: "go-test-race"}}
	if err := store.applyTaskDarwinAcceptance(admission, forms); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(forms[0].DarwinTests, want) || len(forms[1].DarwinTests) != 0 {
		t.Fatalf("request-backed acceptance not bound to go-test: %#v", forms)
	}
	if err := store.applyTaskDarwinAcceptance(admission, []FinalValidationRequest{{Form: "go-test-race"}}); err == nil || !strings.Contains(err.Error(), "requires go-test") {
		t.Fatalf("request-backed declaration accepted without go-test: %v", err)
	}
	mismatch := admission
	mismatch.Attempt.SemanticTaskRef.ContractDigest = "changed-contract"
	if _, err := store.taskDarwinAcceptance(mismatch); err == nil {
		t.Fatal("request-backed Darwin declaration accepted a foreign contract digest")
	}
}

func TestRequestBackedDarwinAcceptanceRejectsMalformedDeclaration(t *testing.T) {
	repo := newControllerRepo(t)
	cfg := controllerTestConfig(repo, filepath.Join(t.TempDir(), "sessions"))
	admission, err := ActivateRequest(cfg, "# Request\n\n## Darwin acceptance\n\n- invalid declaration\n")
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.taskDarwinAcceptance(admission); err == nil || !strings.Contains(err.Error(), "requires a list") {
		t.Fatalf("malformed request-backed Darwin declaration was accepted: %v", err)
	}
}
