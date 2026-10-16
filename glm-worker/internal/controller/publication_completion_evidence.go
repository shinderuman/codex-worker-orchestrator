package controller

import (
	"fmt"
	"reflect"
)

type CompletionAcknowledgement struct {
	SchemaVersion   int                      `json:"schema_version"`
	OperationDigest string                   `json:"operation_digest"`
	ReadinessRef    EvidenceObjectRef        `json:"readiness_ref"`
	ObservationRef  EvidenceObjectRef        `json:"observation_ref"`
	Input           EvidencePublicationInput `json:"input"`
	EvidenceHead    EvidenceObjectRef        `json:"evidence_head"`
	EvidenceLedger  EvidenceObjectRef        `json:"evidence_ledger"`
	Sequence        uint64                   `json:"sequence"`
}

func (s *Store) prepareCompletionAcknowledgement(op ExecutionOperation, phase *TransitionState) (CompletionAcknowledgement, error) {
	if phase.Completion.AcknowledgementRef != nil {
		return s.loadCompletionAcknowledgement(op, *phase)
	}
	receipts, err := s.completionReceiptRefs(op, *phase)
	if err != nil {
		return CompletionAcknowledgement{}, err
	}
	input, err := s.completionReceiptInput(op, receipts)
	if err != nil {
		return CompletionAcknowledgement{}, err
	}
	publication, err := s.prepareEvidencePublication(op.Source.Head, op.Transition, input)
	if err != nil {
		return CompletionAcknowledgement{}, err
	}
	ack := CompletionAcknowledgement{SchemaVersion: controllerSchemaVersion, OperationDigest: op.Transition.OperationDigest, ReadinessRef: *phase.Completion.ReadinessRef, ObservationRef: *phase.Completion.ObservationRef, Input: input, EvidenceHead: publication.EvidenceHeadRef, EvidenceLedger: publication.LedgerRecordRef, Sequence: publication.LedgerRecord.Sequence}
	ref, err := s.putEvidenceJSON("completion-acknowledgement", op.Transition.TransitionID, ack)
	if err != nil {
		return ack, err
	}
	phase.Completion.AcknowledgementRef = &ref
	if err := s.writeTransitionState(*phase); err != nil {
		return ack, err
	}
	return s.loadCompletionAcknowledgement(op, *phase)
}

func (s *Store) completionReceiptRefs(op ExecutionOperation, phase TransitionState) ([]EvidenceObjectRef, error) {
	ready, err := s.loadCompletionReadiness(op, phase)
	if err != nil {
		return nil, err
	}
	observation, err := s.completionObservation(op, phase)
	if err != nil {
		return nil, err
	}
	refs := []EvidenceObjectRef{*phase.Completion.ReadinessRef, *phase.Completion.ObservationRef, observation.GitArchive}
	if ready.LintRef != nil {
		refs = append(refs, *ready.LintRef)
	}
	return canonicalEvidenceRefs(refs), nil
}

func (s *Store) completionReceiptInput(op ExecutionOperation, receipts []EvidenceObjectRef) (EvidencePublicationInput, error) {
	input := op.Evidence
	final, err := s.LoadAttemptFinalization(input.FinalizationRefs[0])
	if err != nil {
		return input, err
	}
	finalRef, _, err := s.StoreAttemptFinalization(completionFinalizationRecord(final, receipts))
	if err != nil {
		return input, err
	}
	input.FinalizationRefs = []EvidenceObjectRef{finalRef}
	task, err := s.LoadTaskIndexRevision(op.Evidence.TaskRevisionRefs[0])
	if err != nil {
		return input, err
	}
	taskRef, _, err := s.StoreTaskIndexRevision(completionTaskRecord(task, op.Evidence.FinalizationRefs, input.FinalizationRefs, receipts))
	if err != nil {
		return input, err
	}
	input.TaskRevisionRefs = []EvidenceObjectRef{taskRef}
	if len(op.Evidence.EpisodeRevisionRefs) != 0 {
		episode, err := s.LoadEpisodeIndexRevision(op.Evidence.EpisodeRevisionRefs[0])
		if err != nil {
			return input, err
		}
		ref, _, err := s.StoreEpisodeIndexRevision(completionEpisodeRecord(episode, op.Evidence, input, receipts))
		if err != nil {
			return input, err
		}
		input.EpisodeRevisionRefs = []EvidenceObjectRef{ref}
	}
	return input, nil
}

