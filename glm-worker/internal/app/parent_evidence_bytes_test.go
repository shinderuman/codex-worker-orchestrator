package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParentEvidenceBytesMeasureProjectedBodies(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	fixture := newParentEvidenceFixture(t)
	activePath := filepath.Join(fixture.repoRoot, "IMPLEMENTATION_TASKS", "current.md")
	if err := os.WriteFile(activePath, []byte(strings.Repeat("evidence-body-", 256)), 0o600); err != nil {
		t.Fatal(err)
	}

	authority := runParentEvidence(t, fixture, parentEvidenceManifest{
		Version: 1,
		Reason:  "measure authority body",
		Authority: []parentEvidenceAuthorityRequest{
			{Kind: "active", BudgetBytes: 16 * 1024},
		},
	})
	authorityPart := authority.Output.Parts[0]
	if authorityPart.Authority == nil || authorityPart.Authority.Content == "" {
		t.Fatalf("authority body missing: %#v", authorityPart)
	}
	if authorityPart.Bytes != len(authorityPart.Authority.Content) {
		t.Fatalf("authority bytes = %d, want projected body bytes %d", authorityPart.Bytes, len(authorityPart.Authority.Content))
	}

	telemetry := runParentEvidence(t, fixture, parentEvidenceManifest{
		Version:   1,
		Reason:    "measure telemetry body",
		Telemetry: &parentEvidenceTelemetryRequest{},
	})
	telemetryPart := telemetry.Output.Parts[0]
	if telemetryPart.Telemetry == nil {
		t.Fatalf("telemetry body missing: %#v", telemetryPart)
	}
	body, err := json.Marshal(telemetryPart.Telemetry)
	if err != nil {
		t.Fatal(err)
	}
	if telemetryPart.Bytes != len(body) {
		t.Fatalf("telemetry bytes = %d, want projected body bytes %d", telemetryPart.Bytes, len(body))
	}
}
