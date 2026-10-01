package controller

import (
	"fmt"
	"sort"
	"strings"
)

func canonicalizeAttemptSeal(record AttemptSeal) AttemptSeal {
	record.FindingRecordRefs = canonicalEvidenceRefs(record.FindingRecordRefs)
	record.ReviewValidationRefs = canonicalEvidenceRefs(record.ReviewValidationRefs)
	record.SessionAssociationRefs = canonicalEvidenceRefs(record.SessionAssociationRefs)
	record.EvidenceRefs = canonicalUniqueEvidenceRefs(attemptSealGraphEvidenceRefs(record))
	record.GitObjectArchiveRoots = canonicalGitObjectArchiveRoots(record.GitObjectArchiveRoots)
	record.RequiredKinds = canonicalUniqueStrings(append(record.RequiredKinds, requiredEvidenceKinds(record)...))
	record.Missing = canonicalStrings(record.Missing)
	record.Unreadable = canonicalStrings(record.Unreadable)
	return record
}

func canonicalUniqueEvidenceRefs(refs []EvidenceObjectRef) []EvidenceObjectRef {
	result := canonicalEvidenceRefs(refs)
	if len(result) < 2 {
		return result
	}
	unique := result[:1]
	for _, ref := range result[1:] {
		if evidenceRefsEqual(ref, unique[len(unique)-1]) {
			continue
		}
		unique = append(unique, ref)
	}
	return unique
}

func canonicalGitObjectArchiveRoots(roots []GitObjectArchiveRoot) []GitObjectArchiveRoot {
	result := append([]GitObjectArchiveRoot(nil), roots...)
	sort.Slice(result, func(i, j int) bool {
		left := result[i].Type + "\x00" + result[i].OID
		right := result[j].Type + "\x00" + result[j].OID
		return left < right
	})
	if len(result) < 2 {
		return result
	}
	unique := result[:1]
	for _, root := range result[1:] {
		if root == unique[len(unique)-1] {
			continue
		}
		unique = append(unique, root)
	}
	return unique
}

func canonicalUniqueStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) < 2 {
		return result
	}
	unique := result[:1]
	for _, value := range result[1:] {
		if value == unique[len(unique)-1] {
			continue
		}
		unique = append(unique, value)
	}
	return unique
}

func attemptSealGraphEvidenceRefs(record AttemptSeal) []EvidenceObjectRef {
	refs := make([]EvidenceObjectRef, 0, len(record.EvidenceRefs)+len(record.FindingRecordRefs)+len(record.ReviewValidationRefs)+len(record.SessionAssociationRefs))
	refs = append(refs, record.EvidenceRefs...)
	refs = append(refs, record.FindingRecordRefs...)
	refs = append(refs, record.ReviewValidationRefs...)
	refs = append(refs, record.SessionAssociationRefs...)
	return refs
}

func requiredEvidenceKinds(record AttemptSeal) []string {
	kinds := make([]string, 0, 1+len(record.EvidenceRefs))
	if record.GitObjectArchive.Required {
		kinds = append(kinds, record.GitObjectArchive.Kind)
	}
	for _, ref := range record.EvidenceRefs {
		if ref.Required {
			kinds = append(kinds, ref.Kind)
		}
	}
	return kinds
}

func validateAttemptSealPortable(record AttemptSeal) error {
	if strings.TrimSpace(record.SourceProjectSnapshotID) == "" {
		return fmt.Errorf("attempt seal source project snapshot identity is incomplete")
	}
	if record.StartedAt.IsZero() || record.SealedAt.IsZero() || record.SealedAt.Before(record.StartedAt) {
		return fmt.Errorf("attempt seal execution timestamps are invalid")
	}
	if strings.TrimSpace(record.EpisodeID) == "" {
		if record.EpisodeRevision != 0 {
			return fmt.Errorf("attempt seal episode revision exists without episode identity")
		}
	} else if record.EpisodeRevision == 0 {
		return fmt.Errorf("attempt seal episode identity is missing its revision")
	}
	if err := validateAttemptSealCandidate(record); err != nil {
		return err
	}
	if err := validateAttemptSealArchiveRoots(record); err != nil {
		return err
	}
	if err := validateAttemptSealDedicatedRefs(record); err != nil {
		return err
	}
	return validateAttemptSealCoverage(record)
}

func validateAttemptSealCandidate(record AttemptSeal) error {
	values := []string{record.CandidateCommitOID, record.CandidateTreeOID, record.CandidateBaseOID, record.CandidateSnapshotID}
	present := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			present++
		}
	}
	if strings.TrimSpace(record.CandidateID) != "" && present == 0 {
		return fmt.Errorf("attempt seal candidate identity lacks exact candidate authority")
	}
	if present != 0 && present != len(values) {
		return fmt.Errorf("attempt seal candidate authority is incomplete")
	}
	return nil
}

