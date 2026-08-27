package workflow

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func lastPacketFromOutput(t *testing.T, out string) packet.Result {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	emitted := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			emitted = strings.TrimSpace(lines[i])
			break
		}
	}
	if emitted == "" {
		t.Fatalf("no emitted result in output:\n%s", out)
	}
	value, err := packet.ParseStructured([]byte(emitted))
	if err != nil {
		t.Fatalf("emitted result is not machine JSON: %v:\n%s", err, out)
	}
	return value
}

func validateTypedResult(result packet.Result) error {
	if err := packet.ValidateWorkerResult(result); err == nil {
		return nil
	}
	return packet.ValidateReviewerResult(result)
}
