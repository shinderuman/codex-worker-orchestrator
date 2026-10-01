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
	if int64(len(data)) != ref.Length {
		return nil, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "stored length does not match reference"}
	}
	if digestBytes(data) != ref.Digest {
		return nil, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "stored digest does not match reference"}
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
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, data) || digestBytes(existing) != digest {
			return &EvidenceIntegrityError{Digest: digest, Reason: "existing content-addressed object conflicts with requested content"}
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect evidence object %s: %w", digest, err)
	}
	if len(digest) < 2 {
		return &EvidenceIntegrityError{Digest: digest, Reason: "digest is malformed"}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create evidence object directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read raced evidence object %s: %w", digest, readErr)
		}
		if !bytes.Equal(existing, data) || digestBytes(existing) != digest {
			return &EvidenceIntegrityError{Digest: digest, Reason: "raced content-addressed object conflicts with requested content"}
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create evidence object %s: %w", digest, err)
	}
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

func (s *Store) evidenceObjectPath(digest string) string {
	prefix := "invalid"
	if len(digest) >= 2 {
		prefix = digest[:2]
	}
	return filepath.Join(s.dir, "evidence", "objects", prefix, digest)
}
