package controller

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestTaskAuthorityPreparedRecoveryPreservesTransportAndRejectsForeignArtifacts(t *testing.T) {
	for _, owned := range []bool{true, false} {
		name := "foreign-artifact"
		if owned {
			name = "owned-transport"
		}
		t.Run(name, func(t *testing.T) {
			f, _, source, _ := rootLaneExecutionFixture(t)
			primary := f.store.Identity().PrimaryRoot
			task := source.Attempt.SemanticTaskRef
			original, err := os.ReadFile(filepath.Join(primary, task.TaskPath))
			if err != nil {
				t.Fatal(err)
			}
			revised := append(append([]byte(nil), original...), []byte("\n追加のTask要求。\n")...)
			writeSuspensionTestFile(t, primary, task.TaskPath, string(revised))
			task.ContractDigest = fmt.Sprintf("%x", sha256.Sum256(revised))
			artifact := "unowned-ignored-artifact"
			if owned {
				artifact = parentaction.StageDirName + "/"
			}
			exclude := filepath.Join(f.store.Identity().CommonDir, "info", "exclude")
			file, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := file.WriteString("\n" + artifact + "\n")
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				t.Fatalf("exclude: %v %v", writeErr, closeErr)
			}
			path := filepath.Join(source.Workspace.Root, artifact)
			if owned {
				prepared, err := parentaction.Prepare(source.Workspace.Root, "decision")
				if err != nil {
					t.Fatal(err)
				}
				path = prepared.Path
			} else {
				writeSuspensionTestFile(t, source.Workspace.Root, artifact, "foreign bytes\n")
			}
			retained, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := CaptureWorkspaceSnapshot(primary)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", source.Attempt.AttemptID+":"+TaskAuthoritySyncProducer+":"+snapshot.ID, true, []byte(`{"runtime":"unstarted"}`))
			if err != nil {
				t.Fatal(err)
			}
			op, err := f.store.planTaskAuthoritySync(source, snapshot.ID, ref, task)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.store.prepareExecutionOperation(&op, source.Head); err != nil {
				t.Fatal(err)
			}
			head, err := f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			pending, err := f.store.PendingCompletionRecovery(head)
			if err != nil || !pending {
				t.Errorf("prepared Task sync has no regular recovery admission: %v %v", pending, err)
			}
			result, recoverErr := f.store.RecoverExecutionOperation(op.Transition.TransitionID)
			if !owned {
				if recoverErr == nil || !strings.Contains(recoverErr.Error(), "unexpected materialization path") {
					t.Fatalf("foreign artifact admitted: %v", recoverErr)
				}
			} else {
				if recoverErr != nil {
					t.Fatal(recoverErr)
				}
				if result.Head.PendingTransitionID != "" || result.Head.ExecutionTaskRef == nil || !result.Head.ExecutionTaskRef.Equal(task) {
					t.Fatal("recovery did not finalize revised Task authority")
				}
				if result.Admission == nil || result.Admission.Attempt.AttemptID != source.Attempt.AttemptID || result.Admission.Snapshot.Head != source.Snapshot.Head || result.Admission.Snapshot.IndexDigest != source.Snapshot.IndexDigest || result.Admission.Snapshot.RefDigest != source.Snapshot.RefDigest {
					t.Fatal("recovery changed execution or protected Git authority")
				}
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(retained, actual) {
				t.Fatal("recovery changed retained artifact")
			}
		})
	}
}
