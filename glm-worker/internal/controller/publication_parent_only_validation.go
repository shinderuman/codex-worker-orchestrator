package controller

import (
	"encoding/json"
	"fmt"
	"reflect"
)

func (s *Store) validateParentOnlyInheritedValidations(admission Admission, forms []FinalValidationRequest, probes []SmokeProbeDeclaration) error {
	attempt := admission.Attempt
	if attempt.ExecutionAuthority != ExecutionAuthorityParentOnly || attempt.PredecessorAttemptID == "" {
		return nil
	}
	previous, err := s.loadAttempt(attempt.PredecessorAttemptID)
	if err != nil {
		return err
	}
	ref, seal, err := s.findAttemptSeal(admission.Head, previous)
	if err != nil {
		return err
	}
	if previous.ExecutionAuthority != ExecutionAuthorityParentOnly || ref.LogicalIdentity != attempt.ResumedFromSealID {
		return fmt.Errorf("parent-only validation predecessor authority differs")
	}
	declaration, err := s.parentOnlySealedValidationDeclaration(seal)
	if err != nil {
		return err
	}
	for _, required := range declaration.Required {
		if !declaredFormCovered(forms, required) {
			return fmt.Errorf("declared validation set drops predecessor validation %s@%s", required.Form, required.WorkingDir)
		}
	}
	for _, required := range declaration.Probes {
		if !parentOnlyProbeCovered(probes, required) {
			return fmt.Errorf("declared validation set drops predecessor smoke probe %s", required.ProbeID)
		}
	}
	return nil
}

func (s *Store) parentOnlySealedValidationDeclaration(seal AttemptSeal) (CandidateValidationObligation, error) {
	for _, ref := range seal.ReviewValidationRefs {
		data, err := s.LoadEvidenceObject(ref)
		if err != nil {
			return CandidateValidationObligation{}, err
		}
		var record CandidateEvidence
		if err := json.Unmarshal(data, &record); err != nil {
			return CandidateValidationObligation{}, err
		}
		if record.Kind != candidateEvidenceKindValidationObligation {
			continue
		}
		data, err = s.LoadEvidenceObject(record.Artifact)
		if err != nil {
			return CandidateValidationObligation{}, err
		}
		var declaration CandidateValidationObligation
		if err := json.Unmarshal(data, &declaration); err != nil {
			return declaration, err
		}
		return declaration, nil
	}
	return CandidateValidationObligation{}, fmt.Errorf("parent-only predecessor validation declaration is missing")
}

func parentOnlyProbeCovered(probes []SmokeProbeDeclaration, required SmokeProbeDeclaration) bool {
	for _, probe := range probes {
		if reflect.DeepEqual(probe, required) {
			return true
		}
	}
	return false
}
