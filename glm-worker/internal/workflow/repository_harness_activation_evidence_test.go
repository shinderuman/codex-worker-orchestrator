package workflow

import "testing"

func TestPinRepositoryHarnessActivationRecordsTaskEvidence(t *testing.T) {
	repoRoot := initMutationRepo(t)
	trackRepositoryHarnessMarker(t, repoRoot)
	w, _, _, st := newPlanFileWorkflow(t, repoRoot, nil, "", 0, nil)

	active, err := w.pinRepositoryHarnessActivation()
	if err != nil || !active {
		t.Fatalf("pin activation = active:%v err:%v", active, err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	recorded, known, err := st.ReadRepositoryHarnessActivation(taskID)
	if err != nil || !known || !recorded {
		t.Fatalf("task activation evidence = active:%v known:%v err:%v", recorded, known, err)
	}
}
