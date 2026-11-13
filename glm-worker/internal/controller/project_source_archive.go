package controller

import "fmt"

func (s *Store) restoreProjectSource(snapshot ProjectSnapshot) error {
	if snapshot.SourceArchive == nil {
		return nil
	}
	roots, err := s.VerifyGitObjectArchive(*snapshot.SourceArchive)
	if err != nil {
		return err
	}
	if len(roots) != 1 || roots[0].OID != snapshot.HeadOID || roots[0].Type != gitCommitObjectType {
		return fmt.Errorf("project source archive root differs from snapshot HEAD")
	}
	if _, err := gitTrimmed(s.identity.PrimaryRoot, "cat-file", "-e", snapshot.HeadOID+"^{commit}"); err == nil {
		return nil
	}
	data, err := s.loadGitObjectArchiveData(*snapshot.SourceArchive)
	if err != nil {
		return err
	}
	archive, err := decodeGitObjectArchiveEnvelope(*snapshot.SourceArchive, data)
	if err != nil {
		return err
	}
	if _, err := runGitBinary(s.identity.PrimaryRoot, archive.Pack, "unpack-objects", "-r"); err != nil {
		return fmt.Errorf("restore project source %s: %w", snapshot.HeadOID, err)
	}
	return nil
}
