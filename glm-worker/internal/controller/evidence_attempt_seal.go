package controller

import "sort"

func canonicalizeAttemptSeal(record AttemptSeal) AttemptSeal {
	record.EvidenceRefs = canonicalEvidenceRefs(record.EvidenceRefs)
	record.FindingRecordRefs = canonicalEvidenceRefs(record.FindingRecordRefs)
	record.ReviewValidationRefs = canonicalEvidenceRefs(record.ReviewValidationRefs)
	record.SessionAssociationRefs = canonicalEvidenceRefs(record.SessionAssociationRefs)
	record.GitObjectArchiveRoots = canonicalGitObjectArchiveRoots(record.GitObjectArchiveRoots)
	record.RequiredKinds = canonicalUniqueStrings(record.RequiredKinds)
	record.Missing = canonicalStrings(record.Missing)
	record.Unreadable = canonicalStrings(record.Unreadable)
	return record
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
