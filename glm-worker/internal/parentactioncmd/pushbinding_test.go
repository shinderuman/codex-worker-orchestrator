package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type pushBindingFixture struct {
	repo    string
	remote  string
	branch  string
	baseOID string
}

func TestPushBindingClassifiesSyncedPostcondition(t *testing.T) {
	fixture := newPushBindingFixture(t)
	var output bytes.Buffer
	if err := runPushBinding(fixture.repo, nil, &output); err != nil {
		t.Fatal(err)
	}
	result := decodePushBindingOutput(t, output)
	if result.Status != "classified" || result.Classification != pushBindingClassificationSynced {
		t.Fatalf("result = %#v", result)
	}
	if result.Target == nil || result.Target.RemoteName != "origin" ||
		result.Target.RemoteRef != "refs/heads/"+fixture.branch ||
		result.Target.TrackingOID != fixture.baseOID ||
		result.Target.LocalOID != fixture.baseOID {
		t.Fatalf("target = %#v", result.Target)
	}
	if !result.TreeClean || result.AttemptOutcome != pushBindingAttemptNone || result.ExpectedOID != fixture.baseOID {
		t.Fatalf("result = %#v", result)
	}
	if result.Postcondition == nil || !result.Postcondition.Met || result.Postcondition.RemoteOID != fixture.baseOID {
		t.Fatalf("postcondition = %#v", result.Postcondition)
	}
	if result.RemoteWrite != nil || result.Failure != nil {
		t.Fatalf("result = %#v", result)
	}
}

func TestPushBindingClassifiesLocalCleanAheadWithParentStandingAuthorityEntry(t *testing.T) {
	fixture := newPushBindingFixture(t)
	writePushBindingFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "second")
	var output bytes.Buffer
	if err := runPushBinding(fixture.repo, nil, &output); err != nil {
		t.Fatal(err)
	}
	result := decodePushBindingOutput(t, output)
	if result.Classification != pushBindingClassificationLocalAhead {
		t.Fatalf("classification = %s", result.Classification)
	}
	if !result.TreeClean || result.Target == nil || result.Target.Ahead != 1 || result.Target.Behind != 0 {
		t.Fatalf("result = %#v", result)
	}
	if result.Postcondition != nil && result.Postcondition.Met {
		t.Fatalf("postcondition = %#v", result.Postcondition)
	}
	if result.RemoteWrite == nil || result.RemoteWrite.Authorization != pushBindingAuthorizationStanding ||
		result.RemoteWrite.Executor != pushBindingExecutorParentOnly ||
		result.RemoteWrite.RemoteRef != "refs/heads/"+fixture.branch ||
		result.RemoteWrite.ExpectedOID != result.Target.LocalOID {
		t.Fatalf("remote_write = %#v", result.RemoteWrite)
	}
}

func TestPushBindingClassifiesAttemptOutcomeWithLocalAhead(t *testing.T) {
	cases := []struct {
		attemptOutcome string
		classification string
	}{
		{pushBindingAttemptRejected, pushBindingClassificationPushRejected},
		{pushBindingAttemptNetworkError, pushBindingClassificationNetworkFailure},
	}
	for _, testCase := range cases {
		fixture := newPushBindingFixture(t)
		writePushBindingFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
		runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "second")
		var output bytes.Buffer
		if err := runPushBinding(fixture.repo, []string{"--attempt-outcome", testCase.attemptOutcome}, &output); err != nil {
			t.Fatalf("%s: %v", testCase.attemptOutcome, err)
		}
		result := decodePushBindingOutput(t, output)
		if result.Classification != testCase.classification || result.AttemptOutcome != testCase.attemptOutcome {
			t.Fatalf("%s: result = %#v", testCase.attemptOutcome, result)
		}
		if result.RemoteWrite == nil || result.RemoteWrite.RemoteRef != "refs/heads/"+fixture.branch ||
			result.RemoteWrite.ExpectedOID != result.Target.LocalOID {
			t.Fatalf("%s: remote_write = %#v", testCase.attemptOutcome, result.RemoteWrite)
		}
	}
}

