package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type GitObjectArchiveRoot struct {
	OID  string `json:"oid"`
	Type string `json:"type"`
}

type gitObjectArchiveEnvelope struct {
	SchemaVersion int                    `json:"schema_version"`
	ObjectFormat  string                 `json:"object_format"`
	Roots         []GitObjectArchiveRoot `json:"roots"`
	PackDigest    string                 `json:"pack_digest"`
	Pack          []byte                 `json:"pack"`
}

type gitObjectArchiveVerification struct {
	objectFormat string
	roots        []GitObjectArchiveRoot
}

const gitCommitObjectType = "commit"
const gitTreeObjectType = "tree"

const gitObjectArchiveMediaType = "application/vnd.codex.git-object-archive+json"

func (s *Store) CaptureGitObjectArchive(repoPath, logicalIdentity string, rootOIDs []string) (EvidenceObjectRef, []GitObjectArchiveRoot, error) {
	data, roots, err := buildGitObjectArchiveEnvelope(repoPath, rootOIDs)
	if err != nil {
		return EvidenceObjectRef{}, nil, err
	}
	ref, err := s.PutEvidenceObject("git-object-archive", gitObjectArchiveMediaType, logicalIdentity, true, data)
	if err != nil {
		return EvidenceObjectRef{}, nil, err
	}
	return ref, append([]GitObjectArchiveRoot(nil), roots...), nil
}

func (s *Store) captureGitObjectArchiveReusing(repoPath, logicalIdentity string, canonical *EvidenceObjectRef, rootOIDs []string) (EvidenceObjectRef, []GitObjectArchiveRoot, error) {
	if canonical == nil {
		return s.CaptureGitObjectArchive(repoPath, logicalIdentity, rootOIDs)
	}
	roots, objectFormat, err := plannedGitObjectArchiveRoots(repoPath, rootOIDs)
	if err != nil {
		return EvidenceObjectRef{}, nil, err
	}
	verified, err := s.verifyGitObjectArchive(*canonical)
	if err != nil {
		return EvidenceObjectRef{}, nil, err
	}
	if verified.objectFormat != objectFormat || !sameGitObjectArchiveRoots(verified.roots, roots) {
		return s.CaptureGitObjectArchive(repoPath, logicalIdentity, rootOIDs)
	}
	return *canonical, append([]GitObjectArchiveRoot(nil), roots...), nil
}

func (s *Store) loadGitObjectArchiveData(ref EvidenceObjectRef) ([]byte, error) {
	if err := validateTypedEvidenceRef(ref, "git-object-archive"); err != nil {
		return nil, err
	}
	if ref.MediaType != gitObjectArchiveMediaType {
		return nil, gitArchiveIntegrityError(ref, "git object archive media type is unsupported")
	}
	return s.LoadEvidenceObject(ref)
}

func decodeGitObjectArchiveEnvelope(ref EvidenceObjectRef, data []byte) (gitObjectArchiveEnvelope, error) {
	var envelope gitObjectArchiveEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return gitObjectArchiveEnvelope{}, gitArchiveIntegrityError(ref, "git object archive envelope is invalid")
	}
	if err := validateGitObjectArchiveEnvelope(ref, envelope); err != nil {
		return gitObjectArchiveEnvelope{}, err
	}
	return envelope, nil
}

func plannedGitObjectArchiveRoots(repoPath string, rootOIDs []string) ([]GitObjectArchiveRoot, string, error) {
	objectFormat, err := resolveGitObjectFormat(repoPath)
	if err != nil {
		return nil, "", err
	}
	roots := make([]GitObjectArchiveRoot, 0, len(rootOIDs))
	seen := make(map[string]bool, len(rootOIDs))
	for _, rawOID := range rootOIDs {
		root, err := resolveGitObjectArchiveRoot(repoPath, rawOID)
		if err != nil {
			return nil, "", err
		}
		if seen[root.OID] {
			continue
		}
		seen[root.OID] = true
		roots = append(roots, root)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].OID < roots[j].OID })
	return roots, objectFormat, nil
}

func sameGitObjectArchiveRoots(left, right []GitObjectArchiveRoot) bool {
	if len(left) != len(right) {
		return false
	}
	byOID := make(map[string]string, len(left))
	for _, root := range left {
		byOID[root.OID] = root.Type
	}
	for _, root := range right {
		if byOID[root.OID] != root.Type {
			return false
		}
	}
	return true
}

func buildGitObjectArchiveEnvelope(repoPath string, rootOIDs []string) ([]byte, []GitObjectArchiveRoot, error) {
	roots, objectIDs, objectFormat, err := collectGitObjectClosure(repoPath, rootOIDs)
	if err != nil {
		return nil, nil, err
	}
	packInput := []byte(strings.Join(objectIDs, "\n") + "\n")
	pack, err := runGitBinary(repoPath, packInput, "pack-objects", "--stdout")
	if err != nil {
		return nil, nil, err
	}
	envelope := gitObjectArchiveEnvelope{
		SchemaVersion: evidenceSchemaVersion,
		ObjectFormat:  objectFormat,
		Roots:         roots,
		PackDigest:    digestBytes(pack),
		Pack:          pack,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, fmt.Errorf("encode git object archive: %w", err)
	}
	return data, roots, nil
}