func completionFinalizationRecord(record AttemptFinalizationRecord, receipts []EvidenceObjectRef) AttemptFinalizationRecord {
	record.FinalizationID = ""
	record.EvidenceRefs = canonicalEvidenceRefs(append(record.EvidenceRefs, receipts...))
	return record
}

func completionTaskRecord(record TaskIndexRevision, before, after, receipts []EvidenceObjectRef) TaskIndexRevision {
	record.RevisionID = ""
	record.Finalizations = replaceCompletionRefs(record.Finalizations, before, after)
	record.PublicationLineageRecords = canonicalEvidenceRefs(append(record.PublicationLineageRecords, receipts...))
	return record
}

func completionEpisodeRecord(record EpisodeIndexRevision, before, after EvidencePublicationInput, receipts []EvidenceObjectRef) EpisodeIndexRevision {
	record.RevisionID = ""
	record.Finalizations = replaceCompletionRefs(record.Finalizations, before.FinalizationRefs, after.FinalizationRefs)
	record.IntegrationHistory = canonicalEvidenceRefs(append(record.IntegrationHistory, receipts...))
	for index, head := range record.TaskIndexHeads {
		replacement := replaceCompletionRefs([]EvidenceObjectRef{head.RevisionRef}, before.TaskRevisionRefs, after.TaskRevisionRefs)
		record.TaskIndexHeads[index].RevisionRef = replacement[0]
	}
	return record
}

func replaceCompletionRefs(refs, before, after []EvidenceObjectRef) []EvidenceObjectRef {
	replacements := make(map[string]EvidenceObjectRef, len(before))
	for index, ref := range before {
		replacements[evidenceRefKey(ref)] = after[index]
	}
	result := append([]EvidenceObjectRef(nil), refs...)
	for index, ref := range result {
		if replacement, ok := replacements[evidenceRefKey(ref)]; ok {
			result[index] = replacement
		}
	}
	return canonicalEvidenceRefs(result)
}

func validateCompletionEvidenceShape(op ExecutionOperation, input EvidencePublicationInput) error {
	episodes := 0
	if op.Terminal.Episode != nil {
		episodes = 1
	}
	if len(input.FinalizationRefs) != 1 || len(input.TaskRevisionRefs) != 1 || len(input.EpisodeRevisionRefs) != episodes {
		return fmt.Errorf("completion evidence plan has unexpected subjects")
	}
	if len(input.AttemptSeals)+len(input.Finalizations)+len(input.TaskRevisions)+len(input.EpisodeRevisions)+len(input.AttemptSealRefs)+len(input.FindingRecordRefs) != 0 {
		return fmt.Errorf("completion evidence plan contains unplanned records")
	}
	return nil
}

func (s *Store) loadCompletionAcknowledgement(op ExecutionOperation, phase TransitionState) (CompletionAcknowledgement, error) {
	var ack CompletionAcknowledgement
	if phase.Completion == nil || phase.Completion.AcknowledgementRef == nil {
		return ack, fmt.Errorf("completion acknowledgement evidence is missing")
	}
	ref := *phase.Completion.AcknowledgementRef
	if err := validateTypedEvidenceRef(ref, "completion-acknowledgement"); err != nil {
		return ack, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return ack, err
	}
	if err := decodeControllerJSON(data, &ack); err != nil {
		return ack, err
	}
	if err := validateCompletionAcknowledgementBinding(op, phase, ref, ack); err != nil {
		return ack, err
	}
	receipts, err := s.completionReceiptRefs(op, phase)
	if err != nil {
		return ack, err
	}
	if err := s.validateCompletionReceiptInput(op, ack.Input, receipts); err != nil {
		return ack, err
	}
	return ack, s.validateCompletionAcknowledgementGraph(op, ack)
}