func validateAttemptSealArchiveRoots(record AttemptSeal) error {
	if len(record.GitObjectArchiveRoots) == 0 {
		return fmt.Errorf("attempt seal git archive roots are empty")
	}
	seen := make(map[string]string, len(record.GitObjectArchiveRoots))
	for _, root := range record.GitObjectArchiveRoots {
		oid := strings.TrimSpace(root.OID)
		typeName := strings.TrimSpace(root.Type)
		if oid == "" || typeName == "" {
			return fmt.Errorf("attempt seal git archive root identity is incomplete")
		}
		if existing, ok := seen[oid]; ok && existing != typeName {
			return fmt.Errorf("attempt seal git archive root %s has conflicting object types", oid)
		}
		seen[oid] = typeName
	}
	required := []GitObjectArchiveRoot{
		{OID: record.ExecutionBaseOID, Type: "commit"},
		{OID: record.BaselineIndexTree, Type: "tree"},
		{OID: record.BaselineWorktreeTree, Type: "tree"},
		{OID: record.CurrentIndexTree, Type: "tree"},
		{OID: record.CurrentWorktreeTree, Type: "tree"},
	}
	if strings.TrimSpace(record.CandidateCommitOID) != "" {
		required = append(required,
			GitObjectArchiveRoot{OID: record.CandidateCommitOID, Type: "commit"},
			GitObjectArchiveRoot{OID: record.CandidateBaseOID, Type: "commit"},
			GitObjectArchiveRoot{OID: record.CandidateTreeOID, Type: "tree"},
		)
	}
	for _, root := range required {
		if seen[strings.TrimSpace(root.OID)] != root.Type {
			return fmt.Errorf("attempt seal git archive roots omit required %s object %s", root.Type, root.OID)
		}
	}
	return nil
}

func validateAttemptSealDedicatedRefs(record AttemptSeal) error {
	for _, ref := range record.FindingRecordRefs {
		if err := validateTypedEvidenceRef(ref, "finding-record"); err != nil {
			return fmt.Errorf("attempt seal finding evidence reference: %w", err)
		}
	}
	for category, refs := range map[string][]EvidenceObjectRef{
		"review/validation": record.ReviewValidationRefs,
		"session association": record.SessionAssociationRefs,
	} {
		for _, ref := range refs {
			if err := validateEvidenceRef(ref); err != nil {
				return fmt.Errorf("attempt seal %s evidence reference: %w", category, err)
			}
		}
	}
	return nil
}

func validateAttemptSealCoverage(record AttemptSeal) error {
	switch record.Coverage {
	case "complete", "open", "incomplete", "corrupt":
	default:
		return fmt.Errorf("attempt seal coverage status %q is invalid", record.Coverage)
	}
	if len(record.RequiredKinds) == 0 {
		return fmt.Errorf("attempt seal required evidence kinds are empty")
	}
	for _, kind := range record.RequiredKinds {
		if strings.TrimSpace(kind) == "" {
			return fmt.Errorf("attempt seal required evidence kind is empty")
		}
	}
	for _, value := range append(append([]string(nil), record.Missing...), record.Unreadable...) {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("attempt seal coverage contains an empty missing/unreadable identity")
		}
	}
	if record.Coverage == "complete" && (len(record.Missing) != 0 || len(record.Unreadable) != 0) {
		return fmt.Errorf("attempt seal complete coverage contains missing or unreadable evidence")
	}
	return nil
}

func (s *Store) validateAttemptSealArchive(record AttemptSeal) error {
	if !record.GitObjectArchive.Required || record.GitObjectArchive.MediaType != gitObjectArchiveMediaType {
		return fmt.Errorf("attempt seal git archive is not a required portable archive")
	}
	roots, err := s.VerifyGitObjectArchive(record.GitObjectArchive)
	if err != nil {
		return fmt.Errorf("attempt seal git archive verification: %w", err)
	}
	roots = canonicalGitObjectArchiveRoots(roots)
	declared := canonicalGitObjectArchiveRoots(record.GitObjectArchiveRoots)
	if len(roots) != len(declared) {
		return fmt.Errorf("attempt seal git archive roots do not match portable archive")
	}
	for i := range roots {
		if roots[i] != declared[i] {
			return fmt.Errorf("attempt seal git archive roots do not match portable archive")
		}
	}
	return nil
}
