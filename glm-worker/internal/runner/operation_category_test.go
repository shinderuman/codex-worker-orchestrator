package runner

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestOperationCategoryForToolCoversAllowlist(t *testing.T) {
	cases := []struct {
		tool    string
		command string
		want    string
	}{
		{tool: "Bash", command: "rg pattern", want: state.OperationCategorySearch},
		{tool: "Bash", command: "go test ./...", want: state.OperationCategoryTest},
		{tool: "Bash", command: "go build ./...", want: state.OperationCategoryBuild},
		{tool: "Bash", command: "gofmt -l .", want: state.OperationCategoryFormat},
		{tool: "Bash", command: "go install std", want: state.OperationCategoryInstall},
		{tool: "Bash", command: "git status --short", want: state.OperationCategoryGitRead},
		{tool: "Bash", command: "git commit -m message", want: state.OperationCategoryGitWrite},
		{tool: "Bash", command: "cat README.md", want: state.OperationCategoryFileRead},
		{tool: "Bash", command: "tee output.txt", want: state.OperationCategoryFileWrite},
		{tool: "Read", want: state.OperationCategoryFileRead},
		{tool: "Edit", want: state.OperationCategoryFileWrite},
		{tool: "Write", want: state.OperationCategoryFileWrite},
		{tool: "Grep", want: state.OperationCategorySearch},
		{tool: "Glob", want: state.OperationCategorySearch},
		{tool: "Bash", command: "echo done", want: state.OperationCategoryOther},
		{tool: "Bash", command: "", want: state.OperationCategoryOther},
		{tool: "WebSearch", want: state.OperationCategoryOther},
		{tool: "TaskOutput", want: state.OperationCategoryOther},
	}
	for _, c := range cases {
		if got := operationCategoryForTool(c.tool, c.command); got != c.want {
			t.Fatalf("operationCategoryForTool(%q, %q) = %q, want %q", c.tool, c.command, got, c.want)
		}
	}
}

func TestShellOperationCategoryResolvesAmbiguousCommands(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{command: "cd /repo && go test ./...", want: state.OperationCategoryTest},
		{command: "GOFLAGS=-mod=readonly go vet ./...", want: state.OperationCategoryTest},
		{command: "/usr/local/bin/go test ./...", want: state.OperationCategoryTest},
		{command: "git diff HEAD && go build ./...", want: state.OperationCategoryGitRead},
		{command: "echo start; go build ./...; echo done", want: state.OperationCategoryBuild},
		{command: "grep pattern | wc -l", want: state.OperationCategorySearch},
		{command: "git notes list", want: state.OperationCategoryOther},
		{command: "go mod tidy", want: state.OperationCategoryOther},
		{command: "make test", want: state.OperationCategoryTest},
		{command: "make", want: state.OperationCategoryBuild},
		{command: "npm install", want: state.OperationCategoryInstall},
		{command: "npm run build", want: state.OperationCategoryBuild},
		{command: "python -m pytest tests", want: state.OperationCategoryTest},
		{command: "pip install requests", want: state.OperationCategoryInstall},
		{command: "echo sk-ant-secret-token", want: state.OperationCategoryOther},
		{command: "sleep 503", want: state.OperationCategoryOther},
		{command: "   ", want: state.OperationCategoryOther},
	}
	for _, c := range cases {
		if got := shellOperationCategory(c.command); got != c.want {
			t.Fatalf("shellOperationCategory(%q) = %q, want %q", c.command, got, c.want)
		}
	}
}
