package taskcontract

import "testing"

func TestParseTaskDependencyStateExplicitFulfilled(t *testing.T) {
	body := "# Task\n\n## Dependencies\n\n- `IMPLEMENTATION_TASKS/current.md`\n\n## Fulfilled dependencies\n\n- `IMPLEMENTATION_TASKS/done.md`\n"
	state, err := ParseTaskDependencyState([]byte(body))
	if err != nil {
		t.Fatalf("ParseTaskDependencyState: %v", err)
	}
	if len(state.Outstanding) != 1 || state.Outstanding[0] != "IMPLEMENTATION_TASKS/current.md" {
		t.Fatalf("outstanding = %#v", state.Outstanding)
	}
	if len(state.Fulfilled) != 1 || state.Fulfilled[0] != "IMPLEMENTATION_TASKS/done.md" {
		t.Fatalf("fulfilled = %#v", state.Fulfilled)
	}
}

func TestParseTaskDependencyStateFulfilledSectionOptional(t *testing.T) {
	body := "# Task\n\n## Dependencies\n\nnone\n"
	state, err := ParseTaskDependencyState([]byte(body))
	if err != nil {
		t.Fatalf("ParseTaskDependencyState: %v", err)
	}
	if len(state.Outstanding) != 0 || len(state.Fulfilled) != 0 {
		t.Fatalf("state = %#v", state)
	}
}

func TestParseTaskDependencyStateRejectsOverlap(t *testing.T) {
	body := "# Task\n\n## Dependencies\n\n- `IMPLEMENTATION_TASKS/same.md`\n\n## Fulfilled dependencies\n\n- `IMPLEMENTATION_TASKS/same.md`\n"
	if _, err := ParseTaskDependencyState([]byte(body)); err == nil {
		t.Fatal("same dependency in outstanding and fulfilled sections was accepted")
	}
}

func TestParseTaskDependencyStateRejectsMalformedFulfilledReference(t *testing.T) {
	body := "# Task\n\n## Dependencies\n\nnone\n\n## Fulfilled dependencies\n\n- `IMPLEMENTATION_TASKS/../escape.md`\n"
	if _, err := ParseTaskDependencyState([]byte(body)); err == nil {
		t.Fatal("malformed fulfilled dependency was accepted")
	}
}
