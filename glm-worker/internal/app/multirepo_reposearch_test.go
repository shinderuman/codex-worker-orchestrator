//go:build unix

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
)

// TestMultiRepositorySearchCacheProcessIsolationは共有GLM_WORKER_HOME上のrepo-search cacheが
// repo別namespaceへ分離されることを、独立2 processの並列実行で検証する。glm-worker CLI
// 本体はまだsearchをproduct配線していないため、子processは将来のproduct呼出と同じ
// reposearch APIを既定cache root(GLM_WORKER_HOME配下のsearch)に対して実行する。
func TestMultiRepositorySearchCacheProcessIsolation(t *testing.T) {
	if os.Getenv("GLM_WORKER_REPOSEARCH_HELPER") == "1" {
		multiRepoSearchHelper()
		return
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}

	root := t.TempDir()
	home := filepath.Join(root, "glm-home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	repoA := newMultiRepoGitRepo(t, filepath.Join(root, "repo-a"), "mrsearchalpha")
	repoB := newMultiRepoGitRepo(t, filepath.Join(root, "repo-b"), "mrsearchbeta")

	var reportA, reportB multiRepoSearchReport
	var searchErrA, searchErrB error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		reportA, searchErrA = runMultiRepoSearchChild(home, repoA, "mrsearchalpha")
	}()
	go func() {
		defer wait.Done()
		reportB, searchErrB = runMultiRepoSearchChild(home, repoB, "mrsearchbeta")
	}()
	wait.Wait()
	if searchErrA != nil || searchErrB != nil {
		t.Fatalf("並列検索子processが失敗しました: %v / %v", searchErrA, searchErrB)
	}

	assertSearchReportIsolated(t, reportA, "mrsearchalpha", "mrsearchbeta")
	assertSearchReportIsolated(t, reportB, "mrsearchbeta", "mrsearchalpha")

	cacheRoot := filepath.Join(home, "search")
	entries, err := os.ReadDir(cacheRoot)
	if err != nil || len(entries) != 2 {
		t.Fatalf("共有cache rootへrepo別dirが2つできていません: err=%v entries=%d", err, len(entries))
	}

	reused, reuseErr := runMultiRepoSearchChild(home, repoA, "mrsearchalpha")
	if reuseErr != nil {
		t.Fatalf("同一repoの2回目検索が失敗しました: %v", reuseErr)
	}
	if reused.CacheStatus != string(reposearch.CacheStatusHit) {
		t.Fatalf("同一repoの2回目検索がcache hitになりません: %s", reused.CacheStatus)
	}
	assertSearchReportIsolated(t, reused, "mrsearchalpha", "mrsearchbeta")
}

// multiRepoSearchReportは子processの検索結果。Report全体へtest側の期待を課さず、
// 非混入検査に必要な構造値だけをやり取りする。
type multiRepoSearchReport struct {
	Error       string   `json:"error,omitempty"`
	CacheStatus string   `json:"cache_status"`
	Paths       []string `json:"paths"`
	Snippets    []string `json:"snippets"`
}

// runMultiRepoSearchChildはtest binaryを子processとして再実行し、共有homeの既定cache
// rootへ検索を実行させる。呼出元のtest環境envを継承しない。test本体goroutineからは
// 並列起動されるため、失敗はerrorとして返す。
func runMultiRepoSearchChild(home string, repo string, query string) (multiRepoSearchReport, error) {
	outDir, err := os.MkdirTemp("", "glm-worker-search-child-*")
	if err != nil {
		return multiRepoSearchReport{}, err
	}
	defer os.RemoveAll(outDir)
	outPath := filepath.Join(outDir, "search-report.json")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestMultiRepositorySearchCacheProcessIsolation")
	command.Dir = repo
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + home,
		"GLM_WORKER_HOME=" + home,
		"GLM_WORKER_REPOSEARCH_HELPER=1",
		"GLM_WORKER_SEARCH_REPO=" + repo,
		"GLM_WORKER_SEARCH_QUERY=" + query,
		"GLM_WORKER_SEARCH_OUT=" + outPath,
	}
	if output, err := command.CombinedOutput(); err != nil {
		return multiRepoSearchReport{}, fmt.Errorf("検索子process失敗(%v): %s", err, output)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return multiRepoSearchReport{}, err
	}
	var report multiRepoSearchReport
	if err := json.Unmarshal(data, &report); err != nil {
		return multiRepoSearchReport{}, fmt.Errorf("検索子processの結果を解析できません: %v: %s", err, data)
	}
	if report.Error != "" {
		return multiRepoSearchReport{}, fmt.Errorf("検索失敗: %s", report.Error)
	}
	return report, nil
}

// assertSearchReportIsolatedは検索結果が自repoのmarker語だけを含み、相手repoのmarker語が
// path・snippetのどこにも現れないことを検査する。
func assertSearchReportIsolated(t *testing.T, report multiRepoSearchReport, own string, other string) {
	t.Helper()
	if len(report.Paths) == 0 {
		t.Fatalf("marker %s の検索結果が空です: %+v", own, report)
	}
	for _, path := range report.Paths {
		if path != "corpus.md" {
			t.Fatalf("自repoのcorpus以外が検索結果へ入っています: %s", path)
		}
	}
	joined := strings.Join(report.Snippets, "\n") + "\n" + strings.Join(report.Paths, "\n")
	if !strings.Contains(joined, own) {
		t.Fatalf("検索結果が自repoのmarker %s を含みません: %+v", own, report)
	}
	if strings.Contains(joined, other) {
		t.Fatalf("検索結果へ相手repoのmarker %s が混入しています: %+v", other, report)
	}
}

// multiRepoSearchHelperは子process側本体。envで指定されたrepo・queryでproductionと同じ
// reposearch.Searchを既定cache rootに対して実行し、結果をJSON fileへ書く。
func multiRepoSearchHelper() {
	code := multiRepoSearchHelperRun()
	os.Exit(code)
}

func multiRepoSearchHelperRun() int {
	outPath := os.Getenv("GLM_WORKER_SEARCH_OUT")
	repo := os.Getenv("GLM_WORKER_SEARCH_REPO")
	query := os.Getenv("GLM_WORKER_SEARCH_QUERY")
	if outPath == "" || repo == "" || query == "" {
		return 2
	}
	report, err := reposearch.Search(context.Background(), repo, query, reposearch.Options{})
	result := multiRepoSearchReport{CacheStatus: string(report.CacheStatus)}
	if err != nil {
		result.Error = err.Error()
	}
	for _, item := range report.Results {
		result.Paths = append(result.Paths, item.Path)
		result.Snippets = append(result.Snippets, item.Snippet)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return 2
	}
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return 2
	}
	return 0
}
