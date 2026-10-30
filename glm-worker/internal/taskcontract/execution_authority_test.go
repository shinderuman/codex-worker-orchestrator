package taskcontract

import (
	"strings"
	"testing"
)

func TestParseExecutionAuthority(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		present bool
		mode    string
	}{
		{name: "absent", content: "# task\n\n## External feasibility\n\nstatus: not-applicable\n"},
		{name: "parent-only", content: parentOnlyAuthorityTask(""), present: true, mode: ExecutionAuthorityModeParentOnly},
		{name: "glm", content: parentOnlyAuthorityTask("glm"), present: true, mode: ExecutionAuthorityModeGLM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			authority, err := ParseExecutionAuthority([]byte(tc.content))
			if err != nil {
				t.Fatal(err)
			}
			if authority.Present != tc.present || authority.Mode != tc.mode {
				t.Fatalf("parsed %#v", authority)
			}
		})
	}
}

func TestParseExecutionAuthorityRejectsMalformedSection(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"missing mode":      "# task\n\n## Execution authority\n\nscope: metadata\n",
		"unknown mode":      parentOnlyAuthorityTask("reviewer"),
		"empty value":       "# task\n\n## Execution authority\n\nmode:\n",
		"duplicate mode":    "# task\n\n## Execution authority\n\nmode: parent-only\nmode: glm\n",
		"unknown key":       "# task\n\n## Execution authority\n\nmode: parent-only\nscope: metadata\n",
		"multiple sections": "# task\n\n## Execution authority\n\nmode: parent-only\n\n## Execution authority\n\nmode: glm\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			authority, err := ParseExecutionAuthority([]byte(content))
			if err == nil {
				t.Fatalf("malformed section parsed as %#v", authority)
			}
			if authority.Present {
				t.Fatal("malformed section reported present")
			}
		})
	}
}

func TestParseExecutionAuthorityIgnoresModeInsideLaterSection(t *testing.T) {
	t.Parallel()
	content := "# task\n\n## Execution authority\n\nmode: glm\n\n## Contract\n\nmode: parent-only is not an authority\n"
	authority, err := ParseExecutionAuthority([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if !authority.Present || authority.Mode != ExecutionAuthorityModeGLM {
		t.Fatalf("parsed %#v", authority)
	}
	if strings.Contains(ExecutionAuthorityModeParentOnly, " ") {
		t.Fatal("mode vocabulary must stay machine-strict")
	}
}

func TestParseExecutionAuthorityIgnoresFencedDeclaration(t *testing.T) {
	t.Parallel()
	content := "# task\n\n```\n## Execution authority\n\nmode: parent-only\n```\n"
	authority, err := ParseExecutionAuthority([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if authority.Present || authority.Mode != "" {
		t.Fatalf("fenced declaration became real authority: %#v", authority)
	}
}

func parentOnlyAuthorityTask(mode string) string {
	if mode == "" {
		mode = ExecutionAuthorityModeParentOnly
	}
	return "# task\n\n## External feasibility\n\nstatus: not-applicable\n\n## Execution authority\n\nmode: " + mode + "\n"
}
