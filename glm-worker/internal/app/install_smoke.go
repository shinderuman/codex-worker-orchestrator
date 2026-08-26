package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

var validInstallSmokeRoles = map[string]bool{
	"worker":   true,
	"reviewer": true,
	"fix":      true,
	"parent":   true,
}

type InstallSmokeError struct {
	Role             string
	ExitCode         int
	ReuseReason      string
	DurationMS       int64
	LogPath          string
	TreeDigest       string
	SmokeInputDigest string
}

func (e *InstallSmokeError) Error() string {
	return fmt.Sprintf("install smokeが失敗しました (exit %d)", e.ExitCode)
}

type installSmokeOutput struct {
	Status      string                    `json:"status"`
	Result      string                    `json:"result"`
	Role        string                    `json:"role,omitempty"`
	ReuseReason string                    `json:"reuse_reason,omitempty"`
	Evidence    *installSmokeEvidenceView `json:"evidence,omitempty"`
	DurationMS  *int64                    `json:"duration_ms,omitempty"`
	Log         *string                   `json:"log,omitempty"`
	Identity    state.SmokeIdentity       `json:"identity"`
}

type installSmokeEvidenceView struct {
	Result      string              `json:"result"`
	Role        string              `json:"role,omitempty"`
	CompletedAt time.Time           `json:"completed_at"`
	DurationMS  int64               `json:"duration_ms"`
	Identity    state.SmokeIdentity `json:"identity"`
}

func runInstallSmoke(role string, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	identity, err := state.CaptureSmokeIdentity(cfg.RepoRoot, cfg.ClaudeBin)
	if err != nil {
		return fmt.Errorf("install smoke identityを取得できません: %w", err)
	}
	records, err := st.ReadSmokeEvidence()
	if err != nil {
		return fmt.Errorf("install smoke evidenceを読めません: %w", err)
	}
	decision := state.DecideSmokeReuse(records, identity)
	if decision.Reusable {
		return writeJSON(stdout, installSmokeOutput{
			Status:      "reused",
			Result:      state.SmokeResultPass,
			Role:        role,
			ReuseReason: decision.Reason,
			Evidence:    evidenceView(*decision.Record),
			Identity:    identity,
		})
	}

	script := filepath.Join(cfg.RepoRoot, "tests", "install_smoke.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("install smoke scriptがありません: %s: %w", script, err)
	}
	startedAt := time.Now().UTC()
	var smokeLog bytes.Buffer
	smoke := exec.Command(script)
	smoke.Dir = cfg.RepoRoot
	smoke.Stdout = &smokeLog
	smoke.Stderr = &smokeLog
	runErr := smoke.Run()
	completedAt := time.Now().UTC()
	durationMS := completedAt.Sub(startedAt).Milliseconds()

	result := state.SmokeResultPass
	exitCode := 0
	if runErr != nil {
		result = state.SmokeResultFail
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	record := state.SmokeEvidenceRecord{
		Result:      result,
		ExitCode:    exitCode,
		Role:        role,
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		DurationMS:  durationMS,
		Identity:    identity,
	}
	appendErr := st.AppendSmokeEvidence(record)
	logPath := writeSmokeLog(st, identity.TreeDigest, result, smokeLog.Bytes())
	smokeFail := &InstallSmokeError{
		Role:             role,
		ExitCode:         exitCode,
		ReuseReason:      decision.Reason,
		DurationMS:       durationMS,
		LogPath:          logPath,
		TreeDigest:       identity.TreeDigest,
		SmokeInputDigest: identity.SmokeInputDigest,
	}
	if runErr != nil {
		if appendErr != nil {
			return errors.Join(fmt.Errorf("install smoke evidenceを記録できません: %w", appendErr), smokeFail)
		}
		return smokeFail
	}
	if appendErr != nil {
		return fmt.Errorf("install smoke evidenceを記録できません: %w", appendErr)
	}
	return writeJSON(stdout, installSmokeOutput{
		Status:      "executed",
		Result:      result,
		Role:        role,
		ReuseReason: decision.Reason,
		DurationMS:  &durationMS,
		Log:         stringPtr(logPath),
		Identity:    identity,
	})
}

func writeSmokeLog(st *state.StateStore, treeDigest string, result string, data []byte) string {
	path := st.SmokeLogPath(treeDigest, result)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ""
	}
	return path
}

func evidenceView(record state.SmokeEvidenceRecord) *installSmokeEvidenceView {
	return &installSmokeEvidenceView{
		Result:      record.Result,
		Role:        record.Role,
		CompletedAt: record.CompletedAt,
		DurationMS:  record.DurationMS,
		Identity:    record.Identity,
	}
}
