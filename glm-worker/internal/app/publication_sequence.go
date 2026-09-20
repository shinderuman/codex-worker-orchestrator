package app

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type PublicationActionSpec struct {
	Stage     string     `json:"stage"`
	Commands  [][]string `json:"commands,omitempty"`
	Parameter string     `json:"parameter,omitempty"`
	Reason    string     `json:"reason,omitempty"`
}

type PublicationSequenceFailure struct {
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

type PublicationSequence struct {
	Stage      string                      `json:"stage"`
	NextAction *PublicationActionSpec      `json:"next_action,omitempty"`
	Failure    *PublicationSequenceFailure `json:"failure,omitempty"`
}

type publicationUpstreamBinding struct {
	remoteName  string
	remoteRef   string
	trackingRef string
	trackingOID string
	configured  bool
}

type publicationValidationRequirement struct {
	form    string
	missing bool
}

const publicationCommitMessageParameter = "<commit-message>"

func ProjectPublicationSequence(repoRoot string, st *state.StateStore) PublicationSequence {
	switch st.TaskStatus() {
	case state.TaskStatusComplete:
		return PublicationSequence{Stage: "complete"}
	case state.TaskStatusAwaitingParentCompletion:
	default:
		return PublicationSequence{Stage: "inactive"}
	}
	if blocked := publicationSequenceAdmission(repoRoot, st); blocked != nil {
		return *blocked
	}
	candidate, candidateErr := st.LoadPublicationCandidate()
	if candidateErr != nil && !errors.Is(candidateErr, os.ErrNotExist) {
		return publicationSequenceBlocked("publication_candidate_invalid", candidateErr.Error())
	}
	head, resolvable := publicationSequenceHead(repoRoot)
	if !resolvable {
		return publicationSequenceBlocked("publication_head_unavailable", "publication requires a resolvable local branch HEAD")
	}
	if candidateErr != nil {
		return projectPublicationSourceSequence(repoRoot, st, head.Head)
	}
	return projectPublicationCandidateSequence(repoRoot, st, candidate, head)
}

func publicationSequenceAdmission(repoRoot string, st *state.StateStore) *PublicationSequence {
	if repoRoot == "" {
		blocked := publicationSequenceBlocked("publication_repository_state_unavailable", "repository root is unavailable")
		return &blocked
	}
	harnessActive, err := workflow.RepositoryHarnessActive(repoRoot, st)
	if err != nil {
		blocked := publicationSequenceBlocked("publication_repository_harness_inactive", err.Error())
		return &blocked
	}
	if !harnessActive {
		blocked := publicationSequenceBlocked("publication_repository_harness_inactive", "repository harness is not active for publication")
		return &blocked
	}
	if blocked := publicationSequenceGuardAdmission(repoRoot); blocked != nil {
		return blocked
	}
	return publicationSequencePlanAdmission(st)
}

func publicationSequenceGuardAdmission(repoRoot string) *PublicationSequence {
	report, err := workflow.InspectPublicationGuardSetup(repoRoot)
	if err != nil {
		blocked := publicationSequenceBlocked("publication_guard_setup_invalid", err.Error())
		return &blocked
	}
	if len(report.Defects) == 0 {
		return nil
	}
	blocked := publicationSequenceGuardSetupBlocked(repoRoot, report)
	return &blocked
}

func publicationSequencePlanAdmission(st *state.StateStore) *PublicationSequence {
	plan, err := st.ParentActionPlan()
	if err != nil {
		blocked := publicationSequenceBlocked("publication_parent_action_not_ready", err.Error())
		return &blocked
	}
	if plan.Allows(state.ParentActionInstall) || plan.Allows(state.ParentActionComplete) {
		return nil
	}
	blocked := publicationSequenceBlocked("publication_parent_action_not_ready", "required action is "+string(plan.RequiredAction))
	return &blocked
}

func publicationSequenceHead(repoRoot string) (state.GitHeadAuthority, bool) {
	head, err := state.ResolveGitHeadAuthority("git", repoRoot)
	if err != nil || head.Unborn || head.Head == "" || head.Detached || !strings.HasPrefix(head.SymbolicHead, "refs/heads/") {
		return state.GitHeadAuthority{}, false
	}
	return head, true
}

func publicationSequenceGuardSetupBlocked(repoRoot string, report workflow.PublicationGuardSetupReport) PublicationSequence {
	details := make([]string, 0, len(report.Defects))
	for _, defect := range report.Defects {
		details = append(details, defect.Hook+" "+defect.Defect+" at "+defect.Path)
	}
	sequence := publicationSequenceBlocked("publication_guard_setup_invalid", strings.Join(details, "; "))
	if !report.TrackedHooksMode {
		return sequence
	}
	repairs := make([][]string, 0, len(report.Defects)+1)
	tracked := make([]string, 0, len(report.Defects)+1)
	for _, defect := range report.Defects {
		tracked = append(tracked, ".githooks/"+defect.Hook)
		if defect.Defect != workflow.PublicationGuardHookNotExecutable {
			continue
		}
		repairs = append(repairs, []string{"chmod", "+x", defect.Path})
	}
	if len(repairs) == 0 {
		return sequence
	}
	sequence.NextAction = &PublicationActionSpec{
		Stage:    "repair-guard-setup",
		Commands: append(repairs, append([]string{"git", "-C", repoRoot, "add"}, tracked...)),
		Reason:   "tracked publication hooks must be executable before any publication operation is admitted",
	}
	return sequence
}

func projectPublicationSourceSequence(repoRoot string, st *state.StateStore, headOID string) PublicationSequence {
	status, err := publicationGitOutput(repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return publicationSequenceBlocked("publication_git_state_unavailable", err.Error())
	}
	staged, unstaged, untracked := countPublicationStatus(status)
	if staged != 0 || unstaged != 0 || untracked != 0 {
		return publicationPrepareSpec(repoRoot, unstaged != 0 || untracked != 0, "accepted work is not yet a publication candidate")
	}
	if blocked := publicationRecoveryAdmission(repoRoot, st, headOID); blocked != nil {
		return *blocked
	}
	return publicationRecoverSpec()
}

func publicationRecoveryAdmission(repoRoot string, st *state.StateStore, headOID string) *PublicationSequence {
	baseline := st.ReadOr("baseline-head", "")
	if baseline == "" {
		blocked := publicationSequenceBlocked("publication_baseline_unavailable", "accepted task baseline is unavailable for recovery classification")
		return &blocked
	}
	if headOID == baseline {
		blocked := publicationSequenceBlocked("publication_source_empty", "nothing is staged or committed for publication; complete the parent metadata sync first")
		return &blocked
	}
	parent, err := publicationGitOutput(repoRoot, "rev-parse", "--verify", "HEAD^")
	if err != nil || strings.TrimSpace(parent) != baseline {
		blocked := publicationSequenceBlocked("publication_recovery_unavailable", "HEAD is not exactly one commit ahead of the accepted baseline")
		return &blocked
	}
	parents, err := publicationGitOutput(repoRoot, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil || len(strings.Fields(parents)) != 2 {
		blocked := publicationSequenceBlocked("publication_recovery_unavailable", "committed state is not a single non-merge commit on the accepted baseline")
		return &blocked
	}
	return nil
}

func publicationRecoverSpec() PublicationSequence {
	sequence := PublicationSequence{Stage: "recover"}
	sequence.NextAction = &PublicationActionSpec{
		Stage:    "recover",
		Commands: [][]string{{"glm-parent-action", "push-binding", "recover"}},
		Reason:   "committed without a publication candidate; bounded adoption recovery is the only machine-admitted path",
	}
	return sequence
}

func publicationPrepareSpec(repoRoot string, needsStaging bool, reason string) PublicationSequence {
	commands := make([][]string, 0, 2)
	if needsStaging {
		commands = append(commands, []string{"git", "-C", repoRoot, "add", "-A"})
	}
	commands = append(commands, []string{"glm-parent-action", "push-binding", "prepare", "--message", publicationCommitMessageParameter})
	sequence := PublicationSequence{Stage: "prepare"}
	sequence.NextAction = &PublicationActionSpec{
		Stage:     "prepare",
		Commands:  commands,
		Parameter: "commit-message",
		Reason:    reason,
	}
	return sequence
}

func projectPublicationCandidateSequence(repoRoot string, st *state.StateStore, candidate state.PublicationCandidate, head state.GitHeadAuthority) PublicationSequence {
	if head.Head != candidate.CommitOID && head.Head != candidate.BaseHead {
		return publicationSequenceBlocked("publication_head_diverged", "local branch HEAD matches neither the publication candidate nor its base")
	}
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		return publicationSequenceBlocked("publication_git_state_unavailable", err.Error())
	}
	current := state.SnapshotDigest{
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
	}
	if head.Head == candidate.BaseHead && current != candidate.Snapshot {
		sourceMoved := current.IndexDigest != candidate.Snapshot.IndexDigest || current.WorktreeDigest != candidate.Snapshot.WorktreeDigest
		return publicationPrepareSpec(repoRoot, sourceMoved, "publication candidate no longer matches the staged source")
	}
	if head.Head == candidate.CommitOID && publicationPromotedSourceDiverged(repoRoot) {
		sequence := publicationPrepareSpec(repoRoot, true, "promoted candidate source diverged after the ref update")
		sequence.Failure = &PublicationSequenceFailure{Reason: "publication_promotion_source_diverged_after_update"}
		return sequence
	}
	if blocked := publicationCandidateGateSequence(repoRoot, st, candidate, head, current); blocked != nil {
		return *blocked
	}
	return projectPublicationPushSequence(repoRoot, candidate, head.SymbolicHead)
}

func publicationCandidateGateSequence(repoRoot string, st *state.StateStore, candidate state.PublicationCandidate, head state.GitHeadAuthority, current state.SnapshotDigest) *PublicationSequence {
	if missing := publicationValidationFormMissing(st, candidate, current); missing.missing {
		blocked := publicationValidationSequence(missing.form)
		return &blocked
	}
	installed, err := publicationRuntimeInstallSatisfied(repoRoot, st, candidate)
	if err != nil {
		blocked := publicationSequenceBlocked("publication_install_classification_unavailable", err.Error())
		return &blocked
	}
	if !installed {
		blocked := publicationActionSequence("install-candidate", [][]string{{"glm-parent-action", "push-binding", "readiness", "--install-candidate"}}, "publication candidate requires runtime install evidence")
		return &blocked
	}
	if head.Head == candidate.BaseHead {
		blocked := publicationActionSequence("promote", [][]string{{"glm-parent-action", "push-binding", "promote"}}, "publication gates are satisfied for the exact candidate")
		return &blocked
	}
	return nil
}

func publicationValidationSequence(form string) PublicationSequence {
	if form == "" {
		return publicationSequenceBlocked("publication_validation_state_unavailable", "required parent validation form is unreadable")
	}
	return publicationActionSequence("finalize-check", [][]string{{"glm-parent-action", "finalize-check", form}}, "required parent validation is not bound to the publication candidate snapshot")
}

func publicationActionSequence(stage string, commands [][]string, reason string) PublicationSequence {
	sequence := PublicationSequence{Stage: stage}
	sequence.NextAction = &PublicationActionSpec{Stage: stage, Commands: commands, Reason: reason}
	return sequence
}

func projectPublicationPushSequence(repoRoot string, candidate state.PublicationCandidate, symbolicHead string) PublicationSequence {
	branch := strings.TrimPrefix(symbolicHead, "refs/heads/")
	upstream := publicationBranchUpstream(repoRoot, branch)
	if !upstream.configured {
		return publicationSequenceBlocked("publication_upstream_unconfigured", "publication branch has no configured upstream remote")
	}
	if upstream.trackingOID != candidate.CommitOID {
		refspec := symbolicHead + ":" + upstream.remoteRef
		return publicationActionSequence("push", [][]string{{"git", "-C", repoRoot, "push", upstream.remoteName, refspec}}, "promoted candidate is not observed on the configured upstream remote ref "+upstream.remoteRef)
	}
	return publicationActionSequence("complete", [][]string{{"glm-parent-action", "complete"}}, "promoted candidate is observed on the publication remote")
}

func publicationPromotedSourceDiverged(repoRoot string) bool {
	status, err := publicationGitOutput(repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return true
	}
	return strings.TrimSpace(status) != ""
}

func publicationBranchUpstream(repoRoot, branch string) publicationUpstreamBinding {
	binding := publicationUpstreamBinding{}
	if branch == "" {
		return binding
	}
	remoteName, remoteErr := publicationGitOutput(repoRoot, "config", "--get", "branch."+branch+".remote")
	if remoteErr != nil {
		return binding
	}
	remoteRef, refErr := publicationGitOutput(repoRoot, "config", "--get", "branch."+branch+".merge")
	if refErr != nil {
		return binding
	}
	binding.configured = true
	binding.remoteName = strings.TrimSpace(remoteName)
	binding.remoteRef = strings.TrimSpace(remoteRef)
	binding.trackingRef = publicationTrackingRef(binding.remoteName, binding.remoteRef)
	if binding.trackingRef != "" {
		if oid, err := publicationGitOutput(repoRoot, "rev-parse", "--verify", "-q", binding.trackingRef); err == nil {
			binding.trackingOID = strings.TrimSpace(oid)
		}
	}
	return binding
}

func publicationTrackingRef(remoteName, remoteRef string) string {
	if !strings.HasPrefix(remoteRef, "refs/heads/") {
		return ""
	}
	return "refs/remotes/" + remoteName + "/" + strings.TrimPrefix(remoteRef, "refs/heads/")
}

func publicationValidationFormMissing(st *state.StateStore, candidate state.PublicationCandidate, current state.SnapshotDigest) publicationValidationRequirement {
	checkpoint, err := st.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) || (err == nil && checkpoint.ParentValidation == nil) {
		return publicationValidationRequirement{}
	}
	if err != nil {
		return publicationValidationRequirement{missing: true}
	}
	requirement := publicationValidationRequirement{form: checkpoint.ParentValidation.Form, missing: true}
	snapshotIDs := state.PublicationValidationSnapshotIDs(candidate, current)
	satisfied, scanErr := publicationValidationEventSatisfied(st, candidate.TaskID, requirement.form, snapshotIDs)
	if scanErr == nil && satisfied {
		requirement.missing = false
	}
	return requirement
}

