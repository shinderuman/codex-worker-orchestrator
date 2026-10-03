package controller

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func validatePublicationPolicy(repo string, policy PublicationPolicy) error {
	if policy.Remote == "" || strings.HasPrefix(policy.Remote, "-") || strings.ContainsAny(policy.Remote, "/\\ \t\r\n") {
		return fmt.Errorf("publication requires a configured remote name")
	}
	for _, ref := range []string{policy.LocalRef, policy.RemoteRef} {
		if !strings.HasPrefix(ref, "refs/heads/") {
			return fmt.Errorf("publication requires explicit branch refs")
		}
		if _, err := runGitBinary(repo, nil, "check-ref-format", ref); err != nil {
			return err
		}
	}
	_, err := runGitBinary(repo, nil, "remote", "get-url", policy.Remote)
	return err
}

func observePublicationRemote(repo string, policy PublicationPolicy) (string, error) {
	if err := validatePublicationPolicy(repo, policy); err != nil {
		return "", err
	}
	data, err := runGitBinary(repo, nil, "ls-remote", "--exit-code", policy.Remote, policy.RemoteRef)
	if err != nil {
		return "", fmt.Errorf("publication remote is unreadable: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		return "", fmt.Errorf("publication remote ref is ambiguous")
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 2 || fields[1] != policy.RemoteRef {
		return "", fmt.Errorf("publication remote identity is invalid")
	}
	oid := fields[0]
	if _, err := runGitBinary(repo, nil, "fetch", "--no-tags", "--no-write-fetch-head", policy.Remote, oid); err != nil {
		return "", err
	}
	return oid, nil
}

func isPublicationDescendant(repo, ancestor, tip string) (bool, error) {
	if ancestor == "" || tip == "" {
		return false, fmt.Errorf("publication ancestry is incomplete")
	}
	_, err := runGitBinary(repo, nil, "merge-base", "--is-ancestor", ancestor, tip)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func enforceObservedPrefix(repo string, head RepositoryControllerHead, remote string) error {
	anchor := head.IntegrationTip
	if head.ObservedPrefix != "" {
		anchor = head.ObservedPrefix
	}
	descendant, err := isPublicationDescendant(repo, anchor, remote)
	if err != nil {
		return err
	}
	if !descendant {
		return fmt.Errorf("remote advancement rewrites immutable prefix or integration authority")
	}
	return nil
}

func updatePublicationRef(repo, ref, old, newOID string) error {
	actual, exists, err := readExecutionRef(repo, ref)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("publication local ref is missing")
	}
	if actual == newOID {
		return nil
	}
	if actual != old {
		return fmt.Errorf("publication local ref moved unexpectedly")
	}
	_, err = runGitBinary(repo, nil, "update-ref", ref, newOID, old)
	return err
}

func (s *Store) observeCandidateRemote(head RepositoryControllerHead, c AcceptedCandidate) (string, error) {
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, c.Policy)
	if err != nil {
		return "", err
	}
	if err := s.ensurePublicationPrefix(head, remote); err != nil {
		return "", err
	}
	return remote, nil
}

func (s *Store) candidatePromotionLocal(c AcceptedCandidate) (string, error) {
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, c.Policy.LocalRef)
	if err != nil {
		return "", err
	}
	if !exists || (local != c.BaseOID && local != c.CommitOID) {
		return "", fmt.Errorf("local ref is outside candidate promotion authority")
	}
	return local, nil
}
