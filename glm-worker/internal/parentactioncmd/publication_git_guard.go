package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type publicationGuardOutput struct {
	Status       string `json:"status"`
	CandidateOID string `json:"candidate_oid,omitempty"`
	Ref          string `json:"ref,omitempty"`
}

const (
	publicationGuardAllowed = "allowed"
	publicationZeroOID      = "0000000000000000000000000000000000000000"
)

func runPublicationRefGuard(cfg config.AppConfig, args []string, stdout io.Writer) error {
	oldOID, newOID, ref, err := parsePublicationRefGuardArgs(args)
	if err != nil {
		return err
	}
	if err := verifyPublicationRefUpdate(cfg, oldOID, newOID, ref); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(publicationGuardOutput{Status: publicationGuardAllowed, CandidateOID: newOID, Ref: ref})
}

func parsePublicationRefGuardArgs(args []string) (string, string, string, error) {
	if len(args) != 7 || args[0] != publicationRefGuardSubcommand || args[1] != "--old" || args[3] != "--new" || args[5] != "--ref" {
		return "", "", "", fmt.Errorf("usage: glm-parent-action push-binding ref-guard --old <oid> --new <oid> --ref <ref>")
	}
	if !pushBindingValidOID(args[2]) || !pushBindingValidOID(args[4]) || strings.TrimSpace(args[6]) == "" {
		return "", "", "", fmt.Errorf("invalid publication ref update")
	}
	return args[2], args[4], args[6], nil
}

func verifyPublicationRefUpdate(cfg config.AppConfig, oldOID, newOID, ref string) error {
	if !strings.HasPrefix(ref, "refs/heads/") {
		return nil
	}
	active, err := publicationGitGuardActive(cfg)
	if err != nil || !active {
		return err
	}
	st := state.AttachStateStore(cfg)
	if !publicationRefGuardRequired(st.TaskStatus()) {
		return nil
	}
	candidate, err := loadPublicationGuardCandidate(st, "ref update")
	if err != nil {
		return err
	}
	if publicationExactPromotionRollback(candidate, oldOID, newOID) {
		return verifyPublicationRefRollback(cfg, candidate, oldOID, ref)
	}
	return verifyPublicationRefCandidate(cfg, st, candidate, oldOID, newOID, ref)
}

func publicationExactPromotionRollback(candidate state.PublicationCandidate, oldOID, newOID string) bool {
	return oldOID == candidate.CommitOID && newOID == candidate.BaseHead
}

func verifyPublicationRefRollback(cfg config.AppConfig, candidate state.PublicationCandidate, oldOID, ref string) error {
	head, err := state.ResolveGitHeadAuthority("git", cfg.RepoRoot)
	if err != nil || head.SymbolicHead != ref || head.Head != oldOID {
		return fmt.Errorf("publication ref rollback rejected: current branch identity changed")
	}
	if failure := publicationPromotionPostcondition(cfg.RepoRoot, candidate); failure == nil {
		return fmt.Errorf("publication ref rollback rejected: promoted candidate remains valid")
	}
	return nil
}

func verifyPublicationRefCandidate(cfg config.AppConfig, st *state.StateStore, candidate state.PublicationCandidate, oldOID, newOID, ref string) error {
	if oldOID != candidate.BaseHead || newOID != candidate.CommitOID {
		return fmt.Errorf("publication ref update rejected: only exact candidate promotion is admitted")
	}
	head, err := state.ResolveGitHeadAuthority("git", cfg.RepoRoot)
	if err != nil || head.SymbolicHead != ref || head.Head != oldOID {
		return fmt.Errorf("publication ref update rejected: current branch identity changed")
	}
	if readiness := projectPublicationReadiness(cfg, st); readiness.Status != publicationReadinessReady {
		return fmt.Errorf("publication ref update rejected: %s", publicationFailureDetail(readiness.Failure))
	}
	if failure := verifyPublicationCandidateCommit(cfg.RepoRoot, candidate); failure != nil {
		return fmt.Errorf("publication ref update rejected: %s", publicationFailureDetail(failure))
	}
	return nil
}

func runPublicationPushGuard(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) == 3 && args[0] == publicationPushGuardSubcommand && args[1] == publicationPreToolUseFlag {
		return runPublicationPreToolUseChecked(args[2], stdout)
	}
	remoteName, localRef, localOID, remoteRef, remoteOID, err := parsePublicationPushGuardArgs(args)
	if err != nil {
		return err
	}
	if err := verifyPublicationPush(cfg, remoteName, localRef, localOID, remoteRef, remoteOID); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(publicationGuardOutput{Status: publicationGuardAllowed, CandidateOID: localOID, Ref: remoteRef})
}

