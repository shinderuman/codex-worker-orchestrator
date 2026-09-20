package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicationPreToolUseCheckedBlocksNestedGitBypass(t *testing.T) {
	commands := []string{
		`echo "$(git push origin main --no-verify)"`,
		`printf '%s\n' "$(command git commit --no-verify -m bypass)"`,
		`cat <(git push origin main --no-verify)`,
		"echo `git push origin main --no-verify`",
		`builtin command git push origin main --no-verify`,
		`builtin eval git push origin main --no-verify`,
		`builtin exec git push origin main --no-verify`,
		`<<- EOF git push origin main --no-verify`,
		`git<<-EOF push origin main --no-verify`,
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			output := publicationPreToolCheckedDecision(t, command)
			if output.Decision != "block" || output.Code != publicationGitGuardBypassCode || output.Reason == "" {
				t.Fatalf("nested bypass was not blocked: %#v", output)
			}
		})
	}
}

func TestPublicationPreToolUseCheckedFailsClosedForNestedDynamicGit(t *testing.T) {
	commands := []string{
		`echo "$($GIT push origin main)"`,
		`cat <($GIT push origin main)`,
		"echo `$GIT push origin main`",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			output := publicationPreToolCheckedDecision(t, command)
			if output.Decision != "block" || output.Code != publicationGitClassificationCode || output.Reason != publicationGitClassificationReason {
				t.Fatalf("nested dynamic Git classification did not fail closed: %#v", output)
			}
		})
	}
}

func TestPublicationPreToolUseCheckedAllowsUnrelatedSubstitutions(t *testing.T) {
	commands := []string{
		`echo "$(date)"`,
		`cat <(printf safe)`,
		"echo `date`",
		`echo $((1 + 2))`,
		`echo $((1 + $(printf 2)))`,
		`builtin printf safe`,
		`builtin command git push origin main`,
		`<<- EOF git push origin main`,
		`git<<-EOF push origin main`,
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := runPublicationPreToolUseChecked(publicationPreToolPayload(t, command), &stdout); err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(stdout.String()) != "" {
				t.Fatalf("allowed command emitted hook decision: %q", stdout.String())
			}
		})
	}
}

func TestPublicationPreToolUseCheckedIgnoresQuotedSubstitutionSyntax(t *testing.T) {
	commands := []string{
		`echo '$(git push origin main --no-verify)'`,
		"echo '`git push origin main --no-verify`'",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := runPublicationPreToolUseChecked(publicationPreToolPayload(t, command), &stdout); err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(stdout.String()) != "" {
				t.Fatalf("quoted literal emitted hook decision: %q", stdout.String())
			}
		})
	}
}

func publicationPreToolCheckedDecision(t *testing.T, command string) publicationPreToolUseOutput {
	t.Helper()
	var stdout bytes.Buffer
	if err := runPublicationPreToolUseChecked(publicationPreToolPayload(t, command), &stdout); err != nil {
		t.Fatal(err)
	}
	var output publicationPreToolUseOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("block output is not JSON: %v: %q", err, stdout.String())
	}
	return output
}
