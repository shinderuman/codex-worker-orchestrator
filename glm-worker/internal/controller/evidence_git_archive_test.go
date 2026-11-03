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
	t.Parallel()
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
	for _, claim := range []string{"root", "type", "format", "version", "pack-digest"} {
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
			case "version":
				envelope.SchemaVersion++
			case "pack-digest":
				envelope.PackDigest = strings.Repeat("0", 64)
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

func TestGitArchiveCachePreservesContentAndReferenceChecks(t *testing.T) {
	t.Parallel()
	source, _ := newControllerLinkedWorktree(t)
	store := newEvidenceTestStore(t)
	commit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	ref, expected, err := store.CaptureGitObjectArchive(source, "reference:git", []string{commit})
	if err != nil {
		t.Fatal(err)
	}
	roots, err := store.VerifyGitObjectArchive(ref)
	if err != nil {
		t.Fatal(err)
	}
	roots[0].OID = "changed-by-caller"
	for _, claim := range []string{"kind", "media-type", "length"} {
		t.Run(claim, func(t *testing.T) {
			changed := ref
			switch claim {
			case "kind":
				changed.Kind = "runtime-artifact"
			case "media-type":
				changed.MediaType = "application/json"
			case "length":
				changed.Length++
			}
			if _, err := store.VerifyGitObjectArchive(changed); err == nil {
				t.Fatal("verified archive bypassed changed reference checks")
			}
		})
	}
	roots, err = store.VerifyGitObjectArchive(ref)
	if err != nil || !reflect.DeepEqual(roots, expected) {
		t.Fatalf("returned roots changed verified archive authority: %v", err)
	}
	if err := os.Remove(store.evidenceObjectPath(ref.Digest)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyGitObjectArchive(ref); err == nil {
		t.Fatal("verified archive bypassed missing content")
	}
}

func TestGitObjectArchiveRejectsIncompleteObjectClosure(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	source, _ := newControllerLinkedWorktree(t)
	store := newEvidenceTestStore(t)
	commit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	ref, _, err := store.CaptureGitObjectArchive(source, "attempt-corrupt:git", []string{commit})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyGitObjectArchive(ref); err != nil {
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
	t.Parallel()
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

func TestCaptureGitObjectArchiveReusesCanonicalEnvelopeForSameRoots(t *testing.T) {
	t.Parallel()
	source, _ := newControllerLinkedWorktree(t)
	store := newEvidenceTestStore(t)
	commit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	canonical, _, err := store.CaptureGitObjectArchive(source, "candidate:"+commit, []string{commit})
	if err != nil {
		t.Fatal(err)
	}
	reused, _, err := store.captureGitObjectArchiveReusing(source, "remote:"+commit, &canonical, []string{commit})
	if err != nil {
		t.Fatal(err)
	}
	if reused.Digest != canonical.Digest || reused.LogicalIdentity != canonical.LogicalIdentity {
		t.Fatalf("same-roots capture minted a duplicate envelope: %#v", reused)
	}
	invalid := canonical
	invalid.MediaType = "application/json"
	if _, _, err := store.captureGitObjectArchiveReusing(source, "remote:"+commit, &invalid, []string{commit}); err == nil {
		t.Fatal("reuse accepted an archive with an unsupported media type")
	}
	tree := controllerGitOutput(t, source, "rev-parse", "HEAD^{tree}")
	fresh, _, err := store.captureGitObjectArchiveReusing(source, "external:"+tree, &canonical, []string{tree})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Digest == canonical.Digest {
		t.Fatal("different-roots capture reused an unrelated envelope")
	}
	uncached, _, err := store.captureGitObjectArchiveReusing(source, "external:"+commit, nil, []string{commit})
	if err != nil {
		t.Fatal(err)
	}
	if uncached.LogicalIdentity != "external:"+commit {
		t.Fatalf("capture without a canonical reference did not mint its own envelope: %#v", uncached)
	}
}
