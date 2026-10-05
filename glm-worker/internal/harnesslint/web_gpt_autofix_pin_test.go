package harnesslint

import (
	"regexp"
	"strings"
	"testing"
)

var webGPTAutofixUsesLine = regexp.MustCompile(`(?m)^\s*uses:\s*([^\s#]+)(?:\s+#.*)?\s*$`)

func TestWebGPTAutofixPublishPinsEveryRemoteActionReference(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../.github/workflows/web-gpt-autofix.yml")
	publish := strings.SplitN(text, "\n  publish:\n", 2)
	if len(publish) != 2 {
		t.Fatal("publish job missing")
	}
	fullSHA := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	matches := webGPTAutofixUsesLine.FindAllStringSubmatch(publish[1], -1)
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

func TestWebGPTAutofixPublishPinParserIncludesTrailingComments(t *testing.T) {
	matches := webGPTAutofixUsesLine.FindAllStringSubmatch("    uses: actions/checkout@v6 # comment\n", -1)
	if len(matches) != 1 || matches[0][1] != "actions/checkout@v6" {
		t.Fatalf("commented uses line escaped validation: %#v", matches)
	}
}
