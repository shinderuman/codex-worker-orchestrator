package failurepathadvisory

import "testing"

func TestParseStructuredOutputRejectsTrailingJSONValue(t *testing.T) {
	for name, raw := range map[string]string{
		"second-object": `{"findings":[],"summary":"s"}{"findings":[],"summary":"t"}`,
		"trailing-token": `{"findings":[],"summary":"s"} true`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseStructuredOutput([]byte(raw)); err == nil {
				t.Fatal("trailing JSON value was accepted")
			}
		})
	}
}

func TestParseStructuredOutputAllowsTrailingWhitespace(t *testing.T) {
	if _, err := ParseStructuredOutput([]byte("{\"findings\":[],\"summary\":\"s\"}\n\t ")); err != nil {
		t.Fatalf("trailing whitespace was rejected: %v", err)
	}
}
