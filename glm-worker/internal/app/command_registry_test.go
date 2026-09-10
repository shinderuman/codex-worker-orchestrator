package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNoArgUsageDelegatesCommandInventoryToHelp(t *testing.T) {
	_, err := ParseCommand(nil)
	if err == nil {
		t.Fatal("ParseCommand(nil) succeeded")
	}
	msg := err.Error()
	if !strings.Contains(msg, "glm-worker --help") {
		t.Fatalf("no-arg usage must direct callers to the registry-backed help: %q", msg)
	}
	if strings.Contains(msg, "--verify-codex-wake") {
		t.Fatalf("no-arg usage duplicated command inventory instead of delegating to help: %q", msg)
	}
}

func TestHelpCommandInventoryCoversParserRegistry(t *testing.T) {
	var out bytes.Buffer
	handled, err := runHelp([]string{"--help"}, &out)
	if err != nil || !handled {
		t.Fatalf("runHelp = handled %v err %v", handled, err)
	}
	var got helpOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	visible := make(map[string]bool, len(got.Commands))
	for _, command := range got.Commands {
		visible[command] = true
	}
	for command := range commandParsers {
		if command == "--decision" || command == "--fix" {
			continue
		}
		if !visible[command] {
			t.Errorf("parser registry command %q is missing from --help", command)
		}
	}
}
