package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controlprovenance"
)

func TestParentActionMachineNegativeResultCannotBePromoted(t *testing.T) {
	st := newParentActionTestStore(t)
	writeParentActionAuthorityFixture(t, st, controlprovenance.ClassificationMachine)
	plan := ParentActionPlan{
		RequiredAction: ParentActionResume,
		AllowedActions: []ParentAction{ParentActionResume},
	}

	admitted, err := st.parentActionAuthorityAdmits(plan, ParentActionAccept)
	if err != nil {
		t.Fatal(err)
	}
	if admitted {
		t.Fatal("machine-negative accept was promoted by parent semantic authority")
	}

	admitted, err = st.parentActionAuthorityAdmits(plan, ParentActionResume)
	if err != nil {
		t.Fatal(err)
	}
	if !admitted {
		t.Fatal("machine-admitted recovery action was rejected")
	}
}

func TestParentActionResidualClassificationPreservesSemanticPromotion(t *testing.T) {
	for _, classification := range []controlprovenance.Classification{
		controlprovenance.ClassificationPartial,
		controlprovenance.ClassificationSemanticParent,
	} {
		t.Run(string(classification), func(t *testing.T) {
			st := newParentActionTestStore(t)
			writeParentActionAuthorityFixture(t, st, classification)
			plan := ParentActionPlan{RequiredAction: ParentActionResume}
			if !plan.AdmitsCommand(ParentActionAccept) {
				t.Fatal("fixture no longer exercises the residual semantic promotion path")
			}

			admitted, err := st.parentActionAuthorityAdmits(plan, ParentActionAccept)
			if err != nil {
				t.Fatal(err)
			}
			if !admitted {
				t.Fatalf("%s classification lost residual parent judgment", classification)
			}
		})
	}
}

func TestParentActionMissingClassificationFailsClosed(t *testing.T) {
	st := newParentActionTestStore(t)
	plan := ParentActionPlan{RequiredAction: ParentActionResume}
	admitted, err := st.parentActionAuthorityAdmits(plan, ParentActionAccept)
	if err != nil {
		t.Fatal(err)
	}
	if admitted {
		t.Fatal("missing canonical classification promoted a negative result")
	}
}

func writeParentActionAuthorityFixture(t *testing.T, st *StateStore, classification controlprovenance.Classification) {
	t.Helper()
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry, err := json.Marshal(controlprovenance.Registry{
		Version: 1,
		Controls: []controlprovenance.Control{{
			ID:                     parentActionAdmissionControlID,
			Classification:         classification,
			Purpose:                "test authority",
			ResidualParentJudgment: "test residual",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, filepath.FromSlash(controlprovenance.RegistryPath)), registry, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("repo-root", repoRoot); err != nil {
		t.Fatal(err)
	}
}
