package parentactioncmd

import (
	"encoding/json"
	"io"
	"os/exec"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const publicationFailurePush = "publication_remote_write_failed"

func runPublicationPush(cfg config.AppConfig, stdout io.Writer) error {
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	return json.NewEncoder(stdout).Encode(publishPublicationCandidate(cfg, st))
}

func publishPublicationCandidate(cfg config.AppConfig, st *state.StateStore) pushBindingOutput {
	preflight := buildPushBinding(cfg.RepoRoot, pushBindingOptions{})
	if publicationPushAlreadySynced(preflight) {
		return preflight
	}
	if preflight.Status == publicationPrepareStatusBlocked || preflight.RemoteWrite == nil ||
		preflight.RemoteWrite.Authorization != pushBindingAuthorizationPublication {
		return blockPublicationPush(preflight, "publication remote write is not authorized")
	}
	candidate, err := st.LoadPublicationCandidate()
	if err != nil || preflight.Target == nil || preflight.Target.LocalOID != candidate.CommitOID || preflight.ExpectedOID != candidate.CommitOID {
		return blockPublicationPush(preflight, "publication candidate does not match remote write target")
	}

	refspec := candidate.CommitOID + ":" + preflight.RemoteWrite.RemoteRef
	command := exec.Command("git", "-C", cfg.RepoRoot, "push", "--no-verify", preflight.RemoteWrite.RemoteName, refspec)
	if err := command.Run(); err != nil {
		post := buildPushBinding(cfg.RepoRoot, pushBindingOptions{ExpectedOID: candidate.CommitOID, AttemptOutcome: pushBindingAttemptRejected})
		return blockPublicationPush(post, "publication push failed")
	}
	post := buildPushBinding(cfg.RepoRoot, pushBindingOptions{ExpectedOID: candidate.CommitOID, AttemptOutcome: pushBindingAttemptCompleted})
	if !publicationPushAlreadySynced(post) {
		return blockPublicationPush(post, "publication push postcondition is not satisfied")
	}
	return post
}

func publicationPushAlreadySynced(output pushBindingOutput) bool {
	return output.Status != publicationPrepareStatusBlocked && output.Classification == pushBindingClassificationSynced &&
		output.Postcondition != nil && output.Postcondition.Met
}

func blockPublicationPush(output pushBindingOutput, detail string) pushBindingOutput {
	output.Status = publicationPrepareStatusBlocked
	output.RemoteWrite = nil
	output.Failure = &finalizationFailure{Stage: "publication", Reason: publicationFailurePush, Detail: compactFinalizationDiagnostic(detail)}
	return output
}
