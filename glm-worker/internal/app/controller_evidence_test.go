package app

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestDecodeControllerEvidenceCommand(t *testing.T) {
	command, err := decodeControllerEvidenceCommand(strings.NewReader(`{"action":"capture-git-archive","logical_identity":"attempt:a1","root_oids":["abc"]}`))
	if err != nil {
		t.Fatalf("decode controller evidence command: %v", err)
	}
	if command.Action != controllerEvidenceCaptureArchive {
		t.Fatalf("action = %q want %q", command.Action, controllerEvidenceCaptureArchive)
	}
	if command.LogicalIdentity != "attempt:a1" || len(command.RootOIDs) != 1 || command.RootOIDs[0] != "abc" {
		t.Fatalf("decoded command = %#v", command)
	}
}

func TestDecodeControllerEvidenceCommandRejectsUnknownAndTrailingInput(t *testing.T) {
	for name, input := range map[string]string{
		"unknown":  `{"action":"build-attempt-bundle","unexpected":true}`,
		"trailing": `{"action":"build-attempt-bundle"}{"action":"build-task-bundle"}`,
		"missing":  `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeControllerEvidenceCommand(strings.NewReader(input)); err == nil {
				t.Fatalf("input %q was accepted", input)
			}
		})
	}
}

func TestRequiredControllerEvidenceRef(t *testing.T) {
	if _, err := requiredControllerEvidenceRef(controllerEvidenceCommand{Action: controllerEvidenceCleanup}); err == nil {
		t.Fatal("missing evidence ref was accepted")
	}
	want := controller.EvidenceObjectRef{Digest: "sha256:test", Kind: "attempt-seal", Length: 1}
	got, err := requiredControllerEvidenceRef(controllerEvidenceCommand{Action: controllerEvidenceCleanup, Ref: &want})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ref = %#v want %#v", got, want)
	}
}
