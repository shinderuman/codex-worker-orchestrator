package parentevidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type Manifest struct {
	Version     int                 `json:"version"`
	Reason      string              `json:"reason"`
	Authority   []AuthorityRequest  `json:"authority"`
	Handoff     *HandoffRequest     `json:"handoff"`
	Status      *StatusRequest      `json:"status"`
	Validations *ValidationsRequest `json:"validations"`
	Telemetry   *TelemetryRequest   `json:"telemetry"`
	Search      []SearchRequest     `json:"search"`
	Diff        []DiffRequest       `json:"diff"`
	Source      []SourceRequest     `json:"source"`
}

type AuthorityRequest struct {
	Kind               string `json:"kind"`
	KnownContentSHA256 string `json:"known_content_sha256"`
	BudgetBytes        int    `json:"budget_bytes"`
}

type HandoffRequest struct {
	KnownDigest string `json:"known_digest"`
	Force       bool   `json:"force"`
}

type StatusRequest struct{}

type ValidationsRequest struct{}

type TelemetryRequest struct{}

type SearchRequest struct {
	Question    string   `json:"question"`
	Scopes      []string `json:"scopes"`
	BudgetBytes int      `json:"budget_bytes"`
}

type DiffRequest struct {
	Question    string   `json:"question"`
	Paths       []string `json:"paths"`
	BudgetBytes int      `json:"budget_bytes"`
}

type SourceRequest struct {
	Question    string `json:"question"`
	Path        string `json:"path"`
	LineStart   int    `json:"line_start"`
	LineEnd     int    `json:"line_end"`
	BudgetBytes int    `json:"budget_bytes"`
}

type Output struct {
	Version     int                         `json:"version"`
	Status      string                      `json:"status"`
	OwnerCallID string                      `json:"owner_call_id"`
	Reason      string                      `json:"reason"`
	TaskID      string                      `json:"task_id,omitempty"`
	TaskStatus  string                      `json:"task_status,omitempty"`
	Parts       []Part                      `json:"parts"`
	Summary     state.ParentEvidenceSummary `json:"evidence_summary"`
}

type Part struct {
	Kind        string          `json:"kind"`
	Detail      string          `json:"detail,omitempty"`
	Status      string          `json:"status"`
	Digest      string          `json:"digest,omitempty"`
	Bytes       int             `json:"bytes"`
	TokenProxy  int             `json:"token_proxy"`
	Reason      string          `json:"reason,omitempty"`
	Locator     string          `json:"locator,omitempty"`
	Authority   *AuthorityBody  `json:"authority,omitempty"`
	Handoff     json.RawMessage `json:"handoff,omitempty"`
	StatusRead  json.RawMessage `json:"status_read,omitempty"`
	Validations []Validation    `json:"validations,omitempty"`
	Telemetry   *TelemetryBody  `json:"telemetry,omitempty"`
	Search      *SearchBody     `json:"search,omitempty"`
	Diff        *DiffBody       `json:"diff,omitempty"`
	Source      *SourceBody     `json:"source,omitempty"`

	Surface string `json:"-"`
}

type AuthorityBody struct {
	Kind           string `json:"kind"`
	SnapshotSHA256 string `json:"authority_snapshot_sha256"`
	ActiveTask     string `json:"active_task"`
	ContentSHA256  string `json:"content_sha256"`
	Content        string `json:"content,omitempty"`
}

type Validation struct {
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	Status          string `json:"status"`
	WorkingDir      string `json:"working_dir"`
	Log             string `json:"log,omitempty"`
	Head            string `json:"head"`
	IndexDigest     string `json:"index_digest"`
	WorktreeDigest  string `json:"worktree_digest"`
}

type TelemetryBody struct {
	Records    int                         `json:"records"`
	ModelCalls int                         `json:"model_calls"`
	Summary    state.ParentEvidenceSummary `json:"summary"`
}

