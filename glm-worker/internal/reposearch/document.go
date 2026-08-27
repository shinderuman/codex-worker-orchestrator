package reposearch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
)

type doc struct {
	Path          string         `json:"path"`
	ContentLength int            `json:"content_length"`
	PathLength    int            `json:"path_length"`
	ContentTF     map[string]int `json:"content_tf"`
	PathTF        map[string]int `json:"path_tf"`
}

type builtIndex struct {
	docs         []doc
	indexed      int
	indexedBytes int
	skipped      int
}

type readOutcome int

const (
	maxFileBytes     = 1 << 20
	binarySniffBytes = 8 << 10
)

const (
	readIndexed readOutcome = iota
	readSkipped
	readMissing
)

func rebuildIndex(ctx context.Context, repoRoot string, settings searchSettings) (builtIndex, error) {
	paths, err := enumerateFiles(ctx, repoRoot, settings.excludeDirs)
	if err != nil {
		return builtIndex{}, err
	}
	if len(paths) > settings.maxFiles {
		return builtIndex{}, fmt.Errorf("%w: 対象file数 %d がMaxFiles %dを超えています", ErrIndexLimit, len(paths), settings.maxFiles)
	}
	index := builtIndex{docs: make([]doc, 0, len(paths))}
	totalBytes := 0
	for _, rel := range paths {
		abs, err := joinWithinRoot(repoRoot, rel)
		if err != nil {
			return builtIndex{}, err
		}
		content, outcome, err := readSearchableFile(abs)
		if err != nil {
			return builtIndex{}, err
		}
		if outcome != readIndexed {
			index.skipped++
			continue
		}
		totalBytes += len(content)
		if totalBytes > settings.maxTotalBytes {
			return builtIndex{}, fmt.Errorf("%w: 読み込み合計 %d bytes がMaxTotalBytes %dを超えています", ErrIndexLimit, totalBytes, settings.maxTotalBytes)
		}
		contentTokens := tokenize(string(content))
		pathTokens := tokenize(rel)
		index.docs = append(index.docs, doc{
			Path:          rel,
			ContentLength: len(contentTokens),
			PathLength:    len(pathTokens),
			ContentTF:     termFrequencies(contentTokens),
			PathTF:        termFrequencies(pathTokens),
		})
	}
	index.indexed = len(index.docs)
	index.indexedBytes = totalBytes
	return index, nil
}

func readSearchableFile(abs string) ([]byte, readOutcome, error) {
	info, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, readMissing, nil
	}
	if err != nil {
		return nil, readSkipped, fmt.Errorf("%sをstatできません: %w", abs, err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, readSkipped, nil
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, readSkipped, fmt.Errorf("%sを読めません: %w", abs, err)
	}
	if bytes.IndexByte(content[:min(len(content), binarySniffBytes)], 0) >= 0 {
		return nil, readSkipped, nil
	}
	return content, readIndexed, nil
}