func TestPushBindingClassifiesNetworkFailureFromUnreachableRemote(t *testing.T) {
	fixture := newPushBindingFixture(t)
	missing := filepath.Join(t.TempDir(), "missing.git")
	runFinalizationGit(t, fixture.repo, "remote", "set-url", "origin", missing)
	var output bytes.Buffer
	if err := runPushBinding(fixture.repo, nil, &output); err != nil {
		t.Fatal(err)
	}
	result := decodePushBindingOutput(t, output)
	if result.Classification != pushBindingClassificationNetworkFailure {
		t.Fatalf("classification = %s", result.Classification)
	}
	if result.RemoteProbe == nil || result.RemoteProbe.State != pushBindingProbeNetworkError || result.RemoteProbe.ExitCode == 0 {
		t.Fatalf("remote_probe = %#v", result.RemoteProbe)
	}
	if result.Postcondition == nil || result.Postcondition.Met {
		t.Fatalf("postcondition = %#v", result.Postcondition)
	}
}

func TestPushBindingNeverLeaksCredentialBearingRemoteURL(t *testing.T) {
	fixture := newPushBindingFixture(t)
	sentinel := "glmpw-sentinel-credential-0123"
	credentialedURL := "https://worker:" + sentinel + "@127.0.0.1:1/leak-probe.git"
	runFinalizationGit(t, fixture.repo, "remote", "set-url", "origin", credentialedURL)
	configured := pushBindingGitOutput(t, fixture.repo, "config", "--get", "remote.origin.url")
	if !strings.Contains(configured, sentinel) {
		t.Fatalf("fixture remote url does not carry the credential sentinel: %q", configured)
	}
	var output bytes.Buffer
	if err := runPushBinding(fixture.repo, nil, &output); err != nil {
		t.Fatal(err)
	}
	raw := output.String()
	if strings.Contains(raw, sentinel) || strings.Contains(raw, credentialedURL) || strings.Contains(raw, "remote_url") {
		t.Fatalf("push binding output leaks the remote url or credential: %s", raw)
	}
	result := decodePushBindingOutput(t, output)
	if result.Status != "classified" || result.Classification != pushBindingClassificationNetworkFailure {
		t.Fatalf("result = %#v", result)
	}
	if result.Target == nil || result.Target.RemoteName != "origin" || result.Target.RemoteRef != "refs/heads/"+fixture.branch {
		t.Fatalf("target = %#v", result.Target)
	}
	if result.RemoteWrite == nil || result.RemoteWrite.RemoteName != "origin" || result.RemoteWrite.RemoteRef != "refs/heads/"+fixture.branch {
		t.Fatalf("remote_write = %#v", result.RemoteWrite)
	}
}

func TestPushBindingClassifiesNonFastForwardWhenRemoteDiverges(t *testing.T) {
	fixture := newPushBindingFixture(t)
	writePushBindingFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "second")
	runFinalizationGit(t, fixture.repo, "checkout", "-q", "-b", "side")
	writePushBindingFile(t, fixture.repo, "side.txt", "side\n")
	runFinalizationGit(t, fixture.repo, "add", "side.txt")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "divergent")
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "side:refs/heads/"+fixture.branch)
	runFinalizationGit(t, fixture.repo, "checkout", "-q", fixture.branch)
	runFinalizationGit(t, fixture.repo, "branch", "-q", "-D", "side")
	var output bytes.Buffer
	if err := runPushBinding(fixture.repo, nil, &output); err != nil {
		t.Fatal(err)
	}
	result := decodePushBindingOutput(t, output)
	if result.Classification != pushBindingClassificationNonFastForward {
		t.Fatalf("classification = %s", result.Classification)
	}
	if result.Target == nil || result.Target.Behind != 1 {
		t.Fatalf("target = %#v", result.Target)
	}
}

func TestPushBindingClassifiesRemoteRefMismatchWhenRefMissing(t *testing.T) {
	fixture := newPushBindingFixture(t)
	runFinalizationGit(t, fixture.remote, "update-ref", "-d", "refs/heads/"+fixture.branch)
	var output bytes.Buffer
	if err := runPushBinding(fixture.repo, nil, &output); err != nil {
		t.Fatal(err)
	}
	result := decodePushBindingOutput(t, output)
	if result.Classification != pushBindingClassificationRemoteRefMismatch {
		t.Fatalf("classification = %s", result.Classification)
	}
	if result.RemoteProbe == nil || result.RemoteProbe.State != pushBindingProbeRead || result.RemoteProbe.RemoteOID != "" {
		t.Fatalf("remote_probe = %#v", result.RemoteProbe)
	}
}