type SearchResult struct {
	Path  string  `json:"path"`
	Line  int     `json:"line"`
	Score float64 `json:"score"`
}

type SearchBody struct {
	Question    string         `json:"question"`
	Scopes      []string       `json:"scopes"`
	Candidates  int            `json:"candidates"`
	ResultCount int            `json:"result_count"`
	Results     []SearchResult `json:"results"`
}

type DiffFile struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	HeadBlob    string `json:"head_blob"`
	IndexBlob   string `json:"index_blob"`
	WorktreeSHA string `json:"worktree_sha256"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
}

type DiffBody struct {
	Question string     `json:"question"`
	Paths    []string   `json:"paths"`
	Files    []DiffFile `json:"files"`
	Body     string     `json:"body,omitempty"`
}

type SourceBody struct {
	Question  string `json:"question"`
	Path      string `json:"path"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Content   string `json:"content,omitempty"`
}

type Providers struct {
	Authority   func(AuthorityRequest) Part
	Handoff     func(HandoffRequest) Part
	Status      func() Part
	Validations func() Part
	Telemetry   func() Part
	Search      func(SearchRequest) Part
}

type Projector struct {
	repoRoot      string
	st            *state.StateStore
	ownerCallID   string
	providers     Providers
	output        Output
	pendingClaims []state.ParentEvidenceLedgerEntry
	leaseEpoch    int64
	leaseErr      error
}

const (
	StatusOK       = "ok"
	StatusRequired = "refinement_required"
	StatusError    = "error"

	PartProjected  = "projected"
	PartUnchanged  = "unchanged"
	PartChanged    = "changed"
	PartUnknown    = "unknown"
	PartRefinement = "refinement_required"
	PartError      = "error"
	PartDisabled   = "disabled"

	ManifestVersion      = 1
	ManifestMaxBytes     = 64 * 1024
	MaxOutputBytes       = 96 * 1024
	MaxDiffPaths         = 32
	MaxSourceLines       = 2000
	MaxBudgetBytes       = 256 * 1024
	SearchMaxBudgetBytes = 64 * 1024
	TelemetryFile        = "parent-evidence.jsonl"
)

var bodyStrippers = []func(*Part) bool{
	func(part *Part) bool {
		return part.Authority != nil && part.Authority.Content != ""
	},
	func(part *Part) bool {
		return len(part.Handoff) > 0
	},
	func(part *Part) bool {
		return len(part.StatusRead) > 0
	},
	func(part *Part) bool {
		return part.Search != nil && len(part.Search.Results) > 0
	},
	func(part *Part) bool {
		return part.Diff != nil && part.Diff.Body != ""
	},
	func(part *Part) bool {
		return part.Source != nil && part.Source.Content != ""
	},
}

func NewProjector(repoRoot string, st *state.StateStore, ownerCallID string, providers Providers) *Projector {
	return &Projector{repoRoot: repoRoot, st: st, ownerCallID: ownerCallID, providers: providers}
}

func (p *Projector) Output() Output {
	return p.output
}

func DecodeManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, machinecli.UsageErrorf("evidence manifestのschemaが不正です: " + err.Error())
	}
	if manifest.Version != ManifestVersion {
		return Manifest{}, machinecli.UsageErrorf("evidence manifestのversionは1だけを受理します")
	}
	if strings.TrimSpace(manifest.Reason) == "" {
		return Manifest{}, machinecli.UsageErrorf("evidence manifestにはreasonが必要です")
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ValidateManifest(manifest Manifest) error {
	if PartCount(manifest) == 0 {
		return machinecli.UsageErrorf("evidence manifestは少なくとも1つのprojection partを指定してください")
	}
	for _, request := range manifest.Authority {
		if err := validateAuthorityPart(request); err != nil {
			return err
		}
	}
	for _, request := range manifest.Search {
		if err := validateSearchPart(request); err != nil {
			return err
		}
	}
	for _, request := range manifest.Diff {
		if err := validateDiffPart(request); err != nil {
			return err
		}
	}
	for _, request := range manifest.Source {
		if err := validateSourcePart(request); err != nil {
			return err
		}
	}
	return nil
}

func PartCount(manifest Manifest) int {
	partCount := len(manifest.Authority) + len(manifest.Search) + len(manifest.Diff) + len(manifest.Source)
	for _, present := range []bool{
		manifest.Handoff != nil,
		manifest.Status != nil,
		manifest.Validations != nil,
		manifest.Telemetry != nil,
	} {
		if present {
			partCount++
		}
	}
	return partCount
}

func validateAuthorityPart(request AuthorityRequest) error {
	if request.Kind != "rules" && request.Kind != "plan" && request.Kind != "active" {
		return machinecli.UsageErrorf("evidence manifestのauthority kindはrules|plan|activeだけを受理します")
	}
	if request.BudgetBytes <= 0 || request.BudgetBytes > MaxBudgetBytes {
		return machinecli.UsageErrorf("evidence manifestのauthority budget_bytesは1..262144で指定してください")
	}
	return nil
}

func validateSearchPart(request SearchRequest) error {
	if strings.TrimSpace(request.Question) == "" || len(request.Scopes) == 0 || request.BudgetBytes <= 0 || request.BudgetBytes > SearchMaxBudgetBytes {
		return machinecli.UsageErrorf("evidence manifestのsearch partはquestion・scopes・budget_bytes(1..65536)を必須とします")
	}
	return nil
}

func validateDiffPart(request DiffRequest) error {
	if strings.TrimSpace(request.Question) == "" || len(request.Paths) == 0 || len(request.Paths) > MaxDiffPaths {
		return machinecli.UsageErrorf("evidence manifestのdiff partはquestionと1..32個のpathsを必須とします")
	}
	if request.BudgetBytes <= 0 || request.BudgetBytes > MaxBudgetBytes {
		return machinecli.UsageErrorf("evidence manifestのdiff budget_bytesは1..262144で指定してください")
	}
	for _, path := range request.Paths {
		if !RelativePath(path) {
			return machinecli.UsageErrorf("evidence manifestのdiff pathsはrepository相対pathだけを受理します: " + path)
		}
	}
	return nil
}

func validateSourcePart(request SourceRequest) error {
	if strings.TrimSpace(request.Question) == "" || !RelativePath(request.Path) {
		return machinecli.UsageErrorf("evidence manifestのsource partはquestionとrepository相対pathを必須とします")
	}
	if request.LineStart < 1 || request.LineEnd < request.LineStart || request.LineEnd-request.LineStart+1 > MaxSourceLines {
		return machinecli.UsageErrorf("evidence manifestのsource行範囲は1..2000行で指定してください")
	}
	if request.BudgetBytes <= 0 || request.BudgetBytes > MaxBudgetBytes {
		return machinecli.UsageErrorf("evidence manifestのsource budget_bytesは1..262144で指定してください")
	}
	return nil
}

func RelativePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return false
	}
	return true
}