func publicationValidationEventSatisfied(st *state.StateStore, taskID, form string, snapshotIDs []string) (bool, error) {
	file, err := os.Open(st.TaskEventLogPath(taskID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			return false, err
		}
		if record.Validation == nil || record.Validation.Form != form || record.Validation.Result != state.ValidationResultPass || record.Validation.ValidationRunID == "" {
			continue
		}
		for _, id := range snapshotIDs {
			if record.Validation.SnapshotID == id {
				return true, nil
			}
		}
	}
	return false, scanner.Err()
}

func publicationRuntimeInstallSatisfied(repoRoot string, st *state.StateStore, candidate state.PublicationCandidate) (bool, error) {
	paths, available, err := taskdiff.ChangedPaths(repoRoot, st)
	if err != nil {
		return false, err
	}
	if !available {
		return false, fmt.Errorf("runtime install task baseline is unavailable")
	}
	runtimePaths := taskdiff.RuntimeChangedPaths(paths)
	if len(runtimePaths) == 0 {
		return true, nil
	}
	digest, err := taskdiff.RuntimeSourceDigest(repoRoot, runtimePaths)
	if err != nil {
		return false, err
	}
	evidence, err := st.LoadRuntimeInstallEvidence()
	if err != nil {
		return false, nil
	}
	return evidence.TaskID == candidate.TaskID &&
		evidence.Head == candidate.CommitOID &&
		evidence.InstalledRevision == candidate.CommitOID &&
		evidence.SourceDigest == digest &&
		evidence.SmokeResult == state.ValidationResultPass, nil
}

func countPublicationStatus(status string) (int, int, int) {
	var staged int
	var unstaged int
	var untracked int
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 3 {
			continue
		}
		x := line[0]
		y := line[1]
		if x == '?' && y == '?' {
			untracked++
			continue
		}
		if x != ' ' {
			staged++
		}
		if y != ' ' {
			unstaged++
		}
	}
	return staged, unstaged, untracked
}

func publicationSequenceBlocked(reason, detail string) PublicationSequence {
	return PublicationSequence{
		Stage:   "blocked",
		Failure: &PublicationSequenceFailure{Reason: reason, Detail: compactPublicationSequenceDetail(detail)},
	}
}

func compactPublicationSequenceDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if len(detail) > 1024 {
		return detail[len(detail)-1024:]
	}
	return detail
}

func publicationGitOutput(repoRoot string, args ...string) (string, error) {
	output, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(output), nil
}
