package reposearch

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type CacheStatus string

type Result struct {
	Path         string
	Score        float64
	ContentScore float64
	PathScore    float64
	Line         int
	Snippet      string
}

type Report struct {
	Results      []Result
	CacheStatus  CacheStatus
	Warnings     []string
	IndexedFiles int
	SkippedFiles int
	Candidates   int
}

type Options struct {
	CacheRoot string

	DisableCache bool

	MaxResults int

	MaxFiles int

	MaxTotalBytes int

	PathWeight *float64

	ExcludeDirs []string

	PathPrefixes []string

	Symbols []string
}

type searchSettings struct {
	cacheRoot     string
	cacheDisabled bool
	limit         int
	maxFiles      int
	maxTotalBytes int
	pathWeight    float64
	excludeDirs   map[string]bool
	pathPrefixes  []string
	symbols       []string
}

const (
	defaultMaxResults    = 20
	hardMaxResults       = 100
	defaultMaxFiles      = 50_000
	hardMaxFiles         = 50_000
	defaultMaxTotalBytes = 256 << 20
	hardMaxTotalBytes    = 256 << 20
	defaultPathWeight    = 0.5
	maxSearchAttempts    = 2
)

const (
	CacheStatusHit          CacheStatus = "hit"
	CacheStatusRebuilt      CacheStatus = "rebuilt"
	CacheStatusWriteWarning CacheStatus = "write-warning"
)

var (
	ErrEmptyQuery     = errors.New("queryをtoken化しても空です")
	ErrIndexRace      = errors.New("検索中にrepository状態が変化しました")
	ErrInvalidOptions = errors.New("reposearchのOptionsが不正です")
	ErrIndexLimit     = errors.New("検索対象がOptions上限を超えています")
)

func Search(ctx context.Context, repoRoot string, query string, opts Options) (Report, error) {
	settings, err := resolveSettings(opts)
	if err != nil {
		return Report{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return Report{}, ErrEmptyQuery
	}
	root, err := resolveCanonicalRoot(ctx, repoRoot)
	if err != nil {
		return Report{}, err
	}
	for attempt := 0; attempt < maxSearchAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		report, raced, err := attemptSearch(ctx, root, queryTokens, settings)
		if err != nil {
			return Report{}, err
		}
		if !raced {
			return report, nil
		}
	}
	return Report{}, ErrIndexRace
}

func resolveSettings(opts Options) (searchSettings, error) {
	limit, err := resolveBound(opts.MaxResults, defaultMaxResults, hardMaxResults, "MaxResults")
	if err != nil {
		return searchSettings{}, err
	}
	maxFiles, err := resolveBound(opts.MaxFiles, defaultMaxFiles, hardMaxFiles, "MaxFiles")
	if err != nil {
		return searchSettings{}, err
	}
	maxTotalBytes, err := resolveBound(opts.MaxTotalBytes, defaultMaxTotalBytes, hardMaxTotalBytes, "MaxTotalBytes")
	if err != nil {
		return searchSettings{}, err
	}
	pathWeight, err := resolvePathWeight(opts.PathWeight)
	if err != nil {
		return searchSettings{}, err
	}
	excludeDirs, err := resolveExcludeDirs(opts.ExcludeDirs)
	if err != nil {
		return searchSettings{}, err
	}
	pathPrefixes, symbols, err := resolveScopes(opts.PathPrefixes, opts.Symbols)
	if err != nil {
		return searchSettings{}, err
	}
	cacheRoot, err := resolveCacheRoot(opts)
	if err != nil {
		return searchSettings{}, err
	}
	return searchSettings{
		cacheRoot:     cacheRoot,
		cacheDisabled: opts.DisableCache,
		limit:         limit,
		maxFiles:      maxFiles,
		maxTotalBytes: maxTotalBytes,
		pathWeight:    pathWeight,
		excludeDirs:   excludeDirs,
		pathPrefixes:  pathPrefixes,
		symbols:       symbols,
	}, nil
}

func resolveScopes(pathPrefixes []string, symbols []string) ([]string, []string, error) {
	resolvedPrefixes := make([]string, 0, len(pathPrefixes))
	for _, prefix := range pathPrefixes {
		clean := filepath.ToSlash(filepath.Clean(prefix))
		if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return nil, nil, fmt.Errorf("%w: PathPrefixesはrepository相対のpath prefixを指定してください: %q", ErrInvalidOptions, prefix)
		}
		resolvedPrefixes = append(resolvedPrefixes, clean)
	}
	resolved := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		tokens := tokenize(symbol)
		if len(tokens) == 0 {
			return nil, nil, fmt.Errorf("%w: Symbolsは識別子として有効な値を指定してください: %q", ErrInvalidOptions, symbol)
		}
		resolved = append(resolved, tokens...)
	}
	return resolvedPrefixes, resolved, nil
}