func (p *Projector) Project(manifest Manifest) {
	p.leaseEpoch, p.leaseErr = p.st.ParentEvidenceLeaseEpoch()
	p.output = Output{
		Version:     ManifestVersion,
		OwnerCallID: p.ownerCallID,
		Reason:      manifest.Reason,
		TaskID:      p.st.ReadOr("task.id", ""),
		TaskStatus:  string(p.st.TaskStatus()),
		Parts:       []Part{},
	}
	for _, request := range manifest.Authority {
		p.output.Parts = append(p.output.Parts, p.recordProvidedPart(
			p.projectAuthority(request), state.ParentEvidenceSurfaceAuthority+":"+request.Kind,
		))
	}
	if manifest.Handoff != nil {
		p.output.Parts = append(p.output.Parts, p.recordProvidedPart(
			p.projectHandoff(*manifest.Handoff), state.ParentEvidenceSurfaceHandoff,
		))
	}
	if manifest.Status != nil {
		p.output.Parts = append(p.output.Parts, p.recordProvidedPart(p.projectStatus(), state.ParentEvidenceSurfaceStatus))
	}
	if manifest.Validations != nil {
		p.output.Parts = append(p.output.Parts, p.recordProvidedPart(p.projectValidations(), state.ParentEvidenceSurfaceValidations))
	}
	if manifest.Telemetry != nil {
		p.output.Parts = append(p.output.Parts, p.recordProvidedPart(p.projectTelemetry(), state.ParentEvidenceSurfaceEvidenceTelemetry))
	}
	for _, request := range manifest.Search {
		p.output.Parts = append(p.output.Parts, p.recordProvidedPart(p.projectSearch(request), state.ParentEvidenceSurfaceSearch))
	}
	for _, request := range manifest.Diff {
		p.output.Parts = append(p.output.Parts, p.projectDiff(request))
	}
	for _, request := range manifest.Source {
		p.output.Parts = append(p.output.Parts, p.projectSource(request))
	}
	p.output.Status = aggregateStatus(p.output.Parts)
	p.output.Summary = p.evidenceSummary()
}

func (p *Projector) projectAuthority(request AuthorityRequest) Part {
	if p.providers.Authority == nil {
		return Part{Kind: "authority", Detail: request.Kind, Status: PartError, Reason: "authority projection provider is unavailable"}
	}
	return p.providers.Authority(request)
}

func (p *Projector) projectHandoff(request HandoffRequest) Part {
	if p.providers.Handoff == nil {
		return Part{Kind: "handoff", Status: PartError, Reason: "handoff projection provider is unavailable"}
	}
	return p.providers.Handoff(request)
}

func (p *Projector) projectStatus() Part {
	if p.providers.Status == nil {
		return Part{Kind: "status", Detail: "status_read", Status: PartError, Reason: "status projection provider is unavailable"}
	}
	return p.providers.Status()
}

func (p *Projector) projectValidations() Part {
	if p.providers.Validations == nil {
		return Part{Kind: "validations", Status: PartError, Reason: "validations projection provider is unavailable"}
	}
	return p.providers.Validations()
}

func (p *Projector) projectTelemetry() Part {
	if p.providers.Telemetry == nil {
		return Part{Kind: "telemetry", Status: PartError, Reason: "telemetry projection provider is unavailable"}
	}
	return p.providers.Telemetry()
}

func (p *Projector) projectSearch(request SearchRequest) Part {
	if p.providers.Search == nil {
		return Part{Kind: "search", Detail: request.Question, Status: PartError, Reason: "search projection provider is unavailable"}
	}
	return p.providers.Search(request)
}

func (p *Projector) recordProvidedPart(part Part, surface string) Part {
	if part.Surface == "" {
		part.Surface = surface
	}
	return p.recordPart(part, part.Surface)
}

func aggregateStatus(parts []Part) string {
	status := StatusOK
	for _, part := range parts {
		switch part.Status {
		case PartError:
			return StatusError
		case PartRefinement:
			status = StatusRequired
		}
	}
	return status
}

func (p *Projector) evidenceSummary() state.ParentEvidenceSummary {
	records, err := p.st.ReadParentEvidence()
	if err != nil {
		return state.ParentEvidenceSummary{}
	}
	return state.SummarizeParentEvidence(records)
}

