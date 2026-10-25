package app

import (
	"encoding/json"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

type controllerPublicationCommand struct {
	Action                  string                            `json:"action"`
	ExpectedGeneration      uint64                            `json:"expected_generation"`
	SourceOID               string                            `json:"source_oid,omitempty"`
	CandidateID             string                            `json:"candidate_id,omitempty"`
	TransitionID            string                            `json:"transition_id,omitempty"`
	Policy                  controller.PublicationPolicy      `json:"policy"`
	Message                 string                            `json:"message,omitempty"`
	Evidence                []controller.EvidenceObjectRef    `json:"evidence,omitempty"`
	RequiredValidationForms []string                          `json:"required_validation_forms,omitempty"`
	EvidenceRecord          *controller.CandidateEvidence     `json:"evidence_record,omitempty"`
	ArtifactJSON            json.RawMessage                   `json:"artifact_json,omitempty"`
	Amendment               *controller.TaskAmendmentApproval `json:"amendment,omitempty"`
}

func executeControllerPublication(cfg config.AppConfig, store *controller.Store, command controllerPublicationCommand) (any, error) {
	if len(command.ArtifactJSON) != 0 && command.Action != "store-evidence" {
		return nil, fmt.Errorf("candidate evidence artifact is restricted to store-evidence")
	}
	if command.Amendment != nil && command.Action != "rebind" {
		return nil, fmt.Errorf("task amendment approval is restricted to candidate rebind")
	}
	if command.Action == "recover" {
		return store.RecoverExecutionOperation(command.TransitionID)
	}
	head, err := store.LoadHead()
	if err != nil {
		return nil, err
	}
	if head.ControllerGeneration != command.ExpectedGeneration {
		return nil, fmt.Errorf("publication command generation is stale")
	}
	input := controller.PublicationInput{ExpectedGeneration: command.ExpectedGeneration, CandidateID: command.CandidateID, Amendment: command.Amendment}
	return executePublicationAction(cfg, store, command, input)
}

func executePublicationAction(cfg config.AppConfig, store *controller.Store, command controllerPublicationCommand, input controller.PublicationInput) (any, error) {
	switch command.Action {
	case "reopen-unsealed":
		return executeUnsealedCompletionReentry(cfg, store, command)
	case "store-evidence":
		return storePublicationCommandEvidence(store, command.EvidenceRecord, command.ArtifactJSON)
	case "install-completion":
		return executeParentCompletionInstall(cfg, store, input, command)
	case "promote":
		return store.PromoteAcceptedCandidate(input)
	case "publish":
		return store.PublishAcceptedCandidate(input)
	case "rebind":
		return store.RebindUnpublishedCandidate(input)
	case "revalidate":
		return store.RevalidateAcceptedCandidate(input, command.Evidence)
	case "reenter":
		return store.ReenterAcceptedCandidate(input)
	case "adopt-source":
		return store.AdoptRetiredExternalSource(controller.RetiredSourceAdvancementInput{ExpectedGeneration: command.ExpectedGeneration, Policy: command.Policy, SourceOID: command.SourceOID})
	case "adopt":
		return store.AdoptExternalAdvancement(controller.ExternalAdvancementInput{ExpectedGeneration: command.ExpectedGeneration, Policy: command.Policy})
	default:
		return executeMutablePublication(cfg, store, command)
	}
}

func executeMutablePublication(cfg config.AppConfig, store *controller.Store, command controllerPublicationCommand) (any, error) {

	if command.Action != "preview" && command.Action != "accept" && command.Action != "adopt-mutable" && command.Action != "complete" {
		return nil, fmt.Errorf("unsupported controller publication action %q", command.Action)
	}
	admission, err := currentControllerSemanticAdmission(cfg, store)
	if err != nil {
		return nil, err
	}
	switch command.Action {
	case "complete":
		return store.AcceptParentCompletion(admission, controller.CandidateAcceptanceInput{Message: command.Message, Policy: command.Policy})
	case "preview":
		tree, err := store.PreviewCandidateTree(admission, command.Policy)
		return map[string]string{"tree_oid": tree, "base_oid": admission.Head.IntegrationTip, "attempt_id": admission.Attempt.AttemptID, "snapshot_id": admission.Snapshot.ID}, err
	case "accept":
		return store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{Message: command.Message, Policy: command.Policy, Evidence: command.Evidence, RequiredValidationForms: command.RequiredValidationForms})
	default:
		return store.AdoptMutableExternalAdvancement(admission, command.Policy)
	}
}

func storePublicationCommandEvidence(store *controller.Store, record *controller.CandidateEvidence, artifact json.RawMessage) (any, error) {
	if record == nil {
		return nil, fmt.Errorf("candidate evidence record is missing")
	}
	if len(artifact) != 0 {
		return store.StoreCandidateEvidenceJSON(*record, artifact)
	}
	return store.StoreCandidateEvidence(*record)
}
