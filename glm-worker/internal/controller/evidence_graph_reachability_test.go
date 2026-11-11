package controller

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type graphHistoryFixture struct {
	store       *Store
	ledger      EvidenceObjectRef
	head        EvidenceObjectRef
	taskRefs    []EvidenceObjectRef
	episodeRefs []EvidenceObjectRef
	refs        map[string]EvidenceObjectRef
	sequence    uint64
	task        SemanticTaskRef
}

func newGraphHistoryStore(tb testing.TB) *Store {
	tb.Helper()
	repo := filepath.Join(tb.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		tb.Fatal(err)
	}
	for name, contents := range map[string]string{
		"source.txt": "base\n",
		"IMPLEMENTATION_PLAN.local.md": "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`
",
		"IMPLEMENTATION_TASKS/root.md": "# root

## External feasibility

status: not-applicable

## Contract

controller root task

## Dependencies

none
",
	} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(contents), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "controller@example.invalid"},
		{"config", "user.name", "Controller Test"},
		{"add", "."},
		{"commit", "-q", "-m", "base"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			tb.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	store, err := Open(controllerTestConfig(repo, filepath.Join(tb.TempDir(), "state", "sessions")))
	if err != nil {
		tb.Fatal(err)
	}
	return store
}

func buildGraphHistory(tb testing.TB, store *Store, count int) graphHistoryFixture {
	tb.Helper()
	result := graphHistoryFixture{
		store: store,
		task: SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/graph-history.md", ContractDigest: "graph-history"},
		refs: map[string]EvidenceObjectRef{},
	}
	subject := taskEvidenceSubjectID(result.task)
	var previousTask, previousEpisode, previousHead, previousLedger *EvidenceObjectRef
	var oldestTask EvidenceObjectRef
	add := func(ref EvidenceObjectRef) {
		result.refs[evidenceRefKey(ref)] = ref
	}
	for i := 1; i <= count; i++ {
		generation := uint64(i)
		transition := fmt.Sprintf("transition-%d", i)
		created := time.Unix(10000+int64(i)*10, 0).UTC()
		task := testTaskIndexRevision(result.task, generation, transition, created)
		task.PreviousRevision = previousTask
		taskRef, _, err := store.StoreTaskIndexRevision(task)
		if err != nil {
			tb.Fatal(err)
		}
		if i == 1 {
			oldestTask = taskRef
		}
		episode := EpisodeIndexRevision{
			SchemaVersion: evidenceSchemaVersion,
			EpisodeID: "episode-graph-history",
			RootTaskRef: result.task,
			EpisodeRevision: generation,
			PreviousRevision: previousEpisode,
			DependencyGraphSnapshotID: fmt.Sprintf("dependency-%d", i),
			AdmittedClosureTaskRefs: []SemanticTaskRef{result.task},
			TaskIndexHeads: []EvidenceSubjectHead{{SubjectID: subject, RevisionRef: oldestTask}},
			State: "open",
			ControllerGeneration: generation,
			TransitionID: transition,
			CreatedAt: created.Add(time.Second),
		}
		episodeRef, _, err := store.StoreEpisodeIndexRevision(episode)
		if err != nil {
			tb.Fatal(err)
		}
		snapshot := fmt.Sprintf("snapshot-%d", i)
		headRef, _, err := store.StoreEvidenceHead(EvidenceHead{
			SchemaVersion: evidenceSchemaVersion,
			RepositoryIdentity: store.Identity().LineageID,
			PreviousHead: previousHead,
			TaskHeads: []EvidenceSubjectHead{{SubjectID: subject, RevisionRef: taskRef}},
			EpisodeHeads: []EvidenceSubjectHead{{SubjectID: episode.EpisodeID, RevisionRef: episodeRef}},
			ControllerGeneration: generation,
			ProjectSnapshotID: snapshot,
			CreatedAt: created.Add(2 * time.Second),
		})
		if err != nil {
			tb.Fatal(err)
		}
		ledgerRef, _, err := store.StoreEvidenceLedgerRecord(EvidenceLedgerRecord{
			SchemaVersion: evidenceSchemaVersion,
			Sequence: generation,
			PreviousRecord: previousLedger,
			RepositoryIdentity: store.Identity().LineageID,
			ControllerGeneration: generation,
			TransitionID: transition,
			ChangedTaskIndexHeads: []EvidenceSubjectHead{{SubjectID: subject, RevisionRef: taskRef}},
			ChangedEpisodeIndexHeads: []EvidenceSubjectHead{{SubjectID: episode.EpisodeID, RevisionRef: episodeRef}},
			ProjectSnapshotID: snapshot,
			EvidenceHeadRef: headRef,
			CreatedAt: created.Add(3 * time.Second),
		})
		if err != nil {
			tb.Fatal(err)
		}
		add(taskRef)
		add(episodeRef)
		add(headRef)
		add(ledgerRef)
		result.taskRefs = append(result.taskRefs, taskRef)
		result.episodeRefs = append(result.episodeRefs, episodeRef)
		previousTask = &taskRef
		previousEpisode = &episodeRef
		previousHead = &headRef
		previousLedger = &ledgerRef
		result.head = headRef
		result.ledger = ledgerRef
		result.sequence = generation
	}
	return result
}

