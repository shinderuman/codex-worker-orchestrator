package controlprovenance

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const RegistryPath = "codex/control-provenance.json"

type Classification = string

const (
	ClassificationMachine            Classification = "machine-enforced"
	ClassificationPartial            Classification = "partial"
	ClassificationProse              Classification = "prose-only"
	ClassificationSemanticParent     Classification = "semantic-parent-only"
	ClassificationExternalUnenforced Classification = "external-unenforceable"
)

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

const (
	NegativeResultMachineRecoveryRequired NegativeResultPolicy = "machine-recovery-required"
	NegativeResultParentResidual          NegativeResultPolicy = "parent-residual"
)

type ResultAuthority struct {
	ControlID      string               `json:"control_id"`
	Classification Classification       `json:"classification"`
	NegativeResult NegativeResultPolicy `json:"negative_result"`
}

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

func Load(repoRoot string) (Registry, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return Registry{}, fmt.Errorf("repository root is unavailable")
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(RegistryPath)))
	if err != nil {
		return Registry{}, fmt.Errorf("read %s: %w", RegistryPath, err)
	}
	return Decode(data)
}

func (r Registry) Lookup(controlID string) (Control, bool) {
	for _, control := range r.Controls {
		if control.ID == controlID {
			return control, true
		}
	}
	return Control{}, false
}

func (r Registry) ResultAuthority(controlID string) (ResultAuthority, error) {
	control, ok := r.Lookup(controlID)
	if !ok {
		return ResultAuthority{}, fmt.Errorf("control %q is not registered", controlID)
	}
	policy, err := NegativeResultPolicyFor(control.Classification)
	if err != nil {
		return ResultAuthority{}, fmt.Errorf("control %q: %w", controlID, err)
	}
	return ResultAuthority{
		ControlID:      control.ID,
		Classification: control.Classification,
		NegativeResult: policy,
	}, nil
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

func (a ResultAuthority) ParentMayPromoteNegativeResult() bool {
	return a.NegativeResult == NegativeResultParentResidual
}
