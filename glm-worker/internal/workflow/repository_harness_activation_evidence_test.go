package workflow

import "testing"

func TestPinRepositoryHarnessActivationRecordsTaskEvidence(t *testing.T) {
	tests := []struct {
		name       string
		activate   bool
		wantActive bool
	}{
		{name: "active repository harness", activate: true, wantActive: true},
		{name: "inactive repository harness", activate: false, wantActive: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			if tt.activate {
				trackRepositoryHarnessMarker(t, repoRoot)
			}
			w, _, _, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)

			active, err := w.pinRepositoryHarnessActivation()
			if err != nil || active != tt.wantActive {
				t.Fatalf("pin activation = active:%v err:%v want active:%v", active, err, tt.wantActive)
			}
			taskID, err := st.TaskID()
			if err != nil {
				t.Fatal(err)
			}
			recorded, known, err := st.ReadRepositoryHarnessActivation(taskID)
			if err != nil || !known || recorded != tt.wantActive {
				t.Fatalf("task activation evidence = active:%v known:%v err:%v want active:%v", recorded, known, err, tt.wantActive)
			}
		})
	}
}
