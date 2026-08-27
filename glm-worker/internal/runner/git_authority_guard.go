package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type GitAuthorityGuardError struct {
	Stage     string
	Mutations []string
	Cause     error
}

type gitAuthoritySnapshot struct {
	active       bool
	head         string
	symbolicHead string
	refsDigest   string
	indexDigest  string
	configDigest string
}

type gitAuthorityGuard struct {
	repoRoot      string
	realGit       string
	tempDir       string
	proxyPath     string
	denyPath      string
	attemptLog    string
	before        gitAuthoritySnapshot
}

func (e *GitAuthorityGuardError) Error() string {
	parts := []string{"git authority guard failed", e.Stage}
	if len(e.Mutations) > 0 {
		parts = append(parts, strings.Join(e.Mutations, ","))
	}
	if e.Cause != nil {
		parts = append(parts, e.Cause.Error())
	}
	return strings.Join(parts, ": ")
}

func (e *GitAuthorityGuardError) Unwrap() error {
	return e.Cause
}

func prepareGitAuthorityGuard(repoRoot string) (*gitAuthorityGuard, error) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		return nil, &GitAuthorityGuardError{Stage: "resolve-git", Cause: err}
	}
	realGit, err = filepath.Abs(realGit)
	if err != nil {
		return nil, &GitAuthorityGuardError{Stage: "resolve-git", Cause: err}
	}
	before, err := captureGitAuthoritySnapshot(realGit, repoRoot)
	if err != nil {
		return nil, &GitAuthorityGuardError{Stage: "capture-before-call", Cause: err}
	}
	guard := &gitAuthorityGuard{repoRoot: repoRoot, realGit: realGit, before: before}
	if !before.active {
		return guard, nil
	}
	if err := guard.prepareProxy(); err != nil {
		guard.cleanup()
		return nil, err
	}
	return guard, nil
}

func (g *gitAuthorityGuard) prepareProxy() error {
	tempDir, err := os.MkdirTemp("", "glm-worker-git-guard-")
	if err != nil {
		return &GitAuthorityGuardError{Stage: "prepare-command-proxy", Cause: err}
	}
	g.tempDir = tempDir
	g.proxyPath = filepath.Join(tempDir, "git")
	g.denyPath = filepath.Join(tempDir, "deny-git-transport")
	g.attemptLog = filepath.Join(tempDir, "blocked-attempts")
	if err := os.WriteFile(g.attemptLog, nil, 0o600); err != nil {
		return &GitAuthorityGuardError{Stage: "prepare-command-proxy", Cause: err}
	}
	proxy := gitAuthorityProxyScript(g.realGit, g.attemptLog)
	if err := os.WriteFile(g.proxyPath, []byte(proxy), 0o700); err != nil {
		return &GitAuthorityGuardError{Stage: "prepare-command-proxy", Cause: err}
	}
	deny := gitAuthorityDenyTransportScript(g.attemptLog)
	if err := os.WriteFile(g.denyPath, []byte(deny), 0o700); err != nil {
		return &GitAuthorityGuardError{Stage: "prepare-command-proxy", Cause: err}
	}
	return nil
}

func (g *gitAuthorityGuard) applyChildEnv(env map[string]string) {
	if g == nil || !g.before.active {
		return
	}
	currentPath := os.Getenv("PATH")
	if currentPath == "" {
		env["PATH"] = g.tempDir
	} else {
		env["PATH"] = g.tempDir + string(os.PathListSeparator) + currentPath
	}
	env["GIT_TERMINAL_PROMPT"] = "0"
	env["GIT_ASKPASS"] = g.denyPath
	env["SSH_ASKPASS"] = g.denyPath
	env["GIT_SSH_COMMAND"] = g.denyPath
	env["GIT_CONFIG_GLOBAL"] = os.DevNull
	env["GIT_CONFIG_SYSTEM"] = os.DevNull
	values := []string{"", "https://", "http://", "ssh://", "git://", "git@", "file://", "/", "./", "../"}
	keys := make([]string, len(values))
	keys[0] = "credential.helper"
	for i := 1; i < len(values); i++ {
		keys[i] = "url.blocked://glm-worker/.pushInsteadOf"
	}
	env["GIT_CONFIG_COUNT"] = strconv.Itoa(len(values))
	for i := range values {
		env[fmt.Sprintf("GIT_CONFIG_KEY_%d", i)] = keys[i]
		env[fmt.Sprintf("GIT_CONFIG_VALUE_%d", i)] = values[i]
	}
}

