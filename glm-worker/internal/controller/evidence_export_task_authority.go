package controller

import (
	"fmt"
	"os"
)

func (b *evidenceExportBuilder) collectSealedTaskAuthority(seal AttemptSeal, prefix string) {
	entryPath := prefix + "/authority.md"
	if seal.SemanticTaskRef.TaskPath == "" || b.hasEntry(entryPath) {
		return
	}
	data, err := b.store.readArchivedGitFile(seal.GitObjectArchive, seal.CurrentWorktreeTree, seal.SemanticTaskRef.TaskPath)
	if err != nil {
		b.recordUnreadable(entryPath, err.Error())
		return
	}
	if digestBytes(data) != seal.SemanticTaskRef.ContractDigest {
		b.recordUnreadable(entryPath, "sealed Task content differs from its contract digest")
		return
	}
	b.commit(EvidenceExportEntry{
		Path: entryPath, Source: "task-authority", SHA256: digestBytes(data), Bytes: int64(len(data)),
		CollectedAt: b.observedAt, Basis: evidenceExportBasisAttemptSeal,
		CanonicalDigest: seal.GitObjectArchive.Digest, CanonicalKind: seal.GitObjectArchive.Kind,
		LogicalIdentity: seal.AttemptID + ":authority.md",
	}, data)
}

func (s *Store) readArchivedGitFile(ref EvidenceObjectRef, tree, path string) ([]byte, error) {
	roots, err := s.VerifyGitObjectArchive(ref)
	if err != nil {
		return nil, err
	}
	bound := false
	for _, root := range roots {
		bound = bound || root.Type == "tree" && root.OID == tree
	}
	if !bound {
		return nil, fmt.Errorf("task authority tree is not a root of the sealed Git archive")
	}
	data, err := s.loadGitObjectArchiveData(ref)
	if err != nil {
		return nil, err
	}
	envelope, err := decodeGitObjectArchiveEnvelope(ref, data)
	if err != nil {
		return nil, err
	}
	return readGitArchiveFile(envelope, tree, path)
}

func readGitArchiveFile(envelope gitObjectArchiveEnvelope, tree, path string) ([]byte, error) {
	repo, err := os.MkdirTemp("", "controller-task-authority-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(repo) }()
	if _, err := runGitBinary(repo, nil, "init", "--bare", "--quiet", "--object-format="+envelope.ObjectFormat, repo); err != nil {
		return nil, err
	}
	if _, err := runGitBinaryWithGitDir(repo, envelope.Pack, "index-pack", "--stdin"); err != nil {
		return nil, err
	}
	return runGitBinaryWithGitDir(repo, nil, "cat-file", "blob", tree+":"+path)
}
