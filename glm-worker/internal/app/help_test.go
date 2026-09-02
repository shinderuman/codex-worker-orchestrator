package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
			foundAuthority := false
			for _, command := range output.Commands {
				foundHelp = foundHelp || command == "--help"
				foundStatus = foundStatus || command == "--status"
				foundAuthority = foundAuthority || command == "--authority"
			}
			if !foundHelp || !foundStatus || !foundAuthority {
				t.Fatalf("help commands = %#v", output.Commands)
			}
		})
	}
}

func TestRunEntryAuthorityBypassesConfig(t *testing.T) {
	root := t.TempDir()
	writeAppTestFile(t, root, "IMPLEMENTATION_RULES.md", "rules-body\n")
	writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/current.md`\n")
	writeAppTestFile(t, root, "IMPLEMENTATION_TASKS/current.md", "task-body\n")

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	var stdout bytes.Buffer
	err = runEntry(
		[]string{"--authority", "active"},
		func() (config.AppConfig, error) {
			t.Fatal("authority bootstrap loaded repository config")
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
	output := stdout.String()
	if !strings.Contains(output, "authority_kind=active\n") || !strings.Contains(output, "active_task=IMPLEMENTATION_TASKS/current.md\n") || !strings.HasSuffix(output, "task-body\n") {
		t.Fatalf("authority output = %q", output)
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

func writeAppTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