func parsePublicationPushGuardArgs(args []string) (string, string, string, string, string, error) {
	if !publicationPushGuardSyntaxValid(args) {
		return "", "", "", "", "", fmt.Errorf("usage: glm-parent-action push-binding push-guard --pre-tool-use <hook-json> | push-guard --remote-name <name> --local-ref <ref> --local-oid <oid> --remote-ref <ref> --remote-oid <oid>")
	}
	if !publicationPushGuardValuesValid(args) {
		return "", "", "", "", "", fmt.Errorf("invalid publication push update")
	}
	return args[2], args[4], args[6], args[8], args[10], nil
}

func publicationPushGuardSyntaxValid(args []string) bool {
	return len(args) == 11 && args[0] == publicationPushGuardSubcommand && args[1] == "--remote-name" &&
		args[3] == "--local-ref" && args[5] == "--local-oid" && args[7] == "--remote-ref" && args[9] == "--remote-oid"
}

func publicationPushGuardValuesValid(args []string) bool {
	return strings.TrimSpace(args[2]) != "" && strings.TrimSpace(args[4]) != "" && pushBindingValidOID(args[6]) &&
		strings.TrimSpace(args[8]) != "" && pushBindingValidOID(args[10])
}

func verifyPublicationPush(cfg config.AppConfig, remoteName, localRef, localOID, remoteRef, remoteOID string) error {
	if !strings.HasPrefix(remoteRef, "refs/heads/") {
		return nil
	}
	active, err := publicationGitGuardActive(cfg)
	if err != nil || !active {
		return err
	}
	st := state.AttachStateStore(cfg)
	if !publicationRefGuardRequired(st.TaskStatus()) {
		return nil
	}
	candidate, err := loadPublicationGuardCandidate(st, "push")
	if err != nil {
		return err
	}
	if err := verifyPublicationPushTarget(cfg, candidate, remoteName, localRef, localOID, remoteRef); err != nil {
		return err
	}
	return verifyPublicationPushBinding(cfg, candidate, remoteOID)
}

func loadPublicationGuardCandidate(st *state.StateStore, operation string) (state.PublicationCandidate, error) {
	candidate, err := st.LoadPublicationCandidate()
	if err != nil {
		return state.PublicationCandidate{}, fmt.Errorf("publication %s rejected: candidate missing: %w", operation, err)
	}
	return candidate, nil
}

func verifyPublicationPushTarget(cfg config.AppConfig, candidate state.PublicationCandidate, remoteName, localRef, localOID, remoteRef string) error {
	if localOID != candidate.CommitOID || !strings.HasPrefix(localRef, "refs/heads/") {
		return fmt.Errorf("publication push rejected: local object is not the exact candidate")
	}
	target, failure := pushBindingTargetFromRepo(cfg.RepoRoot)
	if failure != nil {
		return fmt.Errorf("publication push rejected: %s", publicationFailureDetail(failure))
	}
	if target.LocalOID != candidate.CommitOID || target.RemoteName != remoteName || target.RemoteRef != remoteRef {
		return fmt.Errorf("publication push rejected: remote target does not match candidate binding")
	}
	return nil
}

func verifyPublicationPushBinding(cfg config.AppConfig, candidate state.PublicationCandidate, remoteOID string) error {
	binding := buildPushBinding(cfg.RepoRoot, pushBindingOptions{ExpectedOID: candidate.CommitOID})
	if binding.Status == publicationPrepareStatusBlocked || binding.Failure != nil {
		return fmt.Errorf("publication push rejected: %s", publicationFailureDetail(binding.Failure))
	}
	if binding.Classification == pushBindingClassificationSynced {
		return nil
	}
	if binding.RemoteWrite == nil || binding.RemoteWrite.Authorization != pushBindingAuthorizationPublication {
		return fmt.Errorf("publication push rejected: remote write is not authorized")
	}
	return verifyPublicationPushRemoteOID(binding, remoteOID)
}

func verifyPublicationPushRemoteOID(binding pushBindingOutput, remoteOID string) error {
	observedRemoteOID := remoteOID
	if observedRemoteOID == publicationZeroOID {
		observedRemoteOID = ""
	}
	if binding.RemoteProbe == nil || binding.RemoteProbe.RemoteOID != observedRemoteOID {
		return fmt.Errorf("publication push rejected: remote OID changed from guarded binding")
	}
	return nil
}

func publicationGitGuardActive(cfg config.AppConfig) (bool, error) {
	return workflow.RepositoryHarnessActive(cfg.RepoRoot, state.AttachStateStore(cfg))
}

func publicationRefGuardRequired(status state.TaskStatus) bool {
	return status == state.TaskStatusAwaitingParentCompletion || status == state.TaskStatusComplete
}

func publicationFailureDetail(failure *finalizationFailure) string {
	if failure == nil {
		return "publication authority is unavailable"
	}
	if failure.Detail != "" {
		return failure.Reason + ": " + failure.Detail
	}
	return failure.Reason
}