func (s *Store) VerifyGitObjectArchive(ref EvidenceObjectRef) ([]GitObjectArchiveRoot, error) {
	verified, err := s.verifyGitObjectArchive(ref)
	if err != nil {
		return nil, err
	}
	return append([]GitObjectArchiveRoot(nil), verified.roots...), nil
}

func (s *Store) verifyGitObjectArchive(ref EvidenceObjectRef) (gitObjectArchiveVerification, error) {
	data, err := s.loadGitObjectArchiveData(ref)
	if err != nil {
		return gitObjectArchiveVerification{}, err
	}
	s.archiveVerification.Lock()
	defer s.archiveVerification.Unlock()
	if s.archiveVerified == nil {
		s.archiveVerified = make(map[string]gitObjectArchiveVerification)
	}
	if verified, cached := s.archiveVerified[ref.Digest]; cached {
		return verified, nil
	}
	envelope, err := decodeGitObjectArchiveEnvelope(ref, data)
	if err != nil {
		return gitObjectArchiveVerification{}, err
	}
	if err := verifyGitObjectPack(envelope); err != nil {
		return gitObjectArchiveVerification{}, gitArchiveIntegrityError(ref, err.Error())
	}
	verified := gitObjectArchiveVerification{objectFormat: envelope.ObjectFormat, roots: envelope.Roots}
	s.archiveVerified[ref.Digest] = verified
	return verified, nil
}

func collectGitObjectClosure(repoPath string, rootOIDs []string) ([]GitObjectArchiveRoot, []string, string, error) {
	objectFormat, err := resolveGitObjectFormat(repoPath)
	if err != nil {
		return nil, nil, "", err
	}
	roots, objectSet, err := resolveGitObjectArchiveRoots(repoPath, rootOIDs)
	if err != nil {
		return nil, nil, "", err
	}
	objectIDs := make([]string, 0, len(objectSet))
	for oid := range objectSet {
		objectIDs = append(objectIDs, oid)
	}
	sort.Strings(objectIDs)
	return roots, objectIDs, objectFormat, nil
}

func resolveGitObjectFormat(repoPath string) (string, error) {
	if strings.TrimSpace(repoPath) == "" {
		return "", fmt.Errorf("git object archive repository path is empty")
	}
	objectFormatBytes, err := runGitBinary(repoPath, nil, "rev-parse", "--show-object-format")
	if err != nil {
		return "", err
	}
	objectFormat := strings.TrimSpace(string(objectFormatBytes))
	if objectFormat == "" {
		return "", fmt.Errorf("git object archive object format is empty")
	}
	return objectFormat, nil
}

func resolveGitObjectArchiveRoots(repoPath string, rootOIDs []string) ([]GitObjectArchiveRoot, map[string]struct{}, error) {
	rootSet := make(map[string]GitObjectArchiveRoot, len(rootOIDs))
	objectSet := make(map[string]struct{})
	for _, rawOID := range rootOIDs {
		root, err := resolveGitObjectArchiveRoot(repoPath, rawOID)
		if err != nil {
			return nil, nil, err
		}
		if existing, ok := rootSet[root.OID]; ok && existing.Type != root.Type {
			return nil, nil, fmt.Errorf("git object archive root %s has conflicting object type", root.OID)
		}
		rootSet[root.OID] = root
		if err := collectGitRootClosure(repoPath, root, objectSet); err != nil {
			return nil, nil, err
		}
	}
	if len(rootSet) == 0 || len(objectSet) == 0 {
		return nil, nil, fmt.Errorf("git object archive requires at least one root")
	}
	roots := make([]GitObjectArchiveRoot, 0, len(rootSet))
	for _, root := range rootSet {
		roots = append(roots, root)
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].OID == roots[j].OID {
			return roots[i].Type < roots[j].Type
		}
		return roots[i].OID < roots[j].OID
	})
	return roots, objectSet, nil
}

func resolveGitObjectArchiveRoot(repoPath, rawOID string) (GitObjectArchiveRoot, error) {
	oid := strings.TrimSpace(rawOID)
	if oid == "" {
		return GitObjectArchiveRoot{}, fmt.Errorf("git object archive root is empty")
	}
	typeBytes, err := runGitBinary(repoPath, nil, "cat-file", "-t", oid)
	if err != nil {
		return GitObjectArchiveRoot{}, fmt.Errorf("resolve git object archive root %s: %w", oid, err)
	}
	return GitObjectArchiveRoot{OID: oid, Type: strings.TrimSpace(string(typeBytes))}, nil
}