func (g *gitAuthorityGuard) verify() error {
	if g == nil || !g.before.active {
		return nil
	}
	attempts, err := readGitAuthorityAttempts(g.attemptLog)
	if err != nil {
		return &GitAuthorityGuardError{Stage: "read-blocked-attempts", Cause: err}
	}
	after, err := captureGitAuthoritySnapshot(g.realGit, g.repoRoot)
	if err != nil {
		return &GitAuthorityGuardError{Stage: "capture-after-call", Mutations: attempts, Cause: err}
	}
	mutations := append([]string(nil), attempts...)
	if g.before.head != after.head {
		mutations = append(mutations, "HEAD")
	}
	if g.before.symbolicHead != after.symbolicHead {
		mutations = append(mutations, "symbolic-HEAD")
	}
	if g.before.refsDigest != after.refsDigest {
		mutations = append(mutations, "refs")
	}
	if g.before.indexDigest != after.indexDigest {
		mutations = append(mutations, "index")
	}
	if g.before.configDigest != after.configDigest {
		mutations = append(mutations, "local-config")
	}
	mutations = uniqueSortedStrings(mutations)
	if len(mutations) == 0 {
		return nil
	}
	stage := "blocked-command"
	if g.before.head != after.head || g.before.symbolicHead != after.symbolicHead || g.before.refsDigest != after.refsDigest || g.before.indexDigest != after.indexDigest || g.before.configDigest != after.configDigest {
		stage = "after-call-mutation"
	}
	return &GitAuthorityGuardError{Stage: stage, Mutations: mutations}
}

func (g *gitAuthorityGuard) cleanup() {
	if g == nil || g.tempDir == "" {
		return
	}
	_ = os.RemoveAll(g.tempDir)
}

func captureGitAuthoritySnapshot(realGit, repoRoot string) (gitAuthoritySnapshot, error) {
	probe := exec.Command(realGit, "-C", repoRoot, "rev-parse", "--show-toplevel")
	if _, err := probe.Output(); err != nil {
		if _, statErr := os.Lstat(filepath.Join(repoRoot, ".git")); os.IsNotExist(statErr) {
			return gitAuthoritySnapshot{}, nil
		}
		return gitAuthoritySnapshot{}, fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	head, err := gitAuthorityOptionalOutput(realGit, repoRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return gitAuthoritySnapshot{}, err
	}
	symbolicHead, err := gitAuthorityOptionalOutput(realGit, repoRoot, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return gitAuthoritySnapshot{}, err
	}
	refs, err := gitAuthorityOutput(realGit, repoRoot, "for-each-ref", "--sort=refname", "--format=%(refname)%00%(objectname)%00%(symref)")
	if err != nil {
		return gitAuthoritySnapshot{}, err
	}
	index, err := gitAuthorityOutput(realGit, repoRoot, "ls-files", "-s", "-z")
	if err != nil {
		return gitAuthoritySnapshot{}, err
	}
	localConfig, err := gitAuthorityOutput(realGit, repoRoot, "config", "--local", "--null", "--list")
	if err != nil {
		return gitAuthoritySnapshot{}, err
	}
	return gitAuthoritySnapshot{
		active:       true,
		head:         strings.TrimSpace(string(head)),
		symbolicHead: strings.TrimSpace(string(symbolicHead)),
		refsDigest:   gitAuthorityDigest(refs),
		indexDigest:  gitAuthorityDigest(index),
		configDigest: gitAuthorityDigest(localConfig),
	}, nil
}

func gitAuthorityOutput(realGit, repoRoot string, args ...string) ([]byte, error) {
	command := exec.Command(realGit, append([]string{"-C", repoRoot}, args...)...)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

func gitAuthorityOptionalOutput(realGit, repoRoot string, args ...string) ([]byte, error) {
	command := exec.Command(realGit, append([]string{"-C", repoRoot}, args...)...)
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil, nil
	}
	return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}

func gitAuthorityDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readGitAuthorityAttempts(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var attempts []string
	for _, line := range strings.Split(string(data), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			attempts = append(attempts, "command:"+value)
		}
	}
	return uniqueSortedStrings(attempts), nil
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func gitAuthorityProxyScript(realGit, attemptLog string) string {
	return "#!/bin/sh\n" +
		"real_git=" + shellSingleQuote(realGit) + "\n" +
		"attempt_log=" + shellSingleQuote(attemptLog) + "\n" +
		"command_name=\n" +
		"expect_value=0\n" +
		"for arg do\n" +
		"  if [ \"$expect_value\" -eq 1 ]; then expect_value=0; continue; fi\n" +
		"  case \"$arg\" in\n" +
		"    -C|-c|--git-dir|--work-tree|--namespace|--config-env) expect_value=1 ;;\n" +
		"    --git-dir=*|--work-tree=*|--namespace=*|--config-env=*) ;;\n" +
		"    --version|--help) if [ -z \"$command_name\" ]; then command_name=$arg; fi ;;\n" +
		"    -*) ;;\n" +
		"    *) command_name=$arg; break ;;\n" +
		"  esac\n" +
		"done\n" +
		"case \"$command_name\" in\n" +
		"  status|diff|log|show|grep|ls-files|rev-parse|rev-list|merge-base|cat-file|for-each-ref|show-ref|describe|name-rev|shortlog|blame|ls-tree|check-ignore|check-attr|--version|--help) exec \"$real_git\" \"$@\" ;;\n" +
		"  '') printf '%s\\n' '<missing-subcommand>' >>\"$attempt_log\"; exit 97 ;;\n" +
		"  *) printf '%s\\n' \"$command_name\" >>\"$attempt_log\"; exit 97 ;;\n" +
		"esac\n"
}

func gitAuthorityDenyTransportScript(attemptLog string) string {
	return "#!/bin/sh\n" +
		"printf '%s\\n' 'transport' >>" + shellSingleQuote(attemptLog) + "\n" +
		"exit 97\n"
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
