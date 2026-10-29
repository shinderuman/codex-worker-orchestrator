package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func prepareWorkflowQualityProcesses(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "quality-tools.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contract := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		if key, value, ok := strings.Cut(line, ": "); ok {
			contract[key] = value
		}
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	fail := filepath.Join(bin, "fail")
	for _, tool := range []string{"golangci-lint", "deadcode", "shellcheck", "shfmt"} {
		path := filepath.Join(bin, contract["namespace"]+"-"+tool+"-"+contract[tool])
		writeWorkflowQualityTool(t, path, tool, contract[tool], log, fail)
	}
	writeWorkflowQualityGo(t, bin, contract)
	writeAppTestFile(t, root, "commentlint", "#!/bin/sh\nprintf '%s\\n' '{\"status\":\"pass\",\"violations\":[]}'\n")
	if err := os.Chmod(filepath.Join(root, "commentlint"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUALITY_TOOLS_BIN_DIR", bin)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeWorkflowQualityTool(t *testing.T, path, tool, version, log, fail string) {
	t.Helper()
	body := "#!/bin/sh\ncase \"$1\" in version|--version) printf '%s\\n' " + quoteWorkflowShell(version) + "; exit 0;; esac\n" +
		"printf '%s\\n' " + quoteWorkflowShell(tool+" $*") + " >>" + quoteWorkflowShell(log) + "\n" +
		"if [ -f " + quoteWorkflowShell(fail) + " ]; then\n" +
		" printf '%s\\n' 'cmd/fixture/main.go:1:1: fixture lint rejection (revive)'\n exit 1\nfi\n"
	if err := execfile.Write(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeWorkflowQualityGo(t *testing.T, bin string, contract map[string]string) {
	t.Helper()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	deadcode := filepath.Join(bin, contract["namespace"]+"-deadcode-"+contract["deadcode"])
	metadata := fmt.Sprintf("\tmod\tgolang.org/x/tools\tv%s\n", contract["deadcode"])
	body := "#!/bin/sh\nif [ \"$1\" = version ] && [ \"${2:-}\" = -m ] && [ \"${3:-}\" = " + quoteWorkflowShell(deadcode) + " ]; then\n" +
		" printf '%s' " + quoteWorkflowShell(metadata) + "; exit 0\nfi\nexec " + quoteWorkflowShell(realGo) + " \"$@\"\n"
	if err := execfile.Write(filepath.Join(bin, "go"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func quoteWorkflowShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
