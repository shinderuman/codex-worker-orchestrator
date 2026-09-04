package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type InstallSmokeError struct {
	Role            string
	ExitCode        int
	ExitSource      string
	Evidence        string
	EvidenceWarning string
	Truncated       bool
	DurationMS      int64
}

type installSmokeOutput struct {
	Status     string `json:"status"`
	Result     string `json:"result"`
	Role       string `json:"role,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type installSmokeCapture struct {
	tail  bytes.Buffer
	total int64
}

type installSmokeEvidenceRun struct {
	path  string
	mtime time.Time
}

type installSmokeRedaction struct {
	pattern     *regexp.Regexp
	replacement string
}

const (
	installSmokeRunDirectory         = "install-smoke-runs"
	installSmokeEvidenceLog          = "smoke.log"
	installSmokeEvidenceLimit        = 32 * 1024
	retainedInstallSmokeEvidenceRuns = 5
	installSmokeRedactedValue        = "[redacted]"
	installSmokeInvalidRune          = "�"
)

var validInstallSmokeRoles = map[string]bool{
	"worker":   true,
	"reviewer": true,
	"fix":      true,
	"parent":   true,
}

var installSmokeRedactions = []installSmokeRedaction{
	{
		pattern:     regexp.MustCompile(`(?i)\b([a-z0-9_-]*(?:api[_-]?key|access[_-]?key|secret|token|password|passwd|credential|cookie|session[_-]?id)[a-z0-9_-]*)(\s*[=:]\s*)("[^"\n]*"|'[^'\n]*'|[^\s,;&]+)`),
		replacement: `$1$2` + installSmokeRedactedValue,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b(authorization\s*:\s*)(?:bearer\s+)?[^\s,;&]+`),
		replacement: `$1` + installSmokeRedactedValue,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b(?:sk|ghp|gho|ghu|ghs|ghr|glpat|github_pat|xox[baprs])[-_][a-z0-9_-]{8,}\b|\bakia[0-9a-z]{16}\b|\baiza[0-9a-z_-]{30,}\b`),
		replacement: installSmokeRedactedValue,
	},
	{
		pattern:     regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)([^\s/@:]+):([^\s/@]+)@`),
		replacement: `${1}` + installSmokeRedactedValue + `@`,
	},
}

func (e *InstallSmokeError) Error() string {
	return fmt.Sprintf("install smokeが失敗しました (exit %d)", e.ExitCode)
}

func (c *installSmokeCapture) Write(data []byte) (int, error) {
	c.total += int64(len(data))
	c.tail.Write(data)
	if excess := c.tail.Len() - installSmokeEvidenceLimit; excess > 0 {
		c.tail.Next(excess)
	}
	return len(data), nil
}

func (c *installSmokeCapture) truncated() bool {
	return c.total > int64(c.tail.Len())
}

func (c *installSmokeCapture) evidence() []byte {
	if !c.truncated() {
		return c.tail.Bytes()
	}
	index := bytes.IndexByte(c.tail.Bytes(), '\n')
	if index < 0 {
		return nil
	}
	return c.tail.Bytes()[index+1:]
}

func runInstallSmoke(role string, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	script := filepath.Join(cfg.RepoRoot, "tests", "install_smoke.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("install smoke scriptがありません: %s: %w", script, err)
	}
	started := time.Now()
	capture := &installSmokeCapture{}
	command := exec.Command(script)
	command.Dir = cfg.RepoRoot
	command.Stdout = capture
	command.Stderr = capture
	runErr := command.Run()
	durationMS := time.Since(started).Milliseconds()
	if runErr == nil {
		st.RecordValidation("install-smoke", "install-smoke", role, "pass", 0, state.ValidationExitSourceTarget, durationMS, "")
		return writeJSON(stdout, installSmokeOutput{
			Status:     "executed",
			Result:     "pass",
			Role:       role,
			DurationMS: durationMS,
		})
	}
	return failInstallSmoke(role, st, capture, runErr, durationMS)
}

func failInstallSmoke(role string, st *state.StateStore, capture *installSmokeCapture, runErr error, durationMS int64) error {
	exitCode := 1
	exitSource := state.ValidationExitSourceWrapper
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		exitCode = exitErr.ExitCode()
		exitSource = state.ValidationExitSourceTarget
	}
	evidence, warning := saveInstallSmokeEvidence(st, capture, runErr, exitSource)
	st.RecordValidation("install-smoke", "install-smoke", role, "fail", exitCode, exitSource, durationMS, evidence)
	return &InstallSmokeError{
		Role:            role,
		ExitCode:        exitCode,
		ExitSource:      exitSource,
		Evidence:        evidence,
		EvidenceWarning: warning,
		Truncated:       capture.truncated(),
		DurationMS:      durationMS,
	}
}

func saveInstallSmokeEvidence(st *state.StateStore, capture *installSmokeCapture, runErr error, exitSource string) (string, string) {
	raw := capture.evidence()
	if exitSource == state.ValidationExitSourceWrapper {
		raw = []byte(strings.TrimSpace(runErr.Error()) + "\n" + string(raw))
	}
	content := boundInstallSmokeEvidence(sanitizeInstallSmokeEvidence(raw))
	runID, err := newValidationRunID()
	if err != nil {
		return "", installSmokeEvidenceWarning("run id生成", err)
	}
	path, err := writeInstallSmokeEvidence(st, runID, content)
	if err != nil {
		return "", installSmokeEvidenceWarning("保存", err)
	}
	warning := ""
	if err := pruneInstallSmokeEvidence(st, runID); err != nil {
		warning = installSmokeEvidenceWarning("retention整理", err)
	}
	return path, warning
}

func boundInstallSmokeEvidence(text string) string {
	if len(text) <= installSmokeEvidenceLimit {
		return text
	}
	offset := len(text) - installSmokeEvidenceLimit
	for offset < len(text) && !utf8.RuneStart(text[offset]) {
		offset++
	}
	return text[offset:]
}

func writeInstallSmokeEvidence(st *state.StateStore, runID, content string) (string, error) {
	if !validValidationRunID(runID) {
		return "", fmt.Errorf("invalid validation run id")
	}
	path := st.Path(filepath.Join(installSmokeRunDirectory, runID, installSmokeEvidenceLog))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func pruneInstallSmokeEvidence(st *state.StateStore, currentRunID string) error {
	paths, err := filepath.Glob(st.Path(filepath.Join(installSmokeRunDirectory, "*")))
	if err != nil {
		return err
	}
	current := st.Path(filepath.Join(installSmokeRunDirectory, currentRunID))
	entries := make([]installSmokeEvidenceRun, 0, len(paths))
	for _, path := range paths {
		if path == current {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		entries = append(entries, installSmokeEvidenceRun{path: path, mtime: info.ModTime()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mtime.After(entries[j].mtime) })
	for index, entry := range entries {
		if index < retainedInstallSmokeEvidenceRuns {
			continue
		}
		if err := os.RemoveAll(entry.path); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeInstallSmokeEvidence(data []byte) string {
	text := strings.ReplaceAll(strings.ToValidUTF8(string(data), installSmokeInvalidRune), "\x00", installSmokeInvalidRune)
	for _, redaction := range installSmokeRedactions {
		text = redaction.pattern.ReplaceAllString(text, redaction.replacement)
	}
	return text
}

func installSmokeEvidenceWarning(operation string, err error) string {
	return fmt.Sprintf("install smoke evidenceの%sに失敗しました: %v", operation, err)
}
