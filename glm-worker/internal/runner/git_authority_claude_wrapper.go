package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func (g *gitAuthorityGuard) prepareClaudeWrapper(claudeBin string) (string, error) {
	if g == nil || !g.before.active {
		return claudeBin, nil
	}
	path := filepath.Join(g.tempDir, "claude-with-git-authority-guard")
	content := gitAuthorityClaudeWrapperScript(g, claudeBin)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		return "", &GitAuthorityGuardError{Stage: "prepare-claude-wrapper", Cause: err}
	}
	return path, nil
}

func gitAuthorityClaudeWrapperScript(g *gitAuthorityGuard, claudeBin string) string {
	configValues := []string{"", "https://", "http://", "ssh://", "git://", "git@"}
	result := "#!/bin/sh\n"
	result += "PATH=" + shellSingleQuote(g.tempDir) + ":\"$PATH\"\nexport PATH\n"
	result += "GLM_WORKER_GIT_TEMP_ROOT=" + shellSingleQuote(g.workerTempDir) + "\nexport GLM_WORKER_GIT_TEMP_ROOT\n"
	result += "CLAUDE_CODE_TMPDIR=" + shellSingleQuote(g.workerTempDir) + "\nexport CLAUDE_CODE_TMPDIR\n"
	result += "TMPDIR=" + shellSingleQuote(g.workerTempDir) + "\nexport TMPDIR\n"
	result += "TMP=" + shellSingleQuote(g.workerTempDir) + "\nexport TMP\n"
	result += "TEMP=" + shellSingleQuote(g.workerTempDir) + "\nexport TEMP\n"
	result += "GIT_TERMINAL_PROMPT=0\nexport GIT_TERMINAL_PROMPT\n"
	result += "GIT_ASKPASS=" + shellSingleQuote(g.denyPath) + "\nexport GIT_ASKPASS\n"
	result += "SSH_ASKPASS=" + shellSingleQuote(g.denyPath) + "\nexport SSH_ASKPASS\n"
	result += "GIT_SSH_COMMAND=" + shellSingleQuote(g.denyPath) + "\nexport GIT_SSH_COMMAND\n"
	result += "GIT_CONFIG_GLOBAL=" + shellSingleQuote(os.DevNull) + "\nexport GIT_CONFIG_GLOBAL\n"
	result += "GIT_CONFIG_SYSTEM=" + shellSingleQuote(os.DevNull) + "\nexport GIT_CONFIG_SYSTEM\n"
	result += "GIT_OPTIONAL_LOCKS=0\nexport GIT_OPTIONAL_LOCKS\n"
	result += "GIT_ALLOW_PROTOCOL=none\nexport GIT_ALLOW_PROTOCOL\n"
	result += "GIT_CONFIG_COUNT=" + strconv.Itoa(len(configValues)) + "\nexport GIT_CONFIG_COUNT\n"
	for i, value := range configValues {
		key := "url.blocked://glm-worker/.pushInsteadOf"
		if i == 0 {
			key = "credential.helper"
		}
		result += fmt.Sprintf("GIT_CONFIG_KEY_%d=%s\nexport GIT_CONFIG_KEY_%d\n", i, shellSingleQuote(key), i)
		result += fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s\nexport GIT_CONFIG_VALUE_%d\n", i, shellSingleQuote(value), i)
	}
	result += "exec " + shellSingleQuote(claudeBin) + " \"$@\"\n"
	return result
}
