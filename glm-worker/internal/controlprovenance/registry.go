package controlprovenance

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type Classification = string

type Registry struct {
	Version  int       `json:"version"`
	Controls []Control `json:"controls"`
}

type Control struct {
	ID                     string            `json:"id"`
	Classification         Classification    `json:"classification"`
	Purpose                string            `json:"purpose"`
	MachineOwners          []Locator         `json:"machine_owners,omitempty"`
	Tests                  []Locator         `json:"tests,omitempty"`
	Postconditions         []Locator         `json:"postconditions,omitempty"`
	ProjectionGuards       []ProjectionGuard `json:"projection_guards,omitempty"`
	ResidualParentJudgment string            `json:"residual_parent_judgment"`
	Boundary               string            `json:"boundary,omitempty"`
}

type Locator struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
}

type ProjectionGuard struct {
	Path            string   `json:"path"`
	ForbiddenTokens []string `json:"forbidden_tokens"`
}

type NegativeResultPolicy string

const RegistryPath = "codex/control-provenance.json"

const (
	ClassificationMachine            Classification = "machine-enforced"
	ClassificationPartial            Classification = "partial"
	ClassificationProse              Classification = "prose-only"
	ClassificationSemanticParent     Classification = "semantic-parent-only"
	ClassificationExternalUnenforced Classification = "external-unenforceable"
)

const (
	NegativeResultMachineRecoveryRequired NegativeResultPolicy = "machine-recovery-required"
	NegativeResultParentResidual          NegativeResultPolicy = "parent-residual"
)

func Decode(data []byte) (Registry, error) {
	var registry Registry
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return Registry{}, fmt.Errorf("invalid control provenance registry: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Registry{}, fmt.Errorf("invalid control provenance registry: multiple JSON values")
		}
		return Registry{}, fmt.Errorf("invalid control provenance registry: %w", err)
	}
	return registry, nil
}

func NegativeResultPolicyFor(classification Classification) (NegativeResultPolicy, error) {
	switch classification {
	case ClassificationMachine:
		return NegativeResultMachineRecoveryRequired, nil
	case ClassificationPartial, ClassificationProse, ClassificationSemanticParent, ClassificationExternalUnenforced:
		return NegativeResultParentResidual, nil
	default:
		return "", fmt.Errorf("unsupported control classification %q", classification)
	}
}

func ParentMayPromoteNegativeResult(classification Classification, explicitMachineRecovery bool) (bool, error) {
	policy, err := NegativeResultPolicyFor(classification)
	if err != nil {
		return false, err
	}
	if policy == NegativeResultMachineRecoveryRequired {
		return explicitMachineRecovery, nil
	}
	return true, nil
}
