package harnesslint

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type ciWorkflowGuardDocument struct {
	Jobs map[string]struct {
		If string `yaml:"if"`
	} `yaml:"jobs"`
}

func TestCIWorkflowSkipsWebGPTBranchCreation(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCIWorkflowJobGuards(string(data)); err != nil {
		t.Fatal(err)
	}
	var workflow ciWorkflowGuardDocument
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, layout := range []string{"quoted", "folded", "flow", "comments-and-step-conditions"} {
		t.Run(layout, func(t *testing.T) {
			if err := validateCIWorkflowJobGuards(ciWorkflowGuardFixture(workflow, layout)); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, mutation := range []string{"missing", "false", "step-only", "comment-only", "duplicate", "quoted-prefix-space"} {
		t.Run(mutation, func(t *testing.T) {
			source := ciWorkflowGuardFixture(workflow, "quoted")
			guard := strconv.Quote(workflow.Jobs["basic"].If)
			switch mutation {
			case "missing":
				source = strings.Replace(source, "    if: "+guard, "    steps: []", 1)
			case "false":
				source = strings.Replace(source, guard, "false", 1)
			case "step-only":
				source = strings.Replace(source, "    if: "+guard, "    steps:\n      - if: "+guard, 1)
			case "comment-only":
				source = strings.Replace(source, "    if: "+guard, "    # if: "+guard+"\n    steps: []", 1)
			case "duplicate":
				source += "  basic:\n    if: " + guard + "\n"
			case "quoted-prefix-space":
				source = strings.Replace(source, "'web-gpt/'", "'web-gpt/ '", 1)
			}
			if err := validateCIWorkflowJobGuards(source); err == nil {
				t.Fatalf("invalid CI job guard was accepted: %s", source)
			}
		})
	}
}

func validateCIWorkflowJobGuards(source string) error {
	var workflow ciWorkflowGuardDocument
	if err := yaml.Unmarshal([]byte(source), &workflow); err != nil {
		return err
	}
	expected := map[string]string{
		"basic":         "${{ (github.event_name != 'push' || github.event.created == false) && (github.event_name != 'pull_request' || !startsWith(github.head_ref, 'web-gpt/')) }}",
		"lint":          "${{ always() && (github.event_name != 'push' || github.event.created == false) && (github.event_name != 'pull_request' || !startsWith(github.head_ref, 'web-gpt/')) }}",
		"install-smoke": "${{ (github.event_name == 'push' && github.event.created == false && startsWith(github.ref, 'refs/heads/web-gpt/')) || github.event_name == 'workflow_dispatch' }}",
	}
	for name, want := range expected {
		if strings.TrimSpace(workflow.Jobs[name].If) != want {
			return fmt.Errorf("CI job %q guard differs: got %q, want %q", name, workflow.Jobs[name].If, want)
		}
	}
	return nil
}

func ciWorkflowGuardFixture(workflow ciWorkflowGuardDocument, layout string) string {
	var source strings.Builder
	source.WriteString("jobs:\n")
	for _, name := range []string{"basic", "lint", "install-smoke"} {
		guard := workflow.Jobs[name].If
		fmt.Fprintf(&source, "  %s:", strconv.Quote(name))
		switch layout {
		case "flow":
			fmt.Fprintf(&source, " {if: %s, steps: [{if: false}]}\n", strconv.Quote(guard))
		case "folded":
			fmt.Fprintf(&source, "\n    if: >-\n      %s\n", strings.ReplaceAll(guard, " && ", " &&\n      "))
		case "comments-and-step-conditions":
			fmt.Fprintf(&source, "\n    # if: false\n    name: any description\n    if: %s\n    steps:\n      - if: false\n", strconv.Quote(guard))
		default:
			fmt.Fprintf(&source, "\n    if: %s\n", strconv.Quote(guard))
		}
	}
	return source.String()
}
