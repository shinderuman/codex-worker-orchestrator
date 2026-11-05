package app

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	isolatedScenarioEnv = "GLM_ISOLATED_SCENARIO"

	isolatedScenarioKeyTypedCompletion = "typed-completion"

	isolatedScenarioKeyParentOnlyCompletion = "parent-only-completion"

	isolatedScenarioKeyParentOnlyReentry = "parent-only-reentry"

	isolatedScenarioKeyTypedRetries = "typed-retries"

	isolatedScenarioKeyMetadataCommitHook = "metadata-commit-hook"

	isolatedScenarioKeyCompletionCLIProcessDeath = "completion-cli-process-death"

	isolatedScenarioKeyUserTerminatedAdvancement = "user-terminated-advancement"

	isolatedScenarioKeyReboundCandidatePublish = "rebound-candidate-publish"

	isolatedScenarioKeyAdoptedAdmissionPublish = "adopted-admission-publish"

	isolatedScenarioKeyExecutionPreparationLease = "execution-preparation-lease"

	isolatedScenarioKeyTypedPublicationAbort = "typed-publication-abort"
)

func runIsolatedScenario(t *testing.T, family, scenario string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	key := family + "/" + scenario
	command := exec.Command(executable, "-test.run", "^TestIsolatedHeavyScenario$", "-test.timeout=10m", "-test.v")
	command.Env = append(os.Environ(), isolatedScenarioEnv+"="+key)
	started := time.Now()
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	runErr := command.Run()
	if appFixtureCostTarget(t) {
		t.Logf("app-fixture-cost kind=isolated-scenario key=%s elapsed=%s", key, time.Since(started))
	}
	if runErr != nil {
		t.Fatalf("isolated scenario %s failed: %v\n%s", key, runErr, output.String())
	}
}

func TestIsolatedHeavyScenario(t *testing.T) {
	key := os.Getenv(isolatedScenarioEnv)
	if key == "" {
		return
	}
	family, scenario, ok := strings.Cut(key, "/")
	if !ok {
		t.Fatalf("isolated scenario key %q must be family/scenario", key)
	}
	isolatedScenarioBody(t, family, scenario)
}

func isolatedScenarioBody(t *testing.T, family, scenario string) {
	switch family {
	case isolatedScenarioKeyTypedCompletion:
		for _, candidate := range typedCompletionScenarios() {
			if candidate.name == scenario {
				typedCompletionScenarioBody(t, candidate)
				return
			}
		}
		t.Fatalf("unknown %s scenario %q", family, scenario)
	case isolatedScenarioKeyParentOnlyCompletion:
		for _, candidate := range parentOnlyCompletionScenarios() {
			if candidate.name == scenario {
				parentOnlyCompletionScenarioBody(t, candidate)
				return
			}
		}
		t.Fatalf("unknown %s scenario %q", family, scenario)
	case isolatedScenarioKeyParentOnlyReentry:
		switch scenario {
		case "reenter", "parent-source-rebind":
			parentOnlyReentryRouteBody(t, scenario)
		default:
			t.Fatalf("unknown %s scenario %q", family, scenario)
		}
	case isolatedScenarioKeyTypedRetries:
		switch scenario {
		case "false":
			typedCompletionRetryScenarioBody(t, false)
		case "true":
			typedCompletionRetryScenarioBody(t, true)
		default:
			t.Fatalf("unknown %s scenario %q", family, scenario)
		}
	case isolatedScenarioKeyMetadataCommitHook:
		metadataCommitHookBody(t)
	case isolatedScenarioKeyCompletionCLIProcessDeath:
		completionCLIProcessDeathBody(t)
	case isolatedScenarioKeyUserTerminatedAdvancement:
		userTerminatedAdvancementBody(t)
	case isolatedScenarioKeyReboundCandidatePublish:
		reboundCandidatePublishBody(t)
	case isolatedScenarioKeyAdoptedAdmissionPublish:
		adoptedAdmissionPublishBody(t)
	case isolatedScenarioKeyExecutionPreparationLease:
		executionPreparationLeaseBody(t)
	case isolatedScenarioKeyTypedPublicationAbort:
		typedPublicationAbortBody(t)
	default:
		t.Fatalf("unknown isolated scenario family %q", family)
	}
}