func collectGitRootClosure(repoPath string, root GitObjectArchiveRoot, objectSet map[string]struct{}) error {
	objectSet[root.OID] = struct{}{}
	switch root.Type {
	case gitCommitObjectType, "tag":
		output, err := runGitBinary(repoPath, nil, "rev-list", "--objects", "--no-object-names", root.OID)
		if err != nil {
			return fmt.Errorf("walk git object archive root %s: %w", root.OID, err)
		}
		addGitObjectLines(objectSet, output)
		return nil
	case gitTreeObjectType:
		return collectGitTreeClosure(repoPath, root.OID, objectSet)
	case "blob":
		return nil
	default:
		return fmt.Errorf("git object archive root %s has unsupported object type %q", root.OID, root.Type)
	}
}

func collectGitTreeClosure(repoPath, rootOID string, objectSet map[string]struct{}) error {
	output, err := runGitBinary(repoPath, nil, "ls-tree", "-r", "-t", "--full-tree", rootOID)
	if err != nil {
		return fmt.Errorf("walk git tree archive root %s: %w", rootOID, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(strings.SplitN(line, "\t", 2)[0])
		if len(fields) < 3 {
			return fmt.Errorf("parse git tree archive root %s", rootOID)
		}
		if fields[0] == "160000" && fields[1] == gitCommitObjectType {
			continue
		}
		objectSet[fields[2]] = struct{}{}
	}
	return nil
}

func addGitObjectLines(objectSet map[string]struct{}, output []byte) {
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 0 {
			objectSet[fields[0]] = struct{}{}
		}
	}
}

func validateGitObjectArchiveEnvelope(ref EvidenceObjectRef, envelope gitObjectArchiveEnvelope) error {
	if envelope.SchemaVersion != evidenceSchemaVersion || strings.TrimSpace(envelope.ObjectFormat) == "" || len(envelope.Roots) == 0 || len(envelope.Pack) == 0 {
		return gitArchiveIntegrityError(ref, "git object archive identity is incomplete")
	}
	if digestBytes(envelope.Pack) != envelope.PackDigest {
		return gitArchiveIntegrityError(ref, "git object archive pack digest does not match payload")
	}
	canonical := append([]GitObjectArchiveRoot(nil), envelope.Roots...)
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].OID == canonical[j].OID {
			return canonical[i].Type < canonical[j].Type
		}
		return canonical[i].OID < canonical[j].OID
	})
	seen := make(map[string]struct{}, len(canonical))
	for index, root := range canonical {
		if strings.TrimSpace(root.OID) == "" || strings.TrimSpace(root.Type) == "" {
			return gitArchiveIntegrityError(ref, "git object archive root identity is incomplete")
		}
		if _, exists := seen[root.OID]; exists {
			return gitArchiveIntegrityError(ref, "git object archive contains duplicate roots")
		}
		seen[root.OID] = struct{}{}
		if envelope.Roots[index] != root {
			return gitArchiveIntegrityError(ref, "git object archive roots are not canonical")
		}
	}
	return nil
}

func verifyGitObjectPack(envelope gitObjectArchiveEnvelope) error {
	repo, err := os.MkdirTemp("", "controller-evidence-git-archive-")
	if err != nil {
		return fmt.Errorf("create git archive verification repository: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(repo)
	}()
	if _, err := runGitBinary("", nil, "init", "--bare", "--quiet", "--object-format="+envelope.ObjectFormat, repo); err != nil {
		return err
	}
	if _, err := runGitBinaryWithGitDir(repo, envelope.Pack, "index-pack", "--stdin"); err != nil {
		return fmt.Errorf("index git object archive pack: %w", err)
	}
	for _, root := range envelope.Roots {
		typeBytes, err := runGitBinaryWithGitDir(repo, nil, "cat-file", "-t", root.OID)
		if err != nil {
			if _, probeErr := runGitBinaryWithGitDir(repo, nil, "cat-file", "-e", root.OID); probeErr != nil {
				return fmt.Errorf("git object archive root %s is missing", root.OID)
			}
			return fmt.Errorf("read git object archive root %s: %w", root.OID, err)
		}
		if strings.TrimSpace(string(typeBytes)) != root.Type {
			return fmt.Errorf("git object archive root %s has wrong object type", root.OID)
		}
	}
	args := []string{"fsck", "--connectivity-only", "--no-reflogs", "--no-dangling"}
	for _, root := range envelope.Roots {
		args = append(args, root.OID)
	}
	if _, err := runGitBinaryWithGitDir(repo, nil, args...); err != nil {
		return fmt.Errorf("git object archive closure is incomplete: %w", err)
	}
	return nil
}

func runGitBinary(repoPath string, stdin []byte, args ...string) ([]byte, error) {
	commandArgs := append([]string(nil), args...)
	if repoPath != "" {
		commandArgs = append([]string{"-C", repoPath}, commandArgs...)
	}
	return runGitCommand(stdin, commandArgs...)
}

func runGitBinaryWithGitDir(gitDir string, stdin []byte, args ...string) ([]byte, error) {
	commandArgs := append([]string{"--git-dir", gitDir}, args...)
	return runGitCommand(stdin, commandArgs...)
}

func runGitCommand(stdin []byte, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return output, fmt.Errorf("git %v: %w: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

func gitArchiveIntegrityError(ref EvidenceObjectRef, reason string) error {
	return &EvidenceIntegrityError{Digest: ref.Digest, Reason: reason}
}
