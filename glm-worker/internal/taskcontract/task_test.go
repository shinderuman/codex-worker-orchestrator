package taskcontract

import "testing"

func TestValidateActiveTaskPathContract(t *testing.T) {
	valid := []string{
		"IMPLEMENTATION_TASKS/x.md",
		"IMPLEMENTATION_TASKS/sub/deep/task.md",
		"IMPLEMENTATION_TASKS/.hidden/task.md",
		"IMPLEMENTATION_TASKS/.md",
		"IMPLEMENTATION_TASKS/..md",
	}
	invalid := []string{
		"tasks/task.md",
		"implementation_tasks/task.md",
		"IMPLEMENTATION_TASKS/task.txt",
		"IMPLEMENTATION_TASKS/",
		"IMPLEMENTATION_TASKS//task.md",
		"IMPLEMENTATION_TASKS/./task.md",
		"IMPLEMENTATION_TASKS/../task.md",
		`IMPLEMENTATION_TASKS\task.md`,
	}
	for _, path := range valid {
		if err := ValidateActiveTaskPath(path); err != nil {
			t.Errorf("ValidateActiveTaskPath(%q) = %v", path, err)
		}
	}
	for _, path := range invalid {
		if err := ValidateActiveTaskPath(path); err == nil {
			t.Errorf("ValidateActiveTaskPath(%q) accepted invalid path", path)
		}
	}
}
