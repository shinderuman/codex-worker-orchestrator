package runner

import (
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

var shellProgramCategories = map[string]string{
	"rg":            state.OperationCategorySearch,
	"grep":          state.OperationCategorySearch,
	"cat":           state.OperationCategoryFileRead,
	"head":          state.OperationCategoryFileRead,
	"tail":          state.OperationCategoryFileRead,
	"tee":           state.OperationCategoryFileWrite,
	"gofmt":         state.OperationCategoryFormat,
	"./harnesslint": state.OperationCategoryTest,
	"./commentlint": state.OperationCategoryTest,
}

func operationCategoryForTool(toolName string, command string) string {
	switch toolName {
	case "Read":
		return state.OperationCategoryFileRead
	case "Edit", "Write", "NotebookEdit":
		return state.OperationCategoryFileWrite
	case "Grep", "Glob":
		return state.OperationCategorySearch
	case "Bash":
		return shellOperationCategory(command)
	default:
		return state.OperationCategoryOther
	}
}

func shellOperationCategory(command string) string {
	words, ok := shellCommandWords(command)
	if !ok {
		return state.OperationCategoryOther
	}
	if category := shellWordsOperationCategory(words); category != "" {
		return category
	}
	return state.OperationCategoryOther
}

func shellCommandWords(command string) ([]string, bool) {
	if strings.ContainsAny(command, "|;\n") {
		return nil, false
	}
	if strings.Contains(command, "&") {
		parts := strings.Split(command, "&&")
		if len(parts) != 2 || strings.Contains(parts[0], "&") || strings.Contains(parts[1], "&") {
			return nil, false
		}
		prefix := strings.Fields(parts[0])
		if len(prefix) != 2 || prefix[0] != "cd" {
			return nil, false
		}
		words := strings.Fields(parts[1])
		return words, len(words) > 0
	}
	words := strings.Fields(command)
	return words, len(words) > 0
}

func shellWordsOperationCategory(words []string) string {
	switch words[0] {
	case "go":
		return goOperationCategory(words[1:])
	case "git":
		return gitOperationCategory(words[1:])
	default:
		return shellProgramCategories[words[0]]
	}
}

func gitOperationCategory(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "status", "log", "diff", "show", "blame", "rev-parse", "ls-files", "reflog", "describe", "shortlog":
		return state.OperationCategoryGitRead
	case "branch":
		return gitBranchOperationCategory(args[1:])
	case "add", "commit", "push", "pull", "fetch", "checkout", "switch", "reset", "restore", "stash", "merge", "rebase", "cherry-pick", "clean", "apply", "am", "init", "clone", "worktree", "rm", "mv":
		return state.OperationCategoryGitWrite
	default:
		return ""
	}
}

func gitBranchOperationCategory(args []string) string {
	if len(args) == 0 {
		return state.OperationCategoryGitRead
	}
	if args[0] == "--show-current" || args[0] == "--contains" || strings.HasPrefix(args[0], "--contains=") {
		return state.OperationCategoryGitRead
	}
	return ""
}

func goOperationCategory(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "test", "vet":
		return state.OperationCategoryTest
	case "build":
		return state.OperationCategoryBuild
	case "fmt":
		return state.OperationCategoryFormat
	case "install":
		return state.OperationCategoryInstall
	default:
		return ""
	}
}
