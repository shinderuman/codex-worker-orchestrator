package runner

import (
	"path"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	subcommandTest    = "test"
	subcommandBuild   = "build"
	subcommandFmt     = "fmt"
	subcommandInstall = "install"
)

var shellProgramCategories = map[string]string{
	"rg":            state.OperationCategorySearch,
	"grep":          state.OperationCategorySearch,
	"find":          state.OperationCategorySearch,
	"fd":            state.OperationCategorySearch,
	"ag":            state.OperationCategorySearch,
	"ack":           state.OperationCategorySearch,
	"cat":           state.OperationCategoryFileRead,
	"head":          state.OperationCategoryFileRead,
	"tail":          state.OperationCategoryFileRead,
	"less":          state.OperationCategoryFileRead,
	"bat":           state.OperationCategoryFileRead,
	"wc":            state.OperationCategoryFileRead,
	"stat":          state.OperationCategoryFileRead,
	"tee":           state.OperationCategoryFileWrite,
	"touch":         state.OperationCategoryFileWrite,
	"mkdir":         state.OperationCategoryFileWrite,
	"cp":            state.OperationCategoryFileWrite,
	"mv":            state.OperationCategoryFileWrite,
	"pytest":        state.OperationCategoryTest,
	"jest":          state.OperationCategoryTest,
	"vitest":        state.OperationCategoryTest,
	"mocha":         state.OperationCategoryTest,
	"phpunit":       state.OperationCategoryTest,
	"eslint":        state.OperationCategoryTest,
	"golangci-lint": state.OperationCategoryTest,
	"commentlint":   state.OperationCategoryTest,
	"harnesslint":   state.OperationCategoryTest,
	"shellcheck":    state.OperationCategoryTest,
	"gofmt":         state.OperationCategoryFormat,
	"prettier":      state.OperationCategoryFormat,
	"clang-format":  state.OperationCategoryFormat,
	"shfmt":         state.OperationCategoryFormat,
	"black":         state.OperationCategoryFormat,
	"isort":         state.OperationCategoryFormat,
	"cmake":         state.OperationCategoryBuild,
	"ninja":         state.OperationCategoryBuild,
	"meson":         state.OperationCategoryBuild,
	"tsc":           state.OperationCategoryBuild,
	"gcc":           state.OperationCategoryBuild,
	"clang":         state.OperationCategoryBuild,
	"brew":          state.OperationCategoryInstall,
	"composer":      state.OperationCategoryInstall,
}

var programCategoryHandlers = map[string]func(args []string) string{
	"git":     gitOperationCategory,
	"go":      goOperationCategory,
	"npm":     nodePackageOperationCategory,
	"pnpm":    nodePackageOperationCategory,
	"yarn":    nodePackageOperationCategory,
	"make":    makeOperationCategory,
	"cargo":   cargoOperationCategory,
	"python":  pythonOperationCategory,
	"python3": pythonOperationCategory,
	"pip":     pipOperationCategory,
	"pip3":    pipOperationCategory,
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
	for _, segment := range shellSegments(command) {
		if category := shellSegmentCategory(segment); category != "" {
			return category
		}
	}
	return state.OperationCategoryOther
}

func shellSegments(command string) []string {
	raw := strings.FieldsFunc(command, func(r rune) bool {
		switch r {
		case '&', '|', ';', '\n':
			return true
		}
		return false
	})
	segments := make([]string, 0, len(raw))
	for _, segment := range raw {
		if trimmed := strings.TrimSpace(segment); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return segments
}

func shellSegmentCategory(segment string) string {
	words := strings.Fields(segment)
	index := 0
	for index < len(words) && isEnvAssignment(words[index]) {
		index++
	}
	if index >= len(words) {
		return ""
	}
	return programOperationCategory(path.Base(words[index]), words[index+1:])
}

func isEnvAssignment(word string) bool {
	equals := strings.IndexByte(word, '=')
	if equals <= 0 {
		return false
	}
	for i := 0; i < equals; i++ {
		c := word[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			continue
		}
		if c >= '0' && c <= '9' && i > 0 {
			continue
		}
		return false
	}
	return true
}

func programOperationCategory(program string, args []string) string {
	if handler, ok := programCategoryHandlers[program]; ok {
		return handler(args)
	}
	return shellProgramCategories[program]
}

func gitOperationCategory(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "status", "log", "diff", "show", "branch", "blame", "rev-parse", "ls-files", "reflog", "describe", "shortlog", "remote":
		return state.OperationCategoryGitRead
	case "add", "commit", "push", "pull", "fetch", "checkout", "switch", "reset", "restore", "stash", "merge", "rebase", "cherry-pick", "clean", "apply", "am", "init", "clone", "worktree", "rm", "mv", "tag":
		return state.OperationCategoryGitWrite
	default:
		return ""
	}
}

func goOperationCategory(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case subcommandTest, "vet":
		return state.OperationCategoryTest
	case subcommandBuild, "generate":
		return state.OperationCategoryBuild
	case subcommandFmt:
		return state.OperationCategoryFormat
	case subcommandInstall:
		return state.OperationCategoryInstall
	default:
		return ""
	}
}

func nodePackageOperationCategory(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case subcommandInstall, "i", "ci", "add":
		return state.OperationCategoryInstall
	case subcommandTest, "t", "jest":
		return state.OperationCategoryTest
	case "run":
		return nodeScriptOperationCategory(args[1:])
	default:
		return ""
	}
}

func nodeScriptOperationCategory(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case subcommandBuild, "compile":
		return state.OperationCategoryBuild
	case subcommandTest, "jest", "lint":
		return state.OperationCategoryTest
	case "format", subcommandFmt:
		return state.OperationCategoryFormat
	default:
		return ""
	}
}

func makeOperationCategory(args []string) string {
	if len(args) == 0 {
		return state.OperationCategoryBuild
	}
	switch args[0] {
	case subcommandTest, "check", "lint":
		return state.OperationCategoryTest
	default:
		return state.OperationCategoryBuild
	}
}

func cargoOperationCategory(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case subcommandTest:
		return state.OperationCategoryTest
	case subcommandBuild:
		return state.OperationCategoryBuild
	case subcommandFmt:
		return state.OperationCategoryFormat
	case subcommandInstall:
		return state.OperationCategoryInstall
	default:
		return ""
	}
}

func pythonOperationCategory(args []string) string {
	if len(args) < 2 || args[0] != "-m" {
		return ""
	}
	switch args[1] {
	case "pytest":
		return state.OperationCategoryTest
	case "black", "isort":
		return state.OperationCategoryFormat
	default:
		return ""
	}
}

func pipOperationCategory(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if args[0] == subcommandInstall || args[0] == "download" {
		return state.OperationCategoryInstall
	}
	return ""
}
