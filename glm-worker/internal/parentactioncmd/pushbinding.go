package parentactioncmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

type pushBindingOptions struct {
	ExpectedOID    string
	AttemptOutcome string
}

type pushBindingTarget struct {
	LocalOID    string `json:"local_oid"`
	Branch      string `json:"branch,omitempty"`
	RemoteName  string `json:"remote_name"`
	RemoteRef   string `json:"remote_ref"`
	TrackingRef string `json:"tracking_ref,omitempty"`
	TrackingOID string `json:"tracking_oid,omitempty"`
	Ahead       int    `json:"ahead,omitempty"`
	Behind      int    `json:"behind,omitempty"`
}

type pushBindingProbe struct {
	State     string `json:"state"`
	RemoteOID string `json:"remote_oid,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
}

type pushBindingPostcondition struct {
	ExpectedOID string `json:"expected_oid"`
	RemoteOID   string `json:"remote_oid,omitempty"`
	Met         bool   `json:"met"`
}

type pushBindingRemoteWrite struct {
	Authorization string `json:"authorization"`
	Executor      string `json:"executor"`
	RemoteName    string `json:"remote_name"`
	RemoteRef     string `json:"remote_ref"`
	ExpectedOID   string `json:"expected_oid"`
}

type pushBindingOutput struct {
	Status         string                    `json:"status"`
	AttemptOutcome string                    `json:"attempt_outcome"`
	ExpectedOID    string                    `json:"expected_oid"`
	TreeClean      bool                      `json:"tree_clean"`
	Target         *pushBindingTarget        `json:"target,omitempty"`
	RemoteProbe    *pushBindingProbe         `json:"remote_probe,omitempty"`
	Classification string                    `json:"classification,omitempty"`
	Postcondition  *pushBindingPostcondition `json:"postcondition,omitempty"`
	RemoteWrite    *pushBindingRemoteWrite   `json:"remote_write,omitempty"`
	Failure        *finalizationFailure      `json:"failure,omitempty"`
}

type gitUpstream struct {
	RemoteName  string
	RemoteRef   string
	TrackingRef string
	TrackingOID string
}

const (
	pushBindingUsage                           = "usage: glm-parent-action push-binding [--expected-oid <oid>] [--attempt-outcome <none|completed|rejected|network-error|non-fast-forward>]"
	pushBindingAttemptNone                     = "none"
	pushBindingAttemptCompleted                = "completed"
	pushBindingAttemptRejected                 = "rejected"
	pushBindingAttemptNetworkError             = "network-error"
	pushBindingAttemptNonFastForward           = "non-fast-forward"
	pushBindingProbeRead                       = "read"
	pushBindingProbeNetworkError               = "network_error"
	pushBindingClassificationSynced            = "synced"
	pushBindingClassificationLocalAhead        = "remote_sync_pending_local_ahead"
	pushBindingClassificationPushRejected      = "remote_sync_pending_push_rejected"
	pushBindingClassificationNetworkFailure    = "remote_sync_pending_network_failure"
	pushBindingClassificationNonFastForward    = "remote_sync_pending_non_fast_forward"
	pushBindingClassificationRemoteRefMismatch = "remote_sync_pending_remote_ref_mismatch"
	pushBindingAuthorizationRequired           = "user_decision_required"
	pushBindingExecutorParentOnly              = "parent_codex_surface_only"
	pushBindingOIDLength                       = 40
)

var (
	errGitUpstreamMissing  = errors.New("branch upstream missing")
	errGitRemoteURLMissing = errors.New("remote url missing")
)

func runPushBinding(repoRoot string, args []string, stdout io.Writer) error {
	options, err := parsePushBindingOptions(args)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(buildPushBinding(repoRoot, options))
}

func parsePushBindingOptions(args []string) (pushBindingOptions, error) {
	options := pushBindingOptions{AttemptOutcome: pushBindingAttemptNone}
	sawExpected := false
	sawAttempt := false
	for index := 0; index < len(args); index++ {
		if index+1 >= len(args) {
			return options, fmt.Errorf("%s", pushBindingUsage)
		}
		flag := args[index]
		value := args[index+1]
		index++
		switch flag {
		case "--expected-oid":
			if sawExpected || !pushBindingValidOID(strings.ToLower(value)) {
				return options, fmt.Errorf("%s", pushBindingUsage)
			}
			sawExpected = true
			options.ExpectedOID = strings.ToLower(value)
		case "--attempt-outcome":
			if sawAttempt || !pushBindingAttemptOutcomeValid(value) {
				return options, fmt.Errorf("%s", pushBindingUsage)
			}
			sawAttempt = true
			options.AttemptOutcome = value
		default:
			return options, fmt.Errorf("%s", pushBindingUsage)
		}
	}
	return options, nil
}

func buildPushBinding(repoRoot string, options pushBindingOptions) pushBindingOutput {
	output := pushBindingOutput{Status: "classified", AttemptOutcome: options.AttemptOutcome}
	target, failure := pushBindingTargetFromRepo(repoRoot)
	if failure != nil {
		output.Status = "blocked"
		output.Failure = failure
		return output
	}
	output.Target = target
	output.TreeClean = pushBindingTreeClean(repoRoot)
	output.ExpectedOID = target.LocalOID
	if options.ExpectedOID != "" {
		output.ExpectedOID = options.ExpectedOID
	}
	output.RemoteProbe = pushBindingProbeRemote(repoRoot, target.RemoteName, target.RemoteRef)
	output.Classification = pushBindingClassify(repoRoot, output.RemoteProbe, output.ExpectedOID, options.AttemptOutcome)
	output.Postcondition = &pushBindingPostcondition{
		ExpectedOID: output.ExpectedOID,
		RemoteOID:   output.RemoteProbe.RemoteOID,
		Met:         output.RemoteProbe.State == pushBindingProbeRead && output.RemoteProbe.RemoteOID == output.ExpectedOID,
	}
	if output.Classification != pushBindingClassificationSynced {
		output.RemoteWrite = &pushBindingRemoteWrite{
			Authorization: pushBindingAuthorizationRequired,
			Executor:      pushBindingExecutorParentOnly,
			RemoteName:    target.RemoteName,
			RemoteRef:     target.RemoteRef,
			ExpectedOID:   output.ExpectedOID,
		}
	}
	return output
}

func pushBindingTargetFromRepo(repoRoot string) (*pushBindingTarget, *finalizationFailure) {
	localOID, err := gitFinalizationOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil, &finalizationFailure{Stage: "target", Reason: "head_unresolvable"}
	}
	branchOutput, err := gitFinalizationOutput(repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, &finalizationFailure{Stage: "target", Reason: "head_unresolvable"}
	}
	branch := strings.TrimSpace(branchOutput)
	if branch == "HEAD" {
		return nil, &finalizationFailure{Stage: "target", Reason: "detached_head"}
	}
	upstream, err := resolveGitUpstream(repoRoot, branch)
	if err != nil {
		return nil, &finalizationFailure{Stage: "target", Reason: pushBindingUpstreamFailureReason(err)}
	}
	target := &pushBindingTarget{
		LocalOID:    strings.TrimSpace(localOID),
		Branch:      branch,
		RemoteName:  upstream.RemoteName,
		RemoteRef:   upstream.RemoteRef,
		TrackingRef: upstream.TrackingRef,
		TrackingOID: upstream.TrackingOID,
	}
	if upstream.TrackingOID != "" {
		target.Ahead, target.Behind, _ = gitAheadBehind(repoRoot, upstream.TrackingRef)
	}
	return target, nil
}

func pushBindingUpstreamFailureReason(err error) string {
	if errors.Is(err, errGitRemoteURLMissing) {
		return "remote_unresolvable"
	}
	return "no_upstream"
}

func resolveGitUpstream(repoRoot, branch string) (gitUpstream, error) {
	remoteName, err := gitFinalizationOutput(repoRoot, "config", "--get", "branch."+branch+".remote")
	if err != nil {
		return gitUpstream{}, errGitUpstreamMissing
	}
	remoteRef, err := gitFinalizationOutput(repoRoot, "config", "--get", "branch."+branch+".merge")
	if err != nil {
		return gitUpstream{}, errGitUpstreamMissing
	}
	if _, err := gitFinalizationOutput(repoRoot, "config", "--get", "remote."+strings.TrimSpace(remoteName)+".url"); err != nil {
		return gitUpstream{}, errGitRemoteURLMissing
	}
	upstream := gitUpstream{
		RemoteName: strings.TrimSpace(remoteName),
		RemoteRef:  strings.TrimSpace(remoteRef),
	}
	upstream.TrackingRef = gitTrackingRefForRemoteRef(upstream.RemoteName, upstream.RemoteRef)
	if upstream.TrackingRef != "" {
		if trackingOID, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", "-q", upstream.TrackingRef); err == nil {
			upstream.TrackingOID = strings.TrimSpace(trackingOID)
		}
	}
	return upstream, nil
}

func gitTrackingRefForRemoteRef(remoteName, remoteRef string) string {
	if !strings.HasPrefix(remoteRef, "refs/heads/") {
		return ""
	}
	return "refs/remotes/" + remoteName + "/" + strings.TrimPrefix(remoteRef, "refs/heads/")
}

func pushBindingProbeRemote(repoRoot, remoteName, remoteRef string) *pushBindingProbe {
	command := exec.Command("git", "-C", repoRoot, "ls-remote", remoteName, remoteRef)
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		exitCode := 0
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return &pushBindingProbe{State: pushBindingProbeNetworkError, ExitCode: exitCode}
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(fields) == 2 && fields[1] == remoteRef {
			return &pushBindingProbe{State: pushBindingProbeRead, RemoteOID: strings.ToLower(fields[0])}
		}
	}
	return &pushBindingProbe{State: pushBindingProbeRead}
}

func pushBindingClassify(repoRoot string, probe *pushBindingProbe, expectedOID, attemptOutcome string) string {
	if probe.State != pushBindingProbeRead {
		return pushBindingClassificationNetworkFailure
	}
	if probe.RemoteOID == expectedOID {
		return pushBindingClassificationSynced
	}
	if attemptOutcome == pushBindingAttemptRejected {
		return pushBindingClassificationPushRejected
	}
	if attemptOutcome == pushBindingAttemptNetworkError {
		return pushBindingClassificationNetworkFailure
	}
	if probe.RemoteOID == "" {
		return pushBindingClassificationRemoteRefMismatch
	}
	if pushBindingIsAncestor(repoRoot, probe.RemoteOID, expectedOID) {
		return pushBindingClassificationLocalAhead
	}
	return pushBindingClassificationNonFastForward
}

func pushBindingIsAncestor(repoRoot, ancestorOID, descendantOID string) bool {
	if _, err := gitFinalizationOutput(repoRoot, "cat-file", "-e", ancestorOID+"^{commit}"); err != nil {
		return false
	}
	command := exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", ancestorOID, descendantOID)
	return command.Run() == nil
}

func pushBindingTreeClean(repoRoot string) bool {
	output, err := gitFinalizationOutput(repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	return err == nil && strings.TrimSpace(output) == ""
}

func pushBindingValidOID(oid string) bool {
	if len(oid) != pushBindingOIDLength {
		return false
	}
	for _, char := range oid {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func pushBindingAttemptOutcomeValid(value string) bool {
	switch value {
	case pushBindingAttemptNone, pushBindingAttemptCompleted, pushBindingAttemptRejected,
		pushBindingAttemptNetworkError, pushBindingAttemptNonFastForward:
		return true
	}
	return false
}

func gitAheadBehind(repoRoot, trackingRef string) (int, int, error) {
	output, err := gitFinalizationOutput(repoRoot, "rev-list", "--left-right", "--count", trackingRef+"...HEAD")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list count output: %q", strings.TrimSpace(output))
	}
	behind, behindErr := strconv.Atoi(fields[0])
	ahead, aheadErr := strconv.Atoi(fields[1])
	if behindErr != nil || aheadErr != nil {
		return 0, 0, fmt.Errorf("unexpected rev-list count output: %q", strings.TrimSpace(output))
	}
	return ahead, behind, nil
}
