package controller

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGitArchiveCacheBindsAllVerificationClaims(t *testing.T) {
	source, _ := newControllerLinkedWorktree(t)
	store := newEvidenceTestStore(t)
	commit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	ref, _, err := store.CaptureGitObjectArchive(source, "cache:git", []string{commit})
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.LoadEvidenceObject(ref)
	if err != nil {
		t.Fatal(err)
	}
	var original gitObjectArchiveEnvelope
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyGitObjectArchive(ref); err != nil {
		t.Fatal(err)
	}
	for _, claim := range []string{"root", "type", "format"} {
		t.Run(claim, func(t *testing.T) {
			envelope := original
			envelope.Roots = append([]GitObjectArchiveRoot(nil), original.Roots...)
			switch claim {
			case "root":
				envelope.Roots[0].OID = strings.Repeat("0", 40)
			case "type":
				envelope.Roots[0].Type = "tree"
			case "format":
				envelope.ObjectFormat = "unsupported"
			}
			payload, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			forged, err := store.PutEvidenceObject("git-object-archive", gitObjectArchiveMediaType, "cache:"+claim, true, payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.VerifyGitObjectArchive(forged); err == nil {
				t.Fatal("cached pack accepted invalid archive claims")
			}
		})
	}
}

func TestGitObjectArchiveRejectsIncompleteObjectClosure(t *testing.T) {
	source, _ := newControllerLinkedWorktree(t)
	store := newEvidenceTestStore(t)
	for _, objectType := range []string{"commit", "tree"} {
		t.Run(objectType, func(t *testing.T) {
			oid := controllerGitOutput(t, source, "rev-parse", "HEAD^{"+objectType+"}")
			pack, err := runGitBinary(source, []byte(oid+"\n"), "pack-objects", "--stdout")
			if err != nil {
				t.Fatal(err)
			}
			envelope := gitObjectArchiveEnvelope{SchemaVersion: evidenceSchemaVersion, ObjectFormat: "sha1", Roots: []GitObjectArchiveRoot{{OID: oid, Type: objectType}}, PackDigest: digestBytes(pack), Pack: pack}
			data, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := store.PutEvidenceObject("git-object-archive", gitObjectArchiveMediaType, "incomplete:"+objectType, true, data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.VerifyGitObjectArchive(ref); err == nil {
				t.Fatal("archive missing reachable objects was accepted")
			}
		})
	}
}

func TestGitArchiveVerificationFailureIsRetryable(t *testing.T) {
	source, _ := newControllerLinkedWorktree(t)
	store := newEvidenceTestStore(t)
	commit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	ref, _, err := store.CaptureGitObjectArchive(source, "retry:git", []string{commit})
	if err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	if _, err := store.VerifyGitObjectArchive(ref); err == nil {
		t.Fatal("verification succeeded without Git")
	}
	t.Setenv("PATH", originalPath)
	if _, err := store.VerifyGitObjectArchive(ref); err != nil {
		t.Fatalf("transient failure poisoned the verification cache: %v", err)
	}
}

func TestGitObjectArchiveSurvivesSourceDeletionAndPathReuse(t *testing.T) {
	source, _ := newControllerLinkedWorktree(t)
	store := newEvidenceTestStore(t)
	commit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	tree := controllerGitOutput(t, source, "rev-parse", "HEAD^{tree}")

	ref, roots, err := store.CaptureGitObjectArchive(source, "attempt-a:git", []string{tree, commit, tree})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("archive did not canonicalize duplicate roots: %#v", roots)
	}
	if ref.Kind != "git-object-archive" || ref.MediaType != gitObjectArchiveMediaType || !ref.Required {
		t.Fatalf("archive reference lost evidence identity: %#v", ref)
	}

	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, source, "init", "-q")
	runControllerGit(t, source, "config", "user.email", "replacement@example.invalid")
	runControllerGit(t, source, "config", "user.name", "Replacement")
	if err := os.WriteFile(filepath.Join(source, "replacement.txt"), []byte("new lane\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, source, "add", ".")
	runControllerGit(t, source, "commit", "-q", "-m", "replacement")
	newCommit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	if newCommit == commit {
		t.Fatal("path reuse unexpectedly recreated the old commit")
	}

	verified, err := store.VerifyGitObjectArchive(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(verified, roots) {
		t.Fatalf("archive roots changed after source deletion/path reuse: got=%#v want=%#v", verified, roots)
	}
	for _, root := range verified {
		if root.OID == newCommit {
			t.Fatalf("old archive read evidence from replacement path: %#v", verified)
		}
	}
}

func TestGitObjectArchiveCorruptionFailsLoudly(t *testing.T) {
	source, _ := newControllerLinkedWorktree(t)
	store := newEvidenceTestStore(t)
	commit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	ref, _, err := store.CaptureGitObjectArchive(source, "attempt-corrupt:git", []string{commit})
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.LoadEvidenceObject(ref)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(store.evidenceObjectPath(ref.Digest), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyGitObjectArchive(ref); err == nil {
		t.Fatal("corrupt portable Git archive was accepted")
	} else {
		var integrity *EvidenceIntegrityError
		if !errors.As(err, &integrity) {
			t.Fatalf("corrupt archive did not produce typed integrity failure: %v", err)
		}
	}
}

func TestGitObjectArchiveRejectsMalformedEnvelope(t *testing.T) {
	store := newEvidenceTestStore(t)
	ref, err := store.PutEvidenceObject("git-object-archive", gitObjectArchiveMediaType, "attempt-malformed:git", true, []byte(`{"schema_version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyGitObjectArchive(ref); err == nil {
		t.Fatal("malformed portable Git archive was accepted")
	} else {
		var integrity *EvidenceIntegrityError
		if !errors.As(err, &integrity) {
			t.Fatalf("malformed archive did not produce typed integrity failure: %v", err)
		}
	}
}

func controllerGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", dir}, args...)
	command := exec.Command("git", commandArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", commandArgs, err, output)
	}
	return strings.TrimSpace(string(output))
}
