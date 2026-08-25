package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type decodeProcessError struct {
	Error struct {
		Kind    string         `json:"kind"`
		Message string         `json:"message"`
		Detail  map[string]any `json:"detail"`
	} `json:"error"`
}

func writeProcessErrorJSON(t *testing.T, err error) (decodeProcessError, string) {
	t.Helper()
	var out bytes.Buffer
	if writeErr := WriteProcessError(&out, err); writeErr != nil {
		t.Fatalf("WriteProcessError error = %v", writeErr)
	}
	raw := strings.TrimSpace(out.String())
	if strings.ContainsAny(raw, "\n\r") {
		t.Fatalf("process error出力が1行ではありません: %q", raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("process error出力がmachine JSONではありません: %v: %q", err, raw)
	}
	if len(decoded) != 1 || decoded["error"] == nil {
		t.Fatalf("process error出力はtop-level key \"error\" 1つだけ: %q", raw)
	}
	var envelope decodeProcessError
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("process error envelope decode: %v: %q", err, raw)
	}
	return envelope, raw
}

func TestWriteProcessErrorKindContract(t *testing.T) {
	rateLimit := runner.ZaiRateLimitError{
		Phase:     "reviewer-1",
		Limit:     runner.ZaiFiveHourLimit{ResetAtCST: "2026-08-24 07:06:34", ResetAtRFC3339: "2026-08-24T07:06:34+08:00"},
		TaskID:    "task-rate-1",
		RepoRoot:  "/repo/root",
		RepoShort: "root",
	}
	provider := &runner.ProviderUnavailableError{
		Phase:          "worker-new",
		Classification: "http-503",
		Probes:         4,
		Elapsed:        90 * time.Minute,
		TaskID:         "task-provider-1",
		RepoRoot:       "/repo/root",
	}
	cases := []struct {
		name        string
		err         error
		wantKind    string
		wantMessage string
		checkDetail func(t *testing.T, detail map[string]any)
	}{
		{
			name: "usage", err: &UsageError{Message: "usage: glm-worker --status"},
			wantKind: "usage", wantMessage: "usage: glm-worker --status",
		},
		{
			name: "stdin payload", err: &StdinPayloadError{Message: "stdin payload sha256 mismatch"},
			wantKind: "stdin_payload", wantMessage: "stdin payload sha256 mismatch",
		},
		{
			name: "not found", err: &NotFoundError{Message: "task log not found"},
			wantKind: "not_found", wantMessage: "task log not found",
		},
		{
			name: "no resume checkpoint", err: state.ErrNoResumeCheckpoint,
			wantKind: "not_found", wantMessage: state.ErrNoResumeCheckpoint.Error(),
		},
		{
			name: "repo lock held", err: ErrRepoLockHeld,
			wantKind: "repo_lock_held", wantMessage: ErrRepoLockHeld.Error(),
		},
		{
			name:     "worker error",
			err:      &workflow.WorkerError{Phase: "worker-new", ExitCode: 1, Tail: "tail output", Message: "worker failed"},
			wantKind: "worker_error", wantMessage: "worker failed",
			checkDetail: func(t *testing.T, detail map[string]any) {
				t.Helper()
				want := map[string]any{"phase": "worker-new", "exit_code": float64(1), "output_tail": "tail output"}
				if len(detail) != len(want) {
					t.Fatalf("worker_error detail = %#v", detail)
				}
				for key, value := range want {
					if detail[key] != value {
						t.Fatalf("worker_error detail[%s] = %#v want %#v", key, detail[key], value)
					}
				}
			},
		},
		{
			name: "rate limited", err: rateLimit,
			wantKind:    "rate_limited",
			wantMessage: "Z.ai Coding Plan 5h limit reached; task is stopped and resumable",
			checkDetail: func(t *testing.T, detail map[string]any) {
				t.Helper()
				for key, want := range map[string]any{
					"limit": "ZAI_GLM_CODING_PLAN_5H", "phase": "reviewer-1",
					"task_id": "task-rate-1", "repo_root": "/repo/root",
					"reset_at_cst": "2026-08-24 07:06:34", "reset_at_rfc3339": "2026-08-24T07:06:34+08:00",
					"resume_available": true, "auto_resume_available": true,
					"auto_resume_at_rfc3339": "2026-08-24T07:08:34+08:00",
					"auto_resume_key":        rateLimit.AutoResumeKey(),
				} {
					if detail[key] != want {
						t.Fatalf("rate_limited detail[%s] = %#v want %#v", key, detail[key], want)
					}
				}
				if len(detail) != 10 {
					t.Fatalf("rate_limited detail = %#v", detail)
				}
			},
		},
		{
			name: "provider unavailable", err: provider,
			wantKind:    "provider_unavailable",
			wantMessage: "provider stayed unavailable after probe budget; task is stopped and resumable",
			checkDetail: func(t *testing.T, detail map[string]any) {
				t.Helper()
				for key, want := range map[string]any{
					"phase": "worker-new", "classification": "http-503",
					"probes": float64(4), "elapsed_ms": float64(90 * 60 * 1000),
					"task_id": "task-provider-1", "repo_root": "/repo/root",
					"resume_available": true,
				} {
					if detail[key] != want {
						t.Fatalf("provider_unavailable detail[%s] = %#v want %#v", key, detail[key], want)
					}
				}
				if len(detail) != 7 {
					t.Fatalf("provider_unavailable detail = %#v", detail)
				}
			},
		},
		{
			name: "verification failed", err: &VerificationError{Outcome: autoresume.Fail, Reason: "TOML id mismatch"},
			wantKind: "verification_failed", wantMessage: "TOML id mismatch",
		},
		{
			name: "verification unavailable", err: &VerificationError{Outcome: autoresume.Unavailable, Reason: "sqlite3 not found"},
			wantKind: "verification_unavailable", wantMessage: "sqlite3 not found",
		},
		{
			name: "internal", err: errors.New("boom"),
			wantKind: "internal", wantMessage: "boom",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			envelope, raw := writeProcessErrorJSON(t, c.err)
			if envelope.Error.Kind != c.wantKind {
				t.Fatalf("kind = %q want %q: %s", envelope.Error.Kind, c.wantKind, raw)
			}
			if envelope.Error.Message != c.wantMessage {
				t.Fatalf("message = %q want %q", envelope.Error.Message, c.wantMessage)
			}
			if c.checkDetail == nil {
				if len(envelope.Error.Detail) != 0 {
					t.Fatalf("detailを持たないkindのdetail = %#v: %s", envelope.Error.Detail, raw)
				}
				return
			}
			if len(envelope.Error.Detail) == 0 {
				t.Fatalf("detailを持つkindのdetailが空です: %s", raw)
			}
			c.checkDetail(t, envelope.Error.Detail)
		})
	}
}
