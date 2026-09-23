package runner

import (
	"errors"
	"os"
	"testing"
)

func TestProbeHonorsAlreadyRequestedStop(t *testing.T) {
	r, _, argumentsPath := newProbeFixture(t)
	stop := NewStopController()
	r.AttachStopController(stop)
	stop.Request()

	_, err := r.Probe("opus")
	var interrupted *InterruptedCallError
	if !errors.As(err, &interrupted) {
		t.Fatalf("InterruptedCallErrorを期待: %v", err)
	}
	if _, statErr := os.Stat(argumentsPath); !os.IsNotExist(statErr) {
		t.Fatalf("停止要求済みなのにprobe childが起動しました: %v", statErr)
	}
}
