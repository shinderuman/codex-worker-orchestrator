package reposearch

import (
	"context"
	"fmt"
)

type ExhaustiveMatch struct {
	Path string
}

type ExhaustiveReport struct {
	Predicate       string
	QueryTokens     []string
	EnumeratedFiles int
	ScannedFiles    int
	SkippedFiles    int
	Matches         []ExhaustiveMatch
}

type ExhaustiveOptions struct {
	MaxFiles      int
	MaxTotalBytes int
	MaxMatches    int
	ExcludeDirs   []string
}

const (
	defaultMaxExhaustiveMatches = 512
	hardMaxExhaustiveMatches    = 4096
	exhaustivePredicate         = "any-normalized-query-token-in-path-or-text"
)

func ExhaustiveSearch(ctx context.Context, repoRoot string, query string, opts ExhaustiveOptions) (ExhaustiveReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	queryTokens := uniqueTokenOrder(tokenize(query))
	if len(queryTokens) == 0 {
		return ExhaustiveReport{}, ErrEmptyQuery
	}
	maxFiles, err := resolveBound(opts.MaxFiles, defaultMaxFiles, hardMaxFiles, "MaxFiles")
	if err != nil {
		return ExhaustiveReport{}, err
	}
	maxTotalBytes, err := resolveBound(opts.MaxTotalBytes, defaultMaxTotalBytes, hardMaxTotalBytes, "MaxTotalBytes")
	if err != nil {
		return ExhaustiveReport{}, err
	}
	maxMatches, err := resolveBound(opts.MaxMatches, defaultMaxExhaustiveMatches, hardMaxExhaustiveMatches, "MaxMatches")
	if err != nil {
		return ExhaustiveReport{}, err
	}
	excludeDirs, err := resolveExcludeDirs(opts.ExcludeDirs)
	if err != nil {
		return ExhaustiveReport{}, err
	}
	root, err := resolveCanonicalRoot(ctx, repoRoot)
	if err != nil {
		return ExhaustiveReport{}, err
	}
	before, err := computeFingerprint(ctx, root, excludeDirs)
	if err != nil {
		return ExhaustiveReport{}, err
	}
	paths, err := enumerateFiles(ctx, root, excludeDirs)
	if err != nil {
		return ExhaustiveReport{}, err
	}
	if len(paths) > maxFiles {
		return ExhaustiveReport{}, fmt.Errorf("%w: 対象file数 %d がMaxFiles %dを超えています", ErrIndexLimit, len(paths), maxFiles)
	}
	report := ExhaustiveReport{
		Predicate:       exhaustivePredicate,
		QueryTokens:     append([]string(nil), queryTokens...),
		EnumeratedFiles: len(paths),
		Matches:         make([]ExhaustiveMatch, 0),
	}
	totalBytes := 0
	for _, rel := range paths {
		if err := ctx.Err(); err != nil {
			return ExhaustiveReport{}, err
		}
		abs, err := joinWithinRoot(root, rel)
		if err != nil {
			return ExhaustiveReport{}, err
		}
		content, outcome, err := readSearchableFile(abs)
		if err != nil {
			return ExhaustiveReport{}, err
		}
		if outcome != readIndexed {
			report.SkippedFiles++
			continue
		}
		totalBytes += len(content)
		if totalBytes > maxTotalBytes {
			return ExhaustiveReport{}, fmt.Errorf("%w: 読み込み合計 %d bytes がMaxTotalBytes %dを超えています", ErrIndexLimit, totalBytes, maxTotalBytes)
		}
		report.ScannedFiles++
		if exhaustiveDocumentMatches(rel, string(content), queryTokens) {
			report.Matches = append(report.Matches, ExhaustiveMatch{Path: rel})
			if len(report.Matches) > maxMatches {
				return ExhaustiveReport{}, fmt.Errorf("%w: exhaustive match数がMaxMatches %dを超えています", ErrIndexLimit, maxMatches)
			}
		}
	}
	raced, err := fingerprintUnchanged(ctx, root, excludeDirs, before)
	if err != nil {
		return ExhaustiveReport{}, err
	}
	if raced {
		return ExhaustiveReport{}, ErrIndexRace
	}
	return report, nil
}

func uniqueTokenOrder(tokens []string) []string {
	seen := make(map[string]struct{}, len(tokens))
	result := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		result = append(result, token)
	}
	return result
}

func exhaustiveDocumentMatches(path, content string, queryTokens []string) bool {
	pathTerms := termFrequencies(tokenize(path))
	contentTerms := termFrequencies(tokenize(content))
	for _, token := range queryTokens {
		if pathTerms[token] > 0 || contentTerms[token] > 0 {
			return true
		}
	}
	return false
}
