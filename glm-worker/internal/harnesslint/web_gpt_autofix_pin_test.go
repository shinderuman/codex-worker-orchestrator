package harnesslint

import (
	"regexp"
	"strings"
	"testing"
)

func TestWebGPTAutofixPublishPinsEveryRemoteActionReference(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../.github/workflows/web-gpt-autofix.yml")
	publish := strings.SplitN(text, "\n  publish:\n", 2)
	if len(publish) != 2 {
		t.Fatal("publish job missing")
	}
	usesLine := regexp.MustCompile(`(?m)^\s*uses:\s*([^\s#]+)\s*$`)
	fullSHA := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	matches := usesLine.FindAllStringSubmatch(publish[1], -1)
	if len(matches) == 0 {
		t.Fatal("publish job has no action references")
	}
	for _, match := range matches {
		ref := match[1]
		if strings.HasPrefix(ref, "./") {
			continue
		}
		if !fullSHA.MatchString(ref) {
			t.Fatalf("write-capable publish job must pin every remote action to a full commit SHA: %q", ref)
		}
	}
}