func TestEvidenceGraphHistoryReachabilityAndDigest(t *testing.T) {
	t.Parallel()
	for _, size := range []int{4, 12, 24} {
		t.Run(fmt.Sprintf("history-%d", size), func(t *testing.T) {
			fixture := buildGraphHistory(t, newGraphHistoryStore(t), size)
			got, err := fixture.store.validateEvidenceGraphRoots(fixture.ledger, fixture.head, fixture.sequence)
			if err != nil {
				t.Fatal(err)
			}
			want, err := evidenceGraphDigest(fixture.refs)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("full graph digest differs from explicit immutable ref set: got=%s want=%s", got, want)
			}
		})
	}
}

func TestEvidenceGraphHistoryRejectsFreshMissingOrCorruptObjects(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"missing-task", "corrupt-episode", "missing-ledger"} {
		t.Run(kind, func(t *testing.T) {
			fixture := buildGraphHistory(t, newGraphHistoryStore(t), 8)
			if _, err := fixture.store.validateEvidenceGraphRoots(fixture.ledger, fixture.head, fixture.sequence); err != nil {
				t.Fatal(err)
			}
			var path string
			switch kind {
			case "missing-task":
				path = fixture.store.evidenceObjectPath(fixture.taskRefs[0].Digest)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "corrupt-episode":
				path = fixture.store.evidenceObjectPath(fixture.episodeRefs[0].Digest)
				if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing-ledger":
				path = fixture.store.evidenceObjectPath(fixture.ledger.Digest)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := fixture.store.validateEvidenceGraphRoots(fixture.ledger, fixture.head, fixture.sequence); err == nil {
				t.Fatal("fresh graph validation accepted missing or corrupt immutable object")
			}
		})
	}
}

func TestIndexedPublishedTaskRevisionAuthority(t *testing.T) {
	t.Parallel()
	fixture := buildGraphHistory(t, newGraphHistoryStore(t), 18)
	subject := taskEvidenceSubjectID(fixture.task)
	walker := evidenceGraphWalker{store: fixture.store, refs: map[string]EvidenceObjectRef{}}
	if err := walker.walkTaskRevisionChain(subject, fixture.taskRefs[17], 18); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 6, 17} {
		if err := walker.validateIndexedPublishedTaskRevision(subject, fixture.taskRefs[17], fixture.taskRefs[i]); err != nil {
			t.Fatalf("reachable revision %d rejected: %v", i, err)
		}
	}
	if err := walker.validateIndexedPublishedTaskRevision(subject, fixture.taskRefs[4], fixture.taskRefs[17]); err == nil {
		t.Fatal("unreachable descendant was accepted")
	}
	if err := walker.validateIndexedPublishedTaskRevision("different-subject", fixture.taskRefs[17], fixture.taskRefs[0]); err == nil {
		t.Fatal("different subject was accepted")
	}
	modified := fixture.taskRefs[0]
	modified.MediaType = "text/plain"
	if err := walker.validateIndexedPublishedTaskRevision(subject, fixture.taskRefs[17], modified); err == nil {
		t.Fatal("different media type with identical digest was accepted")
	}
	modified = fixture.taskRefs[0]
	modified.Required = !modified.Required
	if err := walker.validateIndexedPublishedTaskRevision(subject, fixture.taskRefs[17], modified); err == nil {
		t.Fatal("different required attribute with identical digest was accepted")
	}
	if err := walker.walkTaskRevisionChain(subject, fixture.taskRefs[17], 17); err == nil {
		t.Fatal("stricter generation ceiling was bypassed")
	}
	if err := walker.walkTaskRevisionChain(subject, fixture.taskRefs[10], 18); err != nil {
		t.Fatal(err)
	}
	if err := walker.validateIndexedPublishedTaskRevision(subject, fixture.taskRefs[10], fixture.taskRefs[12]); err == nil {
		t.Fatal("changed published authority accepted unrelated descendant")
	}
}

func BenchmarkEvidenceGraphHistory(b *testing.B) {
	for _, count := range []int{8, 16, 32, 64} {
		b.Run(fmt.Sprintf("ledger-episode-task-%d", count), func(b *testing.B) {
			fixture := buildGraphHistory(b, newGraphHistoryStore(b), count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := fixture.store.validateEvidenceGraphRoots(fixture.ledger, fixture.head, fixture.sequence); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