func (p *Projector) recordPart(part Part, surface string) Part {
	if part.Bytes == 0 {
		part.Bytes = len(part.Digest)
	}
	if part.TokenProxy == 0 {
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Surface = surface
	Record(p.st, state.ParentEvidenceRecord{
		Surface: surface, Origin: state.ParentEvidenceOriginEvidence,
		OwnerCallID: p.ownerCallID, Digest: part.Digest, Bytes: part.Bytes,
		TokenProxy: part.TokenProxy, Outcome: part.Status, Reason: part.Reason, Locator: part.Locator,
	})
	if part.Digest != "" {
		p.appendPendingClaim(surface, part.Digest)
	}
	return part
}

func (p *Projector) appendPendingClaim(surface, digest string) {
	for _, claim := range p.pendingClaims {
		if claim.Surface == surface && claim.Digest == digest {
			return
		}
	}
	p.pendingClaims = append(p.pendingClaims, state.ParentEvidenceLedgerEntry{
		Surface: surface, Digest: digest, Origin: state.ParentEvidenceOriginEvidence, OwnerCallID: p.ownerCallID,
	})
}

func (p *Projector) projectDiff(request DiffRequest) Part {
	part := Part{Kind: "diff", Detail: request.Question}
	files, body, err := captureDiff(p.repoRoot, request.Paths)
	if err != nil {
		part.Status = PartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceDiff)
	}
	identity := make([]string, 0, len(files)*2)
	for _, file := range files {
		identity = append(identity, file.Path, file.HeadBlob, file.IndexBlob, file.WorktreeSHA)
	}
	part.Digest = StringDigest(append([]string{request.Question}, identity...)...)
	diffBody := DiffBody{Question: request.Question, Paths: request.Paths, Files: files}
	if len(body) > request.BudgetBytes {
		part.Status = PartRefinement
		part.Reason = fmt.Sprintf(
			"diff body needs %d bytes but the budget is %d; narrow paths or raise budget_bytes; per-file identity is preserved",
			len(body), request.BudgetBytes,
		)
	} else {
		part.Status = PartProjected
		diffBody.Body = body
		part.Bytes = len(body)
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Diff = &diffBody
	part.Locator = "git diff HEAD -- " + strings.Join(request.Paths, " ")
	return p.recordPart(part, state.ParentEvidenceSurfaceDiff)
}

func captureDiff(repoRoot string, paths []string) ([]DiffFile, string, error) {
	body, err := gitOutputIn(repoRoot, append([]string{"diff", "HEAD", "--no-ext-diff", "--no-renames", "--"}, paths...)...)
	if err != nil {
		return nil, "", fmt.Errorf("git diff HEAD: %w", err)
	}
	numstat, err := gitOutputIn(repoRoot, append([]string{"diff", "HEAD", "--numstat", "--no-renames", "--"}, paths...)...)
	if err != nil {
		return nil, "", fmt.Errorf("git diff HEAD --numstat: %w", err)
	}
	status, err := gitOutputIn(repoRoot, append([]string{"status", "--porcelain=v1", "-z", "--untracked-files=all", "--"}, paths...)...)
	if err != nil {
		return nil, "", fmt.Errorf("git status: %w", err)
	}
	files := make([]DiffFile, 0, len(paths))
	for _, path := range paths {
		file := DiffFile{Path: path, Status: diffFileStatusLetter(string(status), path)}
		file.HeadBlob, err = gitTrimmedOutput(repoRoot, "rev-parse", "--verify", "HEAD:"+path)
		if err != nil {
			file.HeadBlob = ""
		}
		file.IndexBlob = diffIndexBlob(repoRoot, path)
		file.WorktreeSHA = diffWorktreeSHA(repoRoot, path)
		file.Additions, file.Deletions = diffNumstat(string(numstat), path)
		files = append(files, file)
	}
	return files, string(body), nil
}

func diffFileStatusLetter(statusOutput string, path string) string {
	for _, record := range strings.Split(strings.TrimRight(statusOutput, "\x00"), "\x00") {
		if record == "" {
			continue
		}
		if len(record) > 3 && record[3:] == path {
			return string(record[0])
		}
	}
	return "unknown"
}

func diffIndexBlob(repoRoot string, path string) string {
	output, err := gitOutputIn(repoRoot, "ls-files", "-s", "--", path)
	if err != nil {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func diffWorktreeSHA(repoRoot string, path string) string {
	abs, err := joinRoot(repoRoot, path)
	if err != nil {
		return ""
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func diffNumstat(numstatOutput string, path string) (int, int) {
	for _, line := range strings.Split(strings.TrimRight(numstatOutput, "\n"), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || fields[2] != path {
			continue
		}
		additions, addErr := strconv.Atoi(fields[0])
		deletions, delErr := strconv.Atoi(fields[1])
		if addErr != nil || delErr != nil {
			return 0, 0
		}
		return additions, deletions
	}
	return 0, 0
}

func (p *Projector) projectSource(request SourceRequest) Part {
	part := Part{
		Kind:    "source",
		Detail:  request.Path,
		Locator: fmt.Sprintf("%s:%d-%d", request.Path, request.LineStart, request.LineEnd),
	}
	abs, err := joinRoot(p.repoRoot, request.Path)
	if err != nil {
		part.Status = PartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceSource)
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		part.Status = PartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceSource)
	}
	lines := strings.Split(string(content), "\n")
	if request.LineEnd > len(lines) {
		part.Status = PartError
		part.Reason = fmt.Sprintf("requested range ends at line %d but the file has %d lines", request.LineEnd, len(lines))
		return p.recordPart(part, state.ParentEvidenceSurfaceSource)
	}
	extracted := strings.Join(lines[request.LineStart-1:request.LineEnd], "\n")
	sum := sha256.Sum256([]byte(extracted))
	part.Digest = hex.EncodeToString(sum[:])
	body := SourceBody{
		Question:  request.Question,
		Path:      request.Path,
		LineStart: request.LineStart,
		LineEnd:   request.LineEnd,
	}
	if len(extracted) > request.BudgetBytes {
		part.Status = PartRefinement
		part.Reason = fmt.Sprintf(
			"source range needs %d bytes but the budget is %d; narrow the line range or raise budget_bytes",
			len(extracted), request.BudgetBytes,
		)
	} else {
		part.Status = PartProjected
		body.Content = extracted
		part.Bytes = len(extracted)
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Source = &body
	return p.recordPart(part, state.ParentEvidenceSurfaceSource)
}

func joinRoot(repoRoot string, rel string) (string, error) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return "", fmt.Errorf("repository rootを解決できません: %w", err)
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("対象file %sを解決できません: %w", rel, err)
	}
	if canonical != root && !strings.HasPrefix(canonical, root+string(filepath.Separator)) {
		return "", fmt.Errorf("対象file %sがrepository境界を越えています", rel)
	}
	info, err := os.Lstat(canonical)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("対象file %sは通常fileではありません", rel)
	}
	return canonical, nil
}

func (p *Projector) Commit(stdout io.Writer, reason string) error {
	return WithLedgerLock(p.st, func() error {
		return p.commitLocked(stdout, reason)
	})
}

func (p *Projector) commitLocked(stdout io.Writer, reason string) error {
	if err := p.validateScope(); err != nil {
		return err
	}
	if LeaseActive(p.st) {
		if err := p.degradeDuplicateParts(); err != nil {
			return err
		}
	}
	output := p.output
	applyTotalBudget(&output)
	p.output = output
	written, err := WriteMeasuredJSON(stdout, output)
	if err != nil {
		return err
	}
	if err := p.markReviewProof(output.Parts); err != nil {
		return err
	}
	if err := p.saveSurvivingClaims(output.Parts); err != nil {
		return err
	}
	Record(p.st, state.ParentEvidenceRecord{
		Surface: state.ParentEvidenceSurfaceEvidenceTelemetry, Origin: state.ParentEvidenceOriginEvidence,
		OwnerCallID: p.ownerCallID, Bytes: written, Outcome: state.ParentEvidenceOutcomeProjected,
		Reason: reason, Locator: p.st.Path(TelemetryFile),
	})
	return nil
}

func (p *Projector) validateScope() error {
	if p.leaseErr != nil {
		return p.leaseErr
	}
	epoch, err := p.st.ParentEvidenceLeaseEpoch()
	if err != nil {
		return err
	}
	if epoch != p.leaseEpoch || p.st.ReadOr("task.id", "") != p.output.TaskID || string(p.st.TaskStatus()) != p.output.TaskStatus {
		return fmt.Errorf("parent evidence scope changed during projection; request fresh evidence")
	}
	return nil
}

func (p *Projector) saveSurvivingClaims(parts []Part) error {
	for _, claim := range p.pendingClaims {
		if !claimSurvivesBudget(parts, claim) {
			continue
		}
		if err := SaveLedger(p.st, claim.Surface, claim.Digest, claim.Origin, claim.OwnerCallID); err != nil {
			return err
		}
	}
	return nil
}

func claimSurvivesBudget(parts []Part, claim state.ParentEvidenceLedgerEntry) bool {
	for _, part := range parts {
		if part.Surface == claim.Surface && part.Digest == claim.Digest && part.Status != PartRefinement {
			return true
		}
	}
	return false
}

func (p *Projector) degradeDuplicateParts() error {
	claimed := make(map[string]bool, len(p.pendingClaims))
	for index := range p.output.Parts {
		part := &p.output.Parts[index]
		if part.Digest == "" {
			continue
		}
		key := part.Surface + "\x00" + part.Digest
		_, delivered, err := p.st.ParentEvidenceDelivered(part.Surface, part.Digest)
		if err != nil {
			return err
		}
		if !delivered && !claimed[key] {
			claimed[key] = true
			continue
		}
		hadBody := partHasBody(part)
		clearPartBody(part)
		part.Reason = UnchangedReason
		if hadBody {
			part.Bytes = 0
			part.TokenProxy = 0
		}
		Record(p.st, state.ParentEvidenceRecord{
			Surface: part.Surface, Origin: state.ParentEvidenceOriginEvidence,
			OwnerCallID: p.ownerCallID, Digest: part.Digest, Outcome: state.ParentEvidenceOutcomeDuplicate,
			Reason: UnchangedReason, Locator: part.Locator,
		})
	}
	return nil
}

func applyTotalBudget(output *Output) {
	for outputSize(*output) > MaxOutputBytes {
		if !stripBody(output) {
			return
		}
	}
	output.Status = aggregateStatus(output.Parts)
}

func outputSize(output Output) int {
	data, err := json.Marshal(output)
	if err != nil {
		return MaxOutputBytes + 1
	}
	return len(data)
}

func stripBody(output *Output) bool {
	for index := len(output.Parts) - 1; index >= 0; index-- {
		if !stripPartBody(&output.Parts[index]) {
			continue
		}
		return true
	}
	return false
}

func stripPartBody(part *Part) bool {
	stripped := false
	for _, strip := range bodyStrippers {
		if strip(part) {
			stripped = true
			break
		}
	}
	if !stripped {
		return false
	}
	clearPartBody(part)
	part.Status = PartRefinement
	part.Reason = "total evidence output budget exceeded; body omitted while counts, digests and locators are preserved"
	part.Bytes = 0
	part.TokenProxy = 0
	return true
}

func partHasBody(part *Part) bool {
	for _, strip := range bodyStrippers {
		if strip(part) {
			return true
		}
	}
	return false
}

func clearPartBody(part *Part) {
	switch {
	case part.Authority != nil && part.Authority.Content != "":
		part.Authority.Content = ""
	case len(part.Handoff) > 0:
		part.Handoff = nil
	case len(part.StatusRead) > 0:
		part.StatusRead = nil
	case part.Search != nil && len(part.Search.Results) > 0:
		part.Search.Results = nil
	case part.Diff != nil && part.Diff.Body != "":
		part.Diff.Body = ""
	case part.Source != nil && part.Source.Content != "":
		part.Source.Content = ""
	}
}

func gitOutputIn(dir string, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

func gitTrimmedOutput(dir string, args ...string) (string, error) {
	output, err := gitOutputIn(dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
