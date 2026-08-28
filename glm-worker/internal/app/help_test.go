package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestRunEntryHelpBypassesConfig(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		t.Run(args[0], func(t *testing.T) {
			var stdout bytes.Buffer
			err := runEntry(
				args,
				func() (config.AppConfig, error) {
					t.Fatal("help loaded repository config")
					return config.AppConfig{}, nil
				},
				nil,
				bytes.NewReader(nil),
				&stdout,
				io.Discard,
			)
			if err != nil {
				t.Fatal(err)
			}
			var output helpOutput
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			if output.Usage != "glm-worker <instruction> | <command>" || output.Aliases["-h"] != "--help" {
				t.Fatalf("help output = %#v", output)
			}
			foundHelp := false
			foundStatus := false
			for _, command := range output.Commands {
				foundHelp = foundHelp || command == "--help"
				foundStatus = foundStatus || command == "--status"
			}
			if !foundHelp || !foundStatus {
				t.Fatalf("help commands = %#v", output.Commands)
			}
		})
	}
}

func TestRunEntryHelpRejectsExtraArgumentsBeforeConfig(t *testing.T) {
	var stdout bytes.Buffer
	err := runEntry(
		[]string{"--help", "extra"},
		func() (config.AppConfig, error) {
			t.Fatal("invalid help loaded repository config")
			return config.AppConfig{}, nil
		},
		nil,
		bytes.NewReader(nil),
		&stdout,
		io.Discard,
	)
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
