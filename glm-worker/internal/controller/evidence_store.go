package controller

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const evidenceSchemaVersion = 1

func (s *Store) PutEvidenceObject(kind, mediaType, logicalIdentity string, required bool, data []byte) (EvidenceObjectRef, error) {
	kind = strings.TrimSpace(kind)
	mediaType = strings.TrimSpace(mediaType)
	logicalIdentity = strings.TrimSpace(logicalIdentity)
	if kind == "" || mediaType == "" || logicalIdentity == "" {
		return EvidenceObjectRef{}, fmt.Errorf("evidence object identity is incomplete")
	}
	ref := EvidenceObjectRef{
		Digest:          digestBytes(data),
		Kind:            kind,
		MediaType:       mediaType,
		Length:          int64(len(data)),
		LogicalIdentity: logicalIdentity,
		Required:        required,
	}
	if err := s.writeEvidenceBytes(ref.Digest, data); err != nil {
		return EvidenceObjectRef{}, err
	}
	return ref, nil
}

func (s *Store) LoadEvidenceObject(ref EvidenceObjectRef) ([]byte, error) {
	if err := validateEvidenceRef(ref); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.evidenceObjectPath(ref.Digest))
	if errors.Is(err, os.ErrNotExist) {
		return nil, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "required content-addressed object is missing"}
	}
	if err != nil {
		return nil, fmt.Errorf("read evidence object %s: %w", ref.Digest, err)
	}
	if err := validateEvidenceBytes(ref.Digest, ref.Length, data); err != nil {
		return nil, err
	}
	return data, nil
}

func validateEvidenceRef(ref EvidenceObjectRef) error {
	if strings.TrimSpace(ref.Digest) == "" || strings.TrimSpace(ref.Kind) == "" || strings.TrimSpace(ref.MediaType) == "" || strings.TrimSpace(ref.LogicalIdentity) == "" || ref.Length < 0 {
		return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "reference identity is incomplete"}
	}
	return nil
}

func (s *Store) writeEvidenceBytes(digest string, data []byte) error {
	path := s.evidenceObjectPath(digest)
	handled, err := validateExistingEvidenceObject(path, digest, data)
	if err != nil || handled {
		return err
	}
	if len(digest) < 2 {
		return &EvidenceIntegrityError{Digest: digest, Reason: "digest is malformed"}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create evidence object directory: %w", err)
	}
	return createEvidenceObject(path, digest, data)
}

func validateExistingEvidenceObject(path, digest string, data []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect evidence object %s: %w", digest, err)
	}
	if err := validateEvidenceBytes(digest, int64(len(data)), existing); err != nil || !bytes.Equal(existing, data) {
		return true, &EvidenceIntegrityError{Digest: digest, Reason: "existing content-addressed object conflicts with requested content"}
	}
	return true, nil
}

func createEvidenceObject(path, digest string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".evidence-object-*.tmp")
	if err != nil {
		return fmt.Errorf("create evidence object temp %s: %w", digest, err)
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("chmod evidence object temp %s: %w", digest, err)
	}
	if err := writeEvidenceObject(file, temporary, digest, data); err != nil {
		return err
	}
	if err := os.Link(temporary, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			if err := validateRacedEvidenceObject(path, digest, data); err != nil {
				return err
			}
			if err := syncDirectoryPath(dir); err != nil {
				return fmt.Errorf("sync raced evidence object namespace %s: %w", digest, err)
			}
			return nil
		}
		return fmt.Errorf("publish evidence object %s: %w", digest, err)
	}
	if err := syncDirectoryPath(dir); err != nil {
		return fmt.Errorf("publish evidence object %s durably: %w", digest, err)
	}
	return nil
}

func validateRacedEvidenceObject(path, digest string, data []byte) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read raced evidence object %s: %w", digest, err)
	}
	if err := validateEvidenceBytes(digest, int64(len(data)), existing); err != nil || !bytes.Equal(existing, data) {
		return &EvidenceIntegrityError{Digest: digest, Reason: "raced content-addressed object conflicts with requested content"}
	}
	return nil
}

func writeEvidenceObject(file *os.File, path, digest string, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write evidence object %s: %w", digest, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("sync evidence object %s: %w", digest, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close evidence object %s: %w", digest, err)
	}
	return nil
}

func validateEvidenceBytes(digest string, length int64, data []byte) error {
	if int64(len(data)) != length {
		return &EvidenceIntegrityError{Digest: digest, Reason: "stored length does not match reference"}
	}
	if digestBytes(data) != digest {
		return &EvidenceIntegrityError{Digest: digest, Reason: "stored digest does not match reference"}
	}
	return nil
}

func (s *Store) evidenceObjectPath(digest string) string {
	prefix := "invalid"
	if len(digest) >= 2 {
		prefix = digest[:2]
	}
	return filepath.Join(s.dir, "evidence", "objects", prefix, digest)
}