func validateCompletionAcknowledgementBinding(op ExecutionOperation, phase TransitionState, ref EvidenceObjectRef, ack CompletionAcknowledgement) error {
	if ack.SchemaVersion != controllerSchemaVersion || ref.LogicalIdentity != op.Transition.TransitionID || ack.OperationDigest != op.Transition.OperationDigest || phase.Completion.ReadinessRef == nil || phase.Completion.ObservationRef == nil {
		return fmt.Errorf("completion acknowledgement journal binding differs")
	}
	if !evidenceRefsEqual(ack.ReadinessRef, *phase.Completion.ReadinessRef) || !evidenceRefsEqual(ack.ObservationRef, *phase.Completion.ObservationRef) {
		return fmt.Errorf("completion acknowledgement receipt binding differs")
	}
	return nil
}

func (s *Store) validateCompletionReceiptInput(op ExecutionOperation, input EvidencePublicationInput, receipts []EvidenceObjectRef) error {
	if err := validateCompletionEvidenceShape(op, input); err != nil {
		return err
	}
	expected, err := s.LoadAttemptFinalization(op.Evidence.FinalizationRefs[0])
	if err != nil {
		return err
	}
	actual, err := s.LoadAttemptFinalization(input.FinalizationRefs[0])
	if err != nil {
		return err
	}
	actual.FinalizationID = ""
	if !reflect.DeepEqual(actual, completionFinalizationRecord(expected, receipts)) {
		return fmt.Errorf("completion finalization differs from the frozen plan and receipts")
	}
	return s.validateCompletionTaskReceipt(op, input, receipts)
}

func (s *Store) validateCompletionTaskReceipt(op ExecutionOperation, input EvidencePublicationInput, receipts []EvidenceObjectRef) error {
	expected, err := s.LoadTaskIndexRevision(op.Evidence.TaskRevisionRefs[0])
	if err != nil {
		return err
	}
	actual, err := s.LoadTaskIndexRevision(input.TaskRevisionRefs[0])
	if err != nil {
		return err
	}
	actual.RevisionID = ""
	if !reflect.DeepEqual(actual, completionTaskRecord(expected, op.Evidence.FinalizationRefs, input.FinalizationRefs, receipts)) {
		return fmt.Errorf("completion Task index differs from the frozen plan and receipts")
	}
	if len(op.Evidence.EpisodeRevisionRefs) == 0 {
		return nil
	}
	expectedEpisode, err := s.LoadEpisodeIndexRevision(op.Evidence.EpisodeRevisionRefs[0])
	if err != nil {
		return err
	}
	actualEpisode, err := s.LoadEpisodeIndexRevision(input.EpisodeRevisionRefs[0])
	if err != nil {
		return err
	}
	actualEpisode.RevisionID = ""
	if !reflect.DeepEqual(actualEpisode, completionEpisodeRecord(expectedEpisode, op.Evidence, input, receipts)) {
		return fmt.Errorf("completion episode index differs from the frozen plan and receipts")
	}
	return nil
}

func (s *Store) validateCompletionAcknowledgementGraph(op ExecutionOperation, ack CompletionAcknowledgement) error {
	if _, err := s.validateEvidenceGraphRoots(ack.EvidenceLedger, ack.EvidenceHead, ack.Sequence); err != nil {
		return err
	}
	ledger, err := s.LoadEvidenceLedgerRecord(ack.EvidenceLedger)
	if err != nil {
		return err
	}
	expected, err := s.buildEvidenceLedgerRecord(op.Source.Head.EvidenceLedgerHeadRef, op.Source.Head, op.Transition, ack.EvidenceHead, ack.Input)
	if err != nil {
		return err
	}
	expected, err = canonicalEvidenceLedgerRecord(expected)
	if err != nil {
		return err
	}
	ledger.RecordDigest = ""
	if !reflect.DeepEqual(ledger, expected) || ack.Sequence != expected.Sequence {
		return fmt.Errorf("completion acknowledgement ledger differs from the journal")
	}
	plan, err := s.planEvidencePublication(op.Source.Head, op.Transition, ack.Input)
	if err != nil {
		return err
	}
	head, err := s.LoadEvidenceHead(ack.EvidenceHead)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(head.PreviousHead, plan.previousHeadRef) || !reflect.DeepEqual(head.TaskHeads, canonicalEvidenceHeads(plan.taskHeads)) || !reflect.DeepEqual(head.EpisodeHeads, canonicalEvidenceHeads(plan.episodeHeads)) {
		return fmt.Errorf("completion acknowledgement head differs from the journal")
	}
	return nil
}
