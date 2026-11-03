package controller

import (
	"os"
	"testing"
)

func TestPublicationPolicyRefFormatReuseKeepsVerdicts(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	repo := fixture.store.identity.PrimaryRoot
	if err := validatePublicationPolicy(repo, policy); err != nil {
		t.Fatal(err)
	}
	if err := validatePublicationPolicy(repo, policy); err != nil {
		t.Fatalf("repeated validation of a valid policy failed: %v", err)
	}
	for _, ref := range []string{"refs/heads/bad..name", "refs/heads/also..bad"} {
		invalid := policy
		invalid.LocalRef = ref
		first := validatePublicationPolicy(repo, invalid)
		second := validatePublicationPolicy(repo, invalid)
		if first == nil || second == nil {
			t.Fatalf("malformed branch ref %s was accepted", ref)
		}
		if first.Error() != second.Error() {
			t.Fatalf("repeated validation of %s changed the failure: %v vs %v", ref, first, second)
		}
	}
	if err := validatePublicationPolicy(repo, policy); err != nil {
		t.Fatalf("valid policy verdict was disturbed by malformed refs: %v", err)
	}
}

func TestPublicationPolicyRefFormatFailureIsRetryable(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	repo := fixture.store.identity.PrimaryRoot
	policy.LocalRef = "refs/heads/ref-format-retry-probe"
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	if err := validatePublicationPolicy(repo, policy); err == nil {
		t.Fatal("policy validation succeeded without Git")
	}
	t.Setenv("PATH", originalPath)
	if err := validatePublicationPolicy(repo, policy); err != nil {
		t.Fatalf("transient failure poisoned the ref format validation cache: %v", err)
	}
}
