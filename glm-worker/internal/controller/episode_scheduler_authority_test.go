package controller

import (
	"strings"
	"testing"
)

func TestEpisodeSchedulerRejectsBlockedAndMissingScheduleMembers(t *testing.T) {
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	revision := BlockerEpisodeRevision{
		ProjectSnapshotID: "project",
		ScopeRootTaskRef:  b,
		AdmittedClosure:   []SemanticTaskRef{b},
		AdmittedOrder:     []SemanticTaskRef{b},
	}
	authority := episodeScheduleAuthority{
		project: ProjectSnapshot{SnapshotID: "project", Tasks: []SemanticTaskRef{b}, Blocked: []string{b.TaskPath}},
		refs:    map[string]SemanticTaskRef{b.TaskPath: b},
		dependencies: map[string][]string{
			b.TaskPath: nil,
		},
	}
	blocked := scheduleEpisodeWithAuthority(revision, authority)
	if blocked.Intent != FindingIntentNoRunnable || !strings.Contains(blocked.Reason, "BLOCKED") {
		t.Fatalf("blocked schedule = %#v", blocked)
	}

	authority.project.Blocked = nil
	missing := scheduleEpisodeWithAuthority(revision, authority)
	if missing.Intent != FindingIntentNoRunnable || !strings.Contains(missing.Reason, "outside executable NEXT") {
		t.Fatalf("missing schedule = %#v", missing)
	}
}

func TestEpisodeSchedulerUsesCanonicalNextOrderWithoutGlobalEscape(t *testing.T) {
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	c := testSemanticRef("IMPLEMENTATION_TASKS/c.md", "c")
	x := testSemanticRef("IMPLEMENTATION_TASKS/x.md", "x")
	revision := BlockerEpisodeRevision{
		ProjectSnapshotID: "project",
		ScopeRootTaskRef:  b,
		AdmittedClosure:   []SemanticTaskRef{b, c},
		AdmittedOrder:     []SemanticTaskRef{b, c},
	}
	authority := episodeScheduleAuthority{
		project: ProjectSnapshot{
			SnapshotID: "project",
			Tasks:      []SemanticTaskRef{b, c, x},
			Next:       []string{x.TaskPath, c.TaskPath, b.TaskPath},
		},
		refs: map[string]SemanticTaskRef{b.TaskPath: b, c.TaskPath: c, x.TaskPath: x},
		dependencies: map[string][]string{
			b.TaskPath: nil,
			c.TaskPath: nil,
			x.TaskPath: nil,
		},
	}
	result := scheduleEpisodeWithAuthority(revision, authority)
	if result.Intent != FindingIntentStartBlockerTask || result.NextTaskRef == nil || !result.NextTaskRef.Equal(c) {
		t.Fatalf("canonical NEXT schedule = %#v", result)
	}
}

func TestEpisodeSchedulerPrioritizesReadySuspendedTask(t *testing.T) {
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	c := testSemanticRef("IMPLEMENTATION_TASKS/c.md", "c")
	revision := BlockerEpisodeRevision{
		ProjectSnapshotID: "project",
		ScopeRootTaskRef:  b,
		AdmittedClosure:   []SemanticTaskRef{b, c},
		AdmittedOrder:     []SemanticTaskRef{c, b},
		ExecutionHistory:  []SemanticTaskRef{b},
	}
	authority := episodeScheduleAuthority{
		project: ProjectSnapshot{
			SnapshotID: "project",
			Tasks:      []SemanticTaskRef{b, c},
			Next:       []string{c.TaskPath, b.TaskPath},
		},
		refs: map[string]SemanticTaskRef{b.TaskPath: b, c.TaskPath: c},
		dependencies: map[string][]string{
			b.TaskPath: nil,
			c.TaskPath: nil,
		},
	}
	result := scheduleEpisodeWithAuthority(revision, authority)
	if result.Intent != FindingIntentResumeBlockerTask || result.NextTaskRef == nil || !result.NextTaskRef.Equal(b) {
		t.Fatalf("resume priority schedule = %#v", result)
	}
}

func TestEpisodeSchedulerRequiresCanonicalOutstandingDependenciesFulfilled(t *testing.T) {
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	c := testSemanticRef("IMPLEMENTATION_TASKS/c.md", "c")
	revision := BlockerEpisodeRevision{
		ProjectSnapshotID: "project",
		ScopeRootTaskRef:  b,
		AdmittedClosure:   []SemanticTaskRef{b, c},
		AdmittedOrder:     []SemanticTaskRef{c, b},
	}
	authority := episodeScheduleAuthority{
		project: ProjectSnapshot{
			SnapshotID: "project",
			Tasks:      []SemanticTaskRef{b, c},
			Next:       []string{b.TaskPath, c.TaskPath},
		},
		refs: map[string]SemanticTaskRef{b.TaskPath: b, c.TaskPath: c},
		dependencies: map[string][]string{
			b.TaskPath: {c.TaskPath},
			c.TaskPath: nil,
		},
	}
	result := scheduleEpisodeWithAuthority(revision, authority)
	if result.NextTaskRef == nil || !result.NextTaskRef.Equal(c) {
		t.Fatalf("dependency readiness schedule = %#v", result)
	}
}
