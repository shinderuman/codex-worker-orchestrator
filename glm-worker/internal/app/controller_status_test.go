package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestControllerStatusProjectsRecoverableAuthority(t *testing.T) {
	cfg := newCanonicalGuardConfig(t, true)
	var output bytes.Buffer
	handled, err := runControllerStatus([]string{"--authority", "controller-status"}, func() (config.AppConfig, error) { return cfg, nil }, &output)
	if !handled || err != nil {
		t.Fatalf("controller-status handled=%v err=%v", handled, err)
	}
	var report controller.ControllerStatusReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("controller-status output is not machine JSON: %v: %s", err, output.String())
	}
	if !report.CanonicalActive || report.Head.LiveLeaseID == "" || report.Live == nil ||
		report.Live.LeaseID != report.Head.LiveLeaseID || report.Live.Purpose != "root-execution" {
		t.Fatalf("controller-status report = %#v", report)
	}

	workspace := report.Live.WorkspaceRoot
	if workspace == "" {
		t.Fatal("controller-status did not project the live workspace root")
	}
}

func TestControllerStatusRequiresExistingController(t *testing.T) {
	cfg := newCanonicalGuardConfig(t, false)
	if _, err := runControllerStatus([]string{"--authority", "controller-status"}, func() (config.AppConfig, error) { return cfg, nil }, nil); err == nil ||
		!bytes.Contains([]byte(err.Error()), []byte("requires existing controller authority")) {
		t.Fatalf("pristine controller status error = %v", err)
	}
}
