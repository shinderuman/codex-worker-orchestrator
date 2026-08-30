package parentactioncmd

import (
	"io"
	"strconv"
	"strings"
	"testing"
)

func TestPayloadWorkerArgsStartKeepsSemanticPayloadOneArgument(t *testing.T) {
	payload := []byte("Task: current ACTIVE\n`$'\"\x00tail")
	args := payloadWorkerArgs("start", payload, nil)
	if len(args) != 1 || args[0] != string(payload) {
		t.Fatalf("start args = %#v", args)
	}
	if payloadStdin("start", payload) != nil {
		t.Fatal("start unexpectedly uses stdin transport")
	}
}

func TestPayloadWorkerArgsDecisionOwnsFraming(t *testing.T) {
	payload := []byte("判断\n`$'\"")
	args := payloadWorkerArgs("decision", payload, nil)
	if len(args) != 4 || args[0] != "--decision-stdin" || args[1] != strconv.Itoa(len(payload)) || args[2] != "--sha256" || len(args[3]) != 64 {
		t.Fatalf("decision args = %#v", args)
	}
	got, err := io.ReadAll(payloadStdin("decision", payload))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("stdin payload = %q", got)
	}
}

func TestValidateFixOptionsMatchesProductionDomain(t *testing.T) {
	valid := []string{"--origin", "glm-reviewer", "--accepted-scope", "current-diff"}
	if err := validateFixOptions(valid); err != nil {
		t.Fatal(err)
	}
	for _, options := range [][]string{
		{"--origin", "invented"},
		{"--accepted-scope", "anything"},
		{"--origin", "codex-review", "--origin", "glm-reviewer"},
		{"--other", "value"},
		{"--origin"},
	} {
		if err := validateFixOptions(options); err == nil {
			t.Fatalf("invalid options accepted: %s", strings.Join(options, " "))
		}
	}
}