func resolvePathWeight(requested *float64) (float64, error) {
	if requested == nil {
		return defaultPathWeight, nil
	}
	if *requested < 0 || math.IsNaN(*requested) || math.IsInf(*requested, 0) {
		return 0, fmt.Errorf("%w: PathWeightは0以上の有限値を指定してください: %v", ErrInvalidOptions, *requested)
	}
	return *requested, nil
}

func resolveCacheRoot(opts Options) (string, error) {
	if opts.DisableCache && opts.CacheRoot != "" {
		return "", fmt.Errorf("%w: DisableCacheとCacheRootは同時指定できません", ErrInvalidOptions)
	}
	if opts.CacheRoot != "" || opts.DisableCache {
		return opts.CacheRoot, nil
	}
	return defaultCacheRoot()
}

func resolveBound(requested, defaultValue, hardCap int, name string) (int, error) {
	switch {
	case requested == 0:
		return defaultValue, nil
	case requested < 0 || requested > hardCap:
		return 0, fmt.Errorf("%w: %sは0..%dで指定してください: %d", ErrInvalidOptions, name, hardCap, requested)
	default:
		return requested, nil
	}
}

func defaultCacheRoot() (string, error) {
	if home := os.Getenv("GLM_WORKER_HOME"); home != "" {
		return filepath.Join(home, "search"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("既定cache rootを解決できません: %w", err)
	}
	return filepath.Join(home, ".glm-worker", "search"), nil
}

func attemptSearch(ctx context.Context, root string, queryTokens []string, settings searchSettings) (Report, bool, error) {
	before, err := computeFingerprint(ctx, root, settings.excludeDirs)
	if err != nil {
		return Report{}, false, err
	}
	index, hit := loadIndex(settings, root, before)
	if !hit {
		index, err = rebuildIndex(ctx, root, settings)
		if err != nil {
			return Report{}, false, err
		}
	}
	raced, err := fingerprintUnchanged(ctx, root, settings.excludeDirs, before)
	if err != nil || raced {
		return Report{}, raced, err
	}
	scoped := scopeDocuments(index.docs, settings)
	results, candidates := rankDocuments(scoped, queryTokens, settings.limit, settings.pathWeight)
	warnings := attachSnippets(root, results, queryTokens)
	raced, err = fingerprintUnchanged(ctx, root, settings.excludeDirs, before)
	if err != nil || raced {
		return Report{}, raced, err
	}
	status := CacheStatusRebuilt
	if hit {
		status = CacheStatusHit
	} else if !settings.cacheDisabled {
		if writeErr := writeIndex(settings, root, before, index); writeErr != nil {
			status = CacheStatusWriteWarning
			warnings = append(warnings, fmt.Sprintf("cacheを書き込めません: %v", writeErr))
		}
	}
	return Report{
		Results:      results,
		CacheStatus:  status,
		Warnings:     warnings,
		IndexedFiles: index.indexed,
		SkippedFiles: index.skipped,
		Candidates:   candidates,
	}, false, nil
}

func scopeDocuments(docs []doc, settings searchSettings) []doc {
	if len(settings.pathPrefixes) == 0 && len(settings.symbols) == 0 {
		return docs
	}
	scoped := make([]doc, 0, len(docs))
	for _, entry := range docs {
		if documentInScope(entry, settings) {
			scoped = append(scoped, entry)
		}
	}
	return scoped
}

func documentInScope(entry doc, settings searchSettings) bool {
	for _, prefix := range settings.pathPrefixes {
		if pathHasPrefix(entry.Path, prefix) {
			return true
		}
	}
	for _, symbol := range settings.symbols {
		if entry.ContentTF[symbol] > 0 {
			return true
		}
	}
	return false
}

func pathHasPrefix(path string, prefix string) bool {
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/")
}

func resolveCanonicalRoot(ctx context.Context, repoRoot string) (string, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", fmt.Errorf("repo rootを絶対pathへ解決できません: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("repo rootを解決できません: %w", err)
	}
	output, err := gitOutput(ctx, canonical, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("git repositoryではありません: %s: %w", canonical, err)
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(output)))
	if err != nil {
		return "", fmt.Errorf("git toplevelを解決できません: %w", err)
	}
	return filepath.Clean(root), nil
}
