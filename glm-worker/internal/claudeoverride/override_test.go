package claudeoverride

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePathPrecedence(t *testing.T) {
	tests := []struct {
		name string
		home string
		env  map[string]string
		want string
	}{
		{name: "home fallback", home: "/h", want: "/h/.config/codex-config/claude-settings.local.json"},
		{name: "xdg", home: "/h", env: map[string]string{"XDG_CONFIG_HOME": "/xdg"}, want: "/xdg/codex-config/claude-settings.local.json"},
		{name: "explicit", home: "/h", env: map[string]string{"CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE": "/custom/o.json"}, want: "/custom/o.json"},
		{name: "explicit wins xdg", home: "/h", env: map[string]string{"XDG_CONFIG_HOME": "/xdg", "CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE": "/c/o.json"}, want: "/c/o.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE", "")
			for key, value := range test.env {
				t.Setenv(key, value)
			}
			if got := ResolvePath(test.home); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestLoadOptionalFile(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), "absent.json")} {
		override, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(override.Sets) != 0 || len(override.Deletes) != 0 {
			t.Fatalf("optional missing override = %#v", override)
		}
	}
}

func TestLoadSetAndDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "override.json")
	if err := os.WriteFile(path, []byte(`{"env":{"SET_KEY":"value","EMPTY":"","DEL_KEY":null}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	override, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if override.Sets["SET_KEY"] != "value" {
		t.Fatalf("SET_KEY = %#v", override.Sets)
	}
	if value, ok := override.Sets["EMPTY"]; !ok || value != "" {
		t.Fatalf("EMPTY must remain a set value: %#v", override.Sets)
	}
	if !contains(override.Deletes, "DEL_KEY") {
		t.Fatalf("DEL_KEY missing from deletes: %#v", override.Deletes)
	}
}

func TestDecodeFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "broken json", content: `{broken`, want: "override JSON"},
		{name: "trailing", content: `{"env":{}} extra`, want: "override JSON"},
		{name: "unknown top-level", content: `{"other":1}`, want: "top-level key"},
		{name: "non-string value", content: `{"env":{"K":123}}`, want: "stringかnullのみ"},
		{name: "env array", content: `{"env":[1,2]}`, want: "override env"},
		{name: "top-level null", content: `null`, want: "top-level nullは許可されません"},
		{name: "env null", content: `{"env":null}`, want: "nullは許可されません"},
		{name: "top-level scalar", content: `"hello"`, want: "top-levelはobjectのみ"},
		{name: "top-level array", content: `[]`, want: "top-levelはobjectのみ"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
