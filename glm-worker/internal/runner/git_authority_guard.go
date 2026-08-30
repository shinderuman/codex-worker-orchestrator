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
	"strings"
)

type GitRefState struct {
	Name     string `json:"name"`
	ObjectID string `json:"object_id"`
	Symref   string `json:"symref,omitempty"`
}

type GitRefChange struct {
	Name   string       `json:"name"`
	Before *GitRefState `json:"before,omitempty"`
	After  *GitRefState `json:"after,omitempty"`
}

type GitAuthorityGuardError struct {
	Stage               string
	Mutations           []string
	RefBeforeDigest     string
	RefAfterDigest      string
	RefChanges          []GitRefChange
	RefChangesTruncated bool
	Cause               error
}

type gitAuthoritySnapshot struct {
	active       bool
	head         string
	symbolicHead string
	refsDigest   string
	refs         []GitRefState
	indexDigest  string
	configDigest string
}

type gitAuthorityGuard struct {
	repoRoot      string
	realGit       string
	tempDir       string
	workerTempDir string
	proxyPath     string
	denyPath      string
	attemptLog    string
	metadataPaths []string
	before        gitAuthoritySnapshot
}

const (
	maxGitAuthorityRefChanges   = 64
	guardStageAfterCallMutation = "after-call-mutation"
)

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
	metadataPaths, err := resolveGitAuthorityMetadataPaths(realGit, repoRoot)
	if err != nil {
		return nil, &GitAuthorityGuardError{Stage: "resolve-metadata", Cause: err}
	}
	guard.metadataPaths = metadataPaths
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
	g.workerTempDir = filepath.Join(tempDir, "worker")
	if err := os.MkdirAll(g.workerTempDir, 0o700); err != nil {
		return &GitAuthorityGuardError{Stage: "prepare-command-proxy", Cause: err}
	}
	g.proxyPath = filepath.Join(tempDir, "git")
	g.denyPath = filepath.Join(tempDir, "deny-git-transport")
	g.attemptLog = filepath.Join(tempDir, "blocked-attempts")
	if err := os.WriteFile(g.attemptLog, nil, 0o600); err != nil {
		return &GitAuthorityGuardError{Stage: "prepare-command-proxy", Cause: err}
	}
	proxy := gitAuthorityProxyScript(g.realGit, g.attemptLog, g.repoRoot, g.workerTempDir)
	if err := os.WriteFile(g.proxyPath, []byte(proxy), 0o700); err != nil {
		return &GitAuthorityGuardError{Stage: "prepare-command-proxy", Cause: err}
	}
	deny := gitAuthorityDenyTransportScript(g.attemptLog)
	if err := os.WriteFile(g.denyPath, []byte(deny), 0o700); err != nil {
		return &GitAuthorityGuardError{Stage: "prepare-command-proxy", Cause: err}
	}
	return nil
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
	snapshotMutations := gitAuthoritySnapshotMutations(g.before, after)
	mutations := append(append([]string(nil), attempts...), snapshotMutations...)
	mutations = uniqueSortedStrings(mutations)
	if len(mutations) == 0 {
		return nil
	}
	stage := "blocked-command"
	if len(snapshotMutations) > 0 {
		stage = guardStageAfterCallMutation
	}
	guardErr := &GitAuthorityGuardError{Stage: stage, Mutations: mutations}
	if g.before.refsDigest != after.refsDigest {
		guardErr.RefBeforeDigest = g.before.refsDigest
		guardErr.RefAfterDigest = after.refsDigest
		guardErr.RefChanges, guardErr.RefChangesTruncated = gitAuthorityRefChanges(g.before.refs, after.refs)
	}
	return guardErr
}

func gitAuthoritySnapshotMutations(before, after gitAuthoritySnapshot) []string {
	mutations := make([]string, 0, 5)
	if before.head != after.head {
		mutations = append(mutations, "HEAD")
	}
	if before.symbolicHead != after.symbolicHead {
		mutations = append(mutations, "symbolic-HEAD")
	}
	if before.refsDigest != after.refsDigest {
		mutations = append(mutations, "refs")
	}
	if before.indexDigest != after.indexDigest {
		mutations = append(mutations, "index")
	}
	if before.configDigest != after.configDigest {
		mutations = append(mutations, "local-config")
	}
	return mutations
}

func gitAuthorityRefChanges(before, after []GitRefState) ([]GitRefChange, bool) {
	beforeByName := make(map[string]GitRefState, len(before))
	afterByName := make(map[string]GitRefState, len(after))
	names := make(map[string]struct{}, len(before)+len(after))
	for _, ref := range before {
		beforeByName[ref.Name] = ref
		names[ref.Name] = struct{}{}
	}
	for _, ref := range after {
		afterByName[ref.Name] = ref
		names[ref.Name] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	changes := make([]GitRefChange, 0)
	truncated := false
	for _, name := range ordered {
		beforeRef, hadBefore := beforeByName[name]
		afterRef, hasAfter := afterByName[name]
		if hadBefore && hasAfter && beforeRef == afterRef {
			continue
		}
		if len(changes) == maxGitAuthorityRefChanges {
			truncated = true
			break
		}
		change := GitRefChange{Name: name}
		if hadBefore {
			value := beforeRef
			change.Before = &value
		}
		if hasAfter {
			value := afterRef
			change.After = &value
		}
		changes = append(changes, change)
	}
	return changes, truncated
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
	parsedRefs, err := parseGitAuthorityRefs(refs)
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
		refs:         parsedRefs,
		indexDigest:  gitAuthorityDigest(index),
		configDigest: gitAuthorityDigest(localConfig),
	}, nil
}

func parseGitAuthorityRefs(data []byte) ([]GitRefState, error) {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return []GitRefState{}, nil
	}
	lines := strings.Split(text, "\n")
	refs := make([]GitRefState, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\x00")
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("git for-each-ref output is malformed")
		}
		refs = append(refs, GitRefState{Name: parts[0], ObjectID: parts[1], Symref: parts[2]})
	}
	return refs, nil
}

func CaptureGitAuthorityRefDigest(repoRoot string) (string, error) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("resolve git for ref capture: %w", err)
	}
	refs, err := gitAuthorityOutput(realGit, repoRoot, "for-each-ref", "--sort=refname", "--format=%(refname)%00%(objectname)%00%(symref)")
	if err != nil {
		return "", err
	}
	return gitAuthorityDigest(refs), nil
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
