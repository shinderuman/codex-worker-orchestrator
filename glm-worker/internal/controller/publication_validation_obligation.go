package controller

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type CandidateValidationObligation struct {
	Required    []FinalValidationRequest `json:"required"`
	Probes      []SmokeProbeDeclaration  `json:"probes,omitempty"`
	NotRequired bool                     `json:"not_required"`
}

type CandidateInstallObligation struct {
	Required bool `json:"required"`
}

const (
	candidateEvidenceKindValidationObligation = "validation-obligation"
	candidateEvidenceKindInstallObligation    = "install-obligation"
	candidateEvidenceDeclared                 = "declared"
)

func (s *Store) validateCandidateInstallObligation(candidate AcceptedCandidate, ref EvidenceObjectRef) error {
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return err
	}
	var obligation CandidateInstallObligation
	if err := json.Unmarshal(data, &obligation); err != nil {
		return err
	}
	if !obligation.Required || !candidate.Policy.RequireInstall {
		return fmt.Errorf("candidate install obligation does not match its required publication policy")
	}
	return nil
}

func (s *Store) validateCandidateValidationObligation(candidate AcceptedCandidate, ref EvidenceObjectRef) error {
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return err
	}
	var declaration CandidateValidationObligation
	if err := json.Unmarshal(data, &declaration); err != nil {
		return err
	}
	if declaration.NotRequired != (len(candidate.RequiredValidations) == 0 && len(candidate.RequiredSmokeProbes) == 0) {
		return fmt.Errorf("candidate validation obligation has no explicit requirement disposition")
	}
	if !reflect.DeepEqual(declaration.Required, candidate.RequiredValidations) {
		return fmt.Errorf("candidate validation obligation differs from the Runtime declaration")
	}
	if !reflect.DeepEqual(declaration.Probes, candidate.RequiredSmokeProbes) {
		return fmt.Errorf("candidate validation obligation differs from the declared post-install smoke probes")
	}
	return nil
}