func TestPushBindingRejectsExpectedOIDBehindLocalHEAD(t *testing.T) {
	fixture := newPushBindingFixture(t)
	writePushBindingFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "second")
	var output bytes.Buffer
	if err := runPushBinding(fixture.repo, []string{"--expected-oid", fixture.baseOID}, &output); err != nil {
		t.Fatal(err)
	}
	result := decodePushBindingOutput(t, output)
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Reason != "expected_oid_not_current_head" {
		t.Fatalf("result = %#v", result)
	}
	if result.RemoteProbe != nil || (result.Postcondition != nil && result.Postcondition.Met) {
		t.Fatalf("postcondition = %#v", result.Postcondition)
	}
}

func TestPushBindingBlocksWhenTargetCannotBeUniquelyDetermined(t *testing.T) {
	fresh := t.TempDir()
	runFinalizationGit(t, fresh, "init", "-q")
	runFinalizationGit(t, fresh, "config", "user.email", "pushbinding@example.invalid")
	runFinalizationGit(t, fresh, "config", "user.name", "push binding test")
	writePushBindingFile(t, fresh, "fresh.txt", "fresh\n")
	runFinalizationGit(t, fresh, "add", "fresh.txt")
	runFinalizationGit(t, fresh, "commit", "-q", "-m", "initial")
	var output bytes.Buffer
	if err := runPushBinding(fresh, nil, &output); err != nil {
		t.Fatal(err)
	}
	result := decodePushBindingOutput(t, output)
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "target" || result.Failure.Reason != "no_upstream" {
		t.Fatalf("result = %#v", result)
	}

	fixture := newPushBindingFixture(t)
	runFinalizationGit(t, fixture.repo, "checkout", "-q", "--detach")
	output.Reset()
	if err := runPushBinding(fixture.repo, nil, &output); err != nil {
		t.Fatal(err)
	}
	result = decodePushBindingOutput(t, output)
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Reason != "detached_head" {
		t.Fatalf("detached result = %#v", result)
	}
}

func TestPushBindingRejectsInvalidArguments(t *testing.T) {
	fixture := newPushBindingFixture(t)
	cases := [][]string{
		{"--attempt-outcome", "surprise"},
		{"--expected-oid", "abc123"},
		{"positional"},
		{"--expected-oid"},
		{"--expected-oid", fixture.baseOID, "--expected-oid", fixture.baseOID},
	}
	for _, args := range cases {
		var output bytes.Buffer
		if err := runPushBinding(fixture.repo, args, &output); err == nil ||
			!strings.Contains(err.Error(), "usage: glm-parent-action push-binding") || output.Len() != 0 {
			t.Fatalf("args = %v: err = %v output = %q", args, err, output.String())
		}
	}
}

func TestExecuteRoutesPushBindingAction(t *testing.T) {
	fixture := newPushBindingFixture(t)
	cfg := config.AppConfig{RepoRoot: fixture.repo}
	var output bytes.Buffer
	if err := execute(cfg, []string{"push-binding"}, &output, &output); err != nil {
		t.Fatalf("execute: %v: %s", err, output.String())
	}
	result := decodePushBindingOutput(t, output)
	if result.Status != "classified" || result.Classification != pushBindingClassificationSynced {
		t.Fatalf("result = %#v", result)
	}
}

func newPushBindingFixture(t *testing.T) pushBindingFixture {
	t.Helper()
	repo := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	runFinalizationGit(t, repo, "init", "-q")
	runFinalizationGit(t, repo, "config", "user.email", "pushbinding@example.invalid")
	runFinalizationGit(t, repo, "config", "user.name", "push binding test")
	runFinalizationGit(t, remote, "init", "-q", "--bare")
	writePushBindingFile(t, repo, "binding.txt", "base\n")
	runFinalizationGit(t, repo, "add", "binding.txt")
	runFinalizationGit(t, repo, "commit", "-q", "-m", "initial")
	branch := strings.TrimSpace(pushBindingGitOutput(t, repo, "symbolic-ref", "--short", "HEAD"))
	runFinalizationGit(t, repo, "remote", "add", "origin", remote)
	runFinalizationGit(t, repo, "push", "-q", "origin", branch)
	runFinalizationGit(t, repo, "branch", "--set-upstream-to=origin/"+branch, branch)
	return pushBindingFixture{
		repo:    repo,
		remote:  remote,
		branch:  strings.TrimSpace(branch),
		baseOID: strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD")),
	}
}

func writePushBindingFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pushBindingGitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

func decodePushBindingOutput(t *testing.T, buffer bytes.Buffer) pushBindingOutput {
	t.Helper()
	var result pushBindingOutput
	if err := json.Unmarshal(buffer.Bytes(), &result); err != nil {
		t.Fatalf("decode push binding output: %v: %s", err, buffer.String())
	}
	return result
}
