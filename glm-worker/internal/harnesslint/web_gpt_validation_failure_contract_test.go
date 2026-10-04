package harnesslint

import (
	"os"
	"strings"
	"testing"
)

func TestWebGPTValidationPreservesVetAndControlRootFailures(t *testing.T) {
	data, err := os.ReadFile("../../../web-gpt-validate.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		`cd "$control_root" || return $?`,
		`go -C "$target_root/glm-worker" vet ./... || return $?`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("web-gpt validation failure contract missing %q", required)
		}
	}
}
