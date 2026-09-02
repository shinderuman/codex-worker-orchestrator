package app

import (
	"io"
	"sort"
)

type helpOutput struct {
	Usage    string            `json:"usage"`
	Commands []string          `json:"commands"`
	Aliases  map[string]string `json:"aliases"`
}

func runHelp(args []string, stdout io.Writer) (bool, error) {
	if len(args) == 0 || (args[0] != "--help" && args[0] != "-h") {
		return false, nil
	}
	if len(args) != 1 {
		return true, usageError("usage: glm-worker --help")
	}
	commands := make([]string, 0, len(commandParsers)+2)
	for name := range commandParsers {
		if name == "--decision" || name == "--fix" {
			continue
		}
		commands = append(commands, name)
	}
	commands = append(commands, "--authority", "--help")
	sort.Strings(commands)
	return true, writeJSON(stdout, helpOutput{
		Usage:    "glm-worker <instruction> | <command>",
		Commands: commands,
		Aliases:  map[string]string{"-h": "--help"},
	})
}
