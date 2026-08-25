package reposearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
)

var captureFingerprint = captureRepositoryFingerprint

type fingerprint struct {
	IndexDigest    string
	WorktreeDigest string
}

func computeFingerprint(ctx context.Context, repoRoot string, excludeDirs map[string]bool) (fingerprint, error) {
	fp, err := captureFingerprint(ctx, repoRoot, excludeDirs)
	if err != nil {
		return fingerprint{}, fmt.Errorf("repository状態のfingerprintを取得できません: %w", err)
	}
	return fp, nil
}

func captureRepositoryFingerprint(ctx context.Context, repoRoot string, excludeDirs map[string]bool) (fingerprint, error) {
	indexDigest, err := fingerprintIndexDigest(ctx, repoRoot, excludeDirs)
	if err != nil {
		return fingerprint{}, err
	}
	worktreeDigest, err := fingerprintWorktreeDigest(ctx, repoRoot, excludeDirs)
	if err != nil {
		return fingerprint{}, err
	}
	return fingerprint{IndexDigest: indexDigest, WorktreeDigest: worktreeDigest}, nil
}

func fingerprintIndexDigest(ctx context.Context, repoRoot string, excludeDirs map[string]bool) (string, error) {
	entries, err := trackedFileEntries(ctx, repoRoot)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	for _, entry := range entries {
		if excludedPath(entry.path, excludeDirs) {
			continue
		}
		hasher.Write([]byte(entry.mode))
		hasher.Write([]byte{0})
		hasher.Write([]byte(entry.sha))
		hasher.Write([]byte{0})
		hasher.Write([]byte(entry.path))
		hasher.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func fingerprintWorktreeDigest(ctx context.Context, repoRoot string, excludeDirs map[string]bool) (string, error) {
	args := append([]string{"diff", "--no-ext-diff", "--no-textconv", "--", "."}, excludePathspecs(excludeDirs)...)
	diffOutput, err := gitOutput(ctx, repoRoot, args...)
	if err != nil {
		return "", err
	}
	untracked, err := untrackedFilePaths(ctx, repoRoot)
	if err != nil {
		return "", err
	}
	return buildWorktreeDigest(diffOutput, untracked, repoRoot, excludeDirs)
}

func excludePathspecs(excludeDirs map[string]bool) []string {
	names := sortedExcludeDirs(excludeDirs)
	pathspecs := make([]string, 0, len(names))
	for _, name := range names {
		pathspecs = append(pathspecs, ":(exclude,literal)"+name+"/")
	}
	return pathspecs
}

func buildWorktreeDigest(diffOutput []byte, untrackedPaths []string, repoRoot string, excludeDirs map[string]bool) (string, error) {
	hasher := sha256.New()
	hasher.Write([]byte("diff\n"))
	hasher.Write(diffOutput)
	hasher.Write([]byte("\nuntracked\n"))

	sort.Strings(untrackedPaths)
	for _, path := range untrackedPaths {
		if path == "" || excludedPath(path, excludeDirs) {
			continue
		}
		absPath, err := joinWithinRoot(repoRoot, path)
		if err != nil {
			return "", fmt.Errorf("untracked %s: %w", path, err)
		}
		hasher.Write([]byte(path))
		hasher.Write([]byte{0})
		if err := hashUntrackedProjection(hasher, absPath); err != nil {
			return "", err
		}
		hasher.Write([]byte("\n"))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func hashUntrackedProjection(hasher hash.Hash, absPath string) error {
	content, outcome, err := readSearchableFile(absPath)
	if err != nil {
		return err
	}
	switch outcome {
	case readMissing:
		return fmt.Errorf("untracked file %sが列挙後に消失しました", absPath)
	case readSkipped:
		hasher.Write([]byte("skipped\x00"))
	case readIndexed:
		sum := sha256.Sum256(content)
		hasher.Write([]byte("indexed\x00"))
		hasher.Write([]byte(hex.EncodeToString(sum[:])))
	}
	return nil
}

func fingerprintUnchanged(ctx context.Context, root string, excludeDirs map[string]bool, before fingerprint) (bool, error) {
	after, err := computeFingerprint(ctx, root, excludeDirs)
	if err != nil {
		return false, err
	}
	return after != before, nil
}
