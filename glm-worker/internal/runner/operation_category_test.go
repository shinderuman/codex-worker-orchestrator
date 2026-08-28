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
		{tool: "WebSearch", want: state.OperationCategoryOther},
	}
	for _, c := range cases {
		if got := operationCategoryForTool(c.tool, c.command); got != c.want {
			t.Fatalf("operationCategoryForTool(%q, %q) = %q, want %q", c.tool, c.command, got, c.want)
		}
	}
}

func TestShellOperationCategoryKeepsObservedFormsNarrow(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{command: "cd /repo && go test ./...", want: state.OperationCategoryTest},
		{command: "git branch --contains HEAD", want: state.OperationCategoryGitRead},
		{command: "git branch --show-current", want: state.OperationCategoryGitRead},
		{command: "./harnesslint", want: state.OperationCategoryTest},
		{command: "GOFLAGS=-mod=readonly go vet ./...", want: state.OperationCategoryOther},
		{command: "/usr/local/bin/go test ./...", want: state.OperationCategoryOther},
		{command: "git diff HEAD && go build ./...", want: state.OperationCategoryOther},
		{command: "echo start; go build ./...; echo done", want: state.OperationCategoryOther},
		{command: "grep pattern | wc -l", want: state.OperationCategoryOther},
		{command: "npm run build", want: state.OperationCategoryOther},
		{command: "python -m pytest tests", want: state.OperationCategoryOther},
		{command: "git branch feature", want: state.OperationCategoryOther},
		{command: "git branch --merged", want: state.OperationCategoryOther},
		{command: "git tag v1", want: state.OperationCategoryOther},
		{command: "go mod tidy", want: state.OperationCategoryOther},
		{command: "cd /repo && go test ./... && go vet ./...", want: state.OperationCategoryOther},
		{command: "   ", want: state.OperationCategoryOther},
	}
	for _, c := range cases {
		if got := shellOperationCategory(c.command); got != c.want {
			t.Fatalf("shellOperationCategory(%q) = %q, want %q", c.command, got, c.want)
		}
	}
}
