package app

import (
	"bytes"
	"context"
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

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/authoritybootstrapcmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type parentEvidenceManifest struct {
	Version     int                               `json:"version"`
	Reason      string                            `json:"reason"`
	Authority   []parentEvidenceAuthorityRequest  `json:"authority"`
	Handoff     *parentEvidenceHandoffRequest     `json:"handoff"`
	Status      *parentEvidenceStatusRequest      `json:"status"`
	Validations *parentEvidenceValidationsRequest `json:"validations"`
	Telemetry   *parentEvidenceTelemetryRequest   `json:"telemetry"`
	Search      []parentEvidenceSearchRequest     `json:"search"`
	Diff        []parentEvidenceDiffRequest       `json:"diff"`
	Source      []parentEvidenceSourceRequest     `json:"source"`
}

type parentEvidenceAuthorityRequest struct {
	Kind               string `json:"kind"`
	KnownContentSHA256 string `json:"known_content_sha256"`
	BudgetBytes        int    `json:"budget_bytes"`
}

type parentEvidenceHandoffRequest struct {
	KnownDigest string `json:"known_digest"`
	Force       bool   `json:"force"`
}

type parentEvidenceStatusRequest struct{}

type parentEvidenceValidationsRequest struct{}

type parentEvidenceTelemetryRequest struct{}

type parentEvidenceSearchRequest struct {
	Question    string   `json:"question"`
	Scopes      []string `json:"scopes"`
	BudgetBytes int      `json:"budget_bytes"`
}

type parentEvidenceDiffRequest struct {
	Question    string   `json:"question"`
	Paths       []string `json:"paths"`
	BudgetBytes int      `json:"budget_bytes"`
}

type parentEvidenceSourceRequest struct {
	Question    string `json:"question"`
	Path        string `json:"path"`
	LineStart   int    `json:"line_start"`
	LineEnd     int    `json:"line_end"`
	BudgetBytes int    `json:"budget_bytes"`
}

type parentEvidenceOutput struct {
	Version     int                         `json:"version"`
	Status      string                      `json:"status"`
	OwnerCallID string                      `json:"owner_call_id"`
	Reason      string                      `json:"reason"`
	TaskID      string                      `json:"task_id,omitempty"`
	TaskStatus  string                      `json:"task_status,omitempty"`
	Parts       []parentEvidencePart        `json:"parts"`
	Summary     state.ParentEvidenceSummary `json:"evidence_summary"`
}

type parentEvidencePart struct {
	Kind        string                       `json:"kind"`
	Detail      string                       `json:"detail,omitempty"`
	Status      string                       `json:"status"`
	Digest      string                       `json:"digest,omitempty"`
	Bytes       int                          `json:"bytes"`
	TokenProxy  int                          `json:"token_proxy"`
	Reason      string                       `json:"reason,omitempty"`
	Locator     string                       `json:"locator,omitempty"`
	Authority   *parentEvidenceAuthorityBody `json:"authority,omitempty"`
	Handoff     json.RawMessage              `json:"handoff,omitempty"`
	StatusRead  json.RawMessage              `json:"status_read,omitempty"`
	Validations []parentHandoffValidation    `json:"validations,omitempty"`
	Telemetry   *parentEvidenceTelemetryBody `json:"telemetry,omitempty"`
	Search      *parentEvidenceSearchBody    `json:"search,omitempty"`
	Diff        *parentEvidenceDiffBody      `json:"diff,omitempty"`
	Source      *parentEvidenceSourceBody    `json:"source,omitempty"`
}

type parentEvidenceAuthorityBody struct {
	Kind           string `json:"kind"`
	SnapshotSHA256 string `json:"authority_snapshot_sha256"`
	ActiveTask     string `json:"active_task"`
	ContentSHA256  string `json:"content_sha256"`
	Content        string `json:"content,omitempty"`
}

type parentEvidenceTelemetryBody struct {
	Records    int                         `json:"records"`
	ModelCalls int                         `json:"model_calls"`
	Summary    state.ParentEvidenceSummary `json:"summary"`
}

type parentEvidenceSearchBody struct {
	Question    string             `json:"question"`
	Scopes      []string           `json:"scopes"`
	Candidates  int                `json:"candidates"`
	ResultCount int                `json:"result_count"`
	Results     []repoSearchResult `json:"results"`
}

type parentEvidenceDiffFile struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	HeadBlob    string `json:"head_blob"`
	IndexBlob   string `json:"index_blob"`
	WorktreeSHA string `json:"worktree_sha256"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
}

type parentEvidenceDiffBody struct {
	Question string                   `json:"question"`
	Paths    []string                 `json:"paths"`
	Files    []parentEvidenceDiffFile `json:"files"`
	Body     string                   `json:"body,omitempty"`
}

type parentEvidenceSourceBody struct {
	Question  string `json:"question"`
	Path      string `json:"path"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Content   string `json:"content,omitempty"`
}

type parentEvidenceProjector struct {
	cfg         config.AppConfig
	st          *state.StateStore
	ownerCallID string
	output      parentEvidenceOutput
}

const (
	parentEvidenceStatusOK         = "ok"
	parentEvidenceStatusRequired   = "refinement_required"
	parentEvidenceStatusError      = "error"
	parentEvidencePartProjected    = "projected"
	parentEvidencePartUnchanged    = "unchanged"
	parentEvidencePartChanged      = "changed"
	parentEvidencePartUnknown      = "unknown"
	parentEvidencePartRefinement   = "refinement_required"
	parentEvidencePartError        = "error"
	parentEvidencePartDisabled     = "disabled"
	parentEvidenceManifestVersion  = 1
	parentEvidenceManifestMaxBytes = 64 * 1024
	parentEvidenceMaxOutputBytes   = 96 * 1024
	parentEvidenceMaxDiffPaths     = 32
	parentEvidenceMaxSourceLines   = 2000
	parentEvidenceMaxBudgetBytes   = 256 * 1024
	parentEvidenceTelemetryFile    = "parent-evidence.jsonl"
)

var parentEvidenceBodyStrippers = []func(*parentEvidencePart) bool{
	func(part *parentEvidencePart) bool {
		return part.Authority != nil && part.Authority.Content != ""
	},
	func(part *parentEvidencePart) bool {
		return len(part.Handoff) > 0
	},
	func(part *parentEvidencePart) bool {
		return len(part.StatusRead) > 0
	},
	func(part *parentEvidencePart) bool {
		return part.Search != nil && len(part.Search.Results) > 0
	},
	func(part *parentEvidencePart) bool {
		return part.Diff != nil && part.Diff.Body != ""
	},
	func(part *parentEvidencePart) bool {
		return part.Source != nil && part.Source.Content != ""
	},
}

func printParentEvidence(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	manifest, err := loadParentEvidenceManifest(cmd.EvidenceManifest)
	if err != nil {
		return err
	}
	ownerCallID, err := state.NewUUID()
	if err != nil {
		return err
	}
	projector := &parentEvidenceProjector{cfg: cfg, st: st, ownerCallID: ownerCallID}
	projector.project(manifest)
	output := projector.output
	applyParentEvidenceTotalBudget(&output)
	written, writeErr := writeMeasuredJSON(stdout, output)
	if writeErr != nil {
		return writeErr
	}
	recordParentEvidence(st, state.ParentEvidenceRecord{
		Surface: state.ParentEvidenceSurfaceEvidenceTelemetry, Origin: state.ParentEvidenceOriginEvidence,
		OwnerCallID: ownerCallID, Bytes: written, Outcome: state.ParentEvidenceOutcomeProjected,
		Reason: manifest.Reason, Locator: st.Path(parentEvidenceTelemetryFile),
	})
	return nil
}

func loadParentEvidenceManifest(path string) (parentEvidenceManifest, error) {
	if path == "" {
		return parentEvidenceManifest{}, usageError("usage: glm-worker --evidence <manifest.json>")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return parentEvidenceManifest{}, &NotFoundError{Message: "evidence manifest file is not found: " + path}
		}
		return parentEvidenceManifest{}, fmt.Errorf("evidence manifestを確認できません: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return parentEvidenceManifest{}, usageError("evidence manifestは通常fileだけを指定できます: " + path)
	}
	if info.Size() > parentEvidenceManifestMaxBytes {
		return parentEvidenceManifest{}, usageError("evidence manifestが上限を超えています")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return parentEvidenceManifest{}, fmt.Errorf("evidence manifestを読めません: %w", err)
	}
	var manifest parentEvidenceManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return parentEvidenceManifest{}, usageError("evidence manifestのschemaが不正です: " + err.Error())
	}
	if manifest.Version != parentEvidenceManifestVersion {
		return parentEvidenceManifest{}, usageError("evidence manifestのversionは1だけを受理します")
	}
	if strings.TrimSpace(manifest.Reason) == "" {
		return parentEvidenceManifest{}, usageError("evidence manifestにはreasonが必要です")
	}
	if err := validateParentEvidenceManifest(manifest); err != nil {
		return parentEvidenceManifest{}, err
	}
	return manifest, nil
}

func validateParentEvidenceManifest(manifest parentEvidenceManifest) error {
	if parentEvidencePartCount(manifest) == 0 {
		return usageError("evidence manifestは少なくとも1つのprojection partを指定してください")
	}
	for _, request := range manifest.Authority {
		if err := validateParentEvidenceAuthorityPart(request); err != nil {
			return err
		}
	}
	for _, request := range manifest.Search {
		if err := validateParentEvidenceSearchPart(request); err != nil {
			return err
		}
	}
	for _, request := range manifest.Diff {
		if err := validateParentEvidenceDiffPart(request); err != nil {
			return err
		}
	}
	for _, request := range manifest.Source {
		if err := validateParentEvidenceSourcePart(request); err != nil {
			return err
		}
	}
	return nil
}

func parentEvidencePartCount(manifest parentEvidenceManifest) int {
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

func validateParentEvidenceAuthorityPart(request parentEvidenceAuthorityRequest) error {
	if request.Kind != "rules" && request.Kind != "plan" && request.Kind != "active" {
		return usageError("evidence manifestのauthority kindはrules|plan|activeだけを受理します")
	}
	if request.BudgetBytes <= 0 || request.BudgetBytes > parentEvidenceMaxBudgetBytes {
		return usageError("evidence manifestのauthority budget_bytesは1..262144で指定してください")
	}
	return nil
}

func validateParentEvidenceSearchPart(request parentEvidenceSearchRequest) error {
	if strings.TrimSpace(request.Question) == "" || len(request.Scopes) == 0 || request.BudgetBytes <= 0 || request.BudgetBytes > repoSearchMaxBudgetBytes {
		return usageError("evidence manifestのsearch partはquestion・scopes・budget_bytes(1..65536)を必須とします")
	}
	return nil
}

func validateParentEvidenceDiffPart(request parentEvidenceDiffRequest) error {
	if strings.TrimSpace(request.Question) == "" || len(request.Paths) == 0 || len(request.Paths) > parentEvidenceMaxDiffPaths {
		return usageError("evidence manifestのdiff partはquestionと1..32個のpathsを必須とします")
	}
	if request.BudgetBytes <= 0 || request.BudgetBytes > parentEvidenceMaxBudgetBytes {
		return usageError("evidence manifestのdiff budget_bytesは1..262144で指定してください")
	}
	for _, path := range request.Paths {
		if !parentEvidenceRelativePath(path) {
			return usageError("evidence manifestのdiff pathsはrepository相対pathだけを受理します: " + path)
		}
	}
	return nil
}

func validateParentEvidenceSourcePart(request parentEvidenceSourceRequest) error {
	if strings.TrimSpace(request.Question) == "" || !parentEvidenceRelativePath(request.Path) {
		return usageError("evidence manifestのsource partはquestionとrepository相対pathを必須とします")
	}
	if request.LineStart < 1 || request.LineEnd < request.LineStart || request.LineEnd-request.LineStart+1 > parentEvidenceMaxSourceLines {
		return usageError("evidence manifestのsource行範囲は1..2000行で指定してください")
	}
	if request.BudgetBytes <= 0 || request.BudgetBytes > parentEvidenceMaxBudgetBytes {
		return usageError("evidence manifestのsource budget_bytesは1..262144で指定してください")
	}
	return nil
}

func parentEvidenceRelativePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return false
	}
	return true
}

func (p *parentEvidenceProjector) project(manifest parentEvidenceManifest) {
	p.output = parentEvidenceOutput{
		Version:     parentEvidenceManifestVersion,
		OwnerCallID: p.ownerCallID,
		Reason:      manifest.Reason,
		TaskID:      p.st.ReadOr("task.id", ""),
		TaskStatus:  string(p.st.TaskStatus()),
		Parts:       []parentEvidencePart{},
	}
	for _, request := range manifest.Authority {
		p.output.Parts = append(p.output.Parts, p.projectAuthority(request))
	}
	if manifest.Handoff != nil {
		p.output.Parts = append(p.output.Parts, p.projectHandoff(*manifest.Handoff))
	}
	if manifest.Status != nil {
		p.output.Parts = append(p.output.Parts, p.projectStatus())
	}
	if manifest.Validations != nil {
		p.output.Parts = append(p.output.Parts, p.projectValidations())
	}
	if manifest.Telemetry != nil {
		p.output.Parts = append(p.output.Parts, p.projectTelemetry())
	}
	for _, request := range manifest.Search {
		p.output.Parts = append(p.output.Parts, p.projectSearch(request))
	}
	for _, request := range manifest.Diff {
		p.output.Parts = append(p.output.Parts, p.projectDiff(request))
	}
	for _, request := range manifest.Source {
		p.output.Parts = append(p.output.Parts, p.projectSource(request))
	}
	p.output.Status = parentEvidenceAggregateStatus(p.output.Parts)
	p.output.Summary = p.evidenceSummary()
}

func parentEvidenceAggregateStatus(parts []parentEvidencePart) string {
	status := parentEvidenceStatusOK
	for _, part := range parts {
		switch part.Status {
		case parentEvidencePartError:
			return parentEvidenceStatusError
		case parentEvidencePartRefinement:
			status = parentEvidenceStatusRequired
		}
	}
	return status
}

func (p *parentEvidenceProjector) evidenceSummary() state.ParentEvidenceSummary {
	records, err := p.st.ReadParentEvidence()
	if err != nil {
		return state.ParentEvidenceSummary{}
	}
	return state.SummarizeParentEvidence(records)
}

func (p *parentEvidenceProjector) recordPart(part parentEvidencePart, surface string) parentEvidencePart {
	if part.Bytes == 0 {
		part.Bytes = len(part.Digest)
	}
	if part.TokenProxy == 0 {
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	recordParentEvidence(p.st, state.ParentEvidenceRecord{
		Surface: surface, Origin: state.ParentEvidenceOriginEvidence,
		OwnerCallID: p.ownerCallID, Digest: part.Digest, Bytes: part.Bytes,
		TokenProxy: part.TokenProxy, Outcome: part.Status, Reason: part.Reason, Locator: part.Locator,
	})
	return part
}

func (p *parentEvidenceProjector) saveLedger(surface, digest string) {
	saveParentEvidenceLedger(p.st, surface, digest, state.ParentEvidenceOriginEvidence, p.ownerCallID)
}

func (p *parentEvidenceProjector) projectAuthority(request parentEvidenceAuthorityRequest) parentEvidencePart {
	surface := state.ParentEvidenceSurfaceAuthority + ":" + request.Kind
	output, err := authoritybootstrapcmd.BuildFromRoot(p.cfg.RepoRoot, request.Kind, request.KnownContentSHA256)
	part := parentEvidencePart{Kind: "authority", Detail: request.Kind}
	if err != nil {
		part.Status = parentEvidencePartError
		part.Reason = err.Error()
		return p.recordPart(part, surface)
	}
	body := parentEvidenceAuthorityBody{
		Kind:           output.AuthorityKind,
		SnapshotSHA256: output.AuthoritySnapshotSHA256,
		ActiveTask:     output.ActiveTask,
		ContentSHA256:  output.ContentSHA256,
	}
	part.Digest = output.ContentSHA256
	switch output.ContentMatch {
	case authoritybootstrapcmd.ContentMatchUnchanged:
		part.Status = parentEvidencePartUnchanged
	case authoritybootstrapcmd.ContentMatchChanged:
		part.Status = parentEvidencePartChanged
		body.Content = output.Content
	default:
		part.Status = parentEvidencePartProjected
		body.Content = output.Content
	}
	if body.Content != "" && len(body.Content) > request.BudgetBytes {
		part.Status = parentEvidencePartRefinement
		part.Reason = fmt.Sprintf(
			"changed authority body needs %d bytes but the budget is %d; raise budget_bytes for kind %s instead of receiving a truncated body",
			len(body.Content), request.BudgetBytes, request.Kind,
		)
		body.Content = ""
	}
	if body.Content != "" {
		part.Bytes = len(body.Content)
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Locator = "authority:" + request.Kind
	part.Authority = &body
	return p.recordPart(part, surface)
}

func (p *parentEvidenceProjector) projectHandoff(request parentEvidenceHandoffRequest) parentEvidencePart {
	value := buildParentHandoff(p.st)
	digest, _ := parentEvidenceDigest(value)
	part := parentEvidencePart{Kind: "handoff", Digest: digest, Locator: "handoff:current-state"}
	if !request.Force && request.KnownDigest != "" && request.KnownDigest == digest {
		part.Status = parentEvidencePartUnchanged
		p.saveLedger(state.ParentEvidenceSurfaceHandoff, digest)
		return p.recordPart(part, state.ParentEvidenceSurfaceHandoff)
	}
	data, err := json.Marshal(value)
	if err != nil {
		part.Status = parentEvidencePartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceHandoff)
	}
	part.Status = parentEvidencePartProjected
	part.Handoff = json.RawMessage(data)
	part.Bytes = len(data)
	part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	p.saveLedger(state.ParentEvidenceSurfaceHandoff, digest)
	return p.recordPart(part, state.ParentEvidenceSurfaceHandoff)
}

func (p *parentEvidenceProjector) projectStatus() parentEvidencePart {
	taskID := p.st.ReadOr("task.id", "")
	logs, logErr := readStatusTelemetry(p.st, taskID)
	value := buildStatusOutput(p.st, taskID, logs, logErr)
	data, err := json.Marshal(value)
	part := parentEvidencePart{Kind: "status", Detail: "status_read"}
	if err != nil {
		part.Status = parentEvidencePartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceStatus)
	}
	part.Digest = parentStatusReadDigest(p.st)
	part.Status = parentEvidencePartProjected
	part.StatusRead = json.RawMessage(data)
	part.Bytes = len(data)
	part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	part.Locator = "status:current-state"
	p.saveLedger(state.ParentEvidenceSurfaceStatus, part.Digest)
	return p.recordPart(part, state.ParentEvidenceSurfaceStatus)
}

func (p *parentEvidenceProjector) projectValidations() parentEvidencePart {
	part := parentEvidencePart{Kind: "validations", Locator: "quality-gate-runs"}
	repoRoot := p.st.ReadOr("repo-root", "")
	if repoRoot == "" {
		part.Status = parentEvidencePartUnknown
		part.Reason = "repository root is unavailable"
		return p.recordPart(part, state.ParentEvidenceSurfaceValidations)
	}
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		part.Status = parentEvidencePartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceValidations)
	}
	digest := &state.SnapshotDigest{
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
	}
	records := currentParentValidations(p.st, repoRoot, digest)
	routing := currentParentRoutingEvidence(p.st, repoRoot, p.st.ReadOr("task.id", ""), digest)
	part.Validations = records
	part.Detail = fmt.Sprintf("%d runs, routing %d", len(records), len(routing))
	if len(records) == 0 {
		part.Status = parentEvidencePartUnknown
		part.Reason = "no validation run matches the current snapshot"
	} else {
		rendered, marshalErr := json.Marshal(records)
		if marshalErr != nil {
			part.Status = parentEvidencePartError
			part.Reason = marshalErr.Error()
			return p.recordPart(part, state.ParentEvidenceSurfaceValidations)
		}
		part.Status = parentEvidencePartProjected
		part.Bytes = len(rendered)
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Digest = parentEvidenceStringDigest(fmt.Sprintf("%v", records))
	return p.recordPart(part, state.ParentEvidenceSurfaceValidations)
}

func (p *parentEvidenceProjector) projectTelemetry() parentEvidencePart {
	part := parentEvidencePart{Kind: "telemetry", Locator: p.st.Path(parentEvidenceTelemetryFile)}
	records, err := p.st.ReadParentEvidence()
	if err != nil {
		part.Status = parentEvidencePartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceEvidenceTelemetry)
	}
	logs, logErr := readStatusTelemetry(p.st, p.st.ReadOr("task.id", ""))
	if logErr != nil && len(logs) == 0 {
		part.Status = parentEvidencePartUnknown
		part.Reason = "model call telemetry is unavailable"
	} else {
		part.Status = parentEvidencePartProjected
	}
	body := parentEvidenceTelemetryBody{
		Records:    len(records),
		ModelCalls: len(logs),
		Summary:    state.SummarizeParentEvidence(records),
	}
	part.Telemetry = &body
	rendered, marshalErr := json.Marshal(body)
	if marshalErr != nil {
		part.Status = parentEvidencePartError
		part.Reason = marshalErr.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceEvidenceTelemetry)
	}
	part.Bytes = len(rendered)
	part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	part.Digest = parentEvidenceStringDigest(fmt.Sprintf("%d:%d", body.Records, body.ModelCalls))
	return p.recordPart(part, state.ParentEvidenceSurfaceEvidenceTelemetry)
}

func (p *parentEvidenceProjector) projectSearch(request parentEvidenceSearchRequest) parentEvidencePart {
	part := parentEvidencePart{Kind: "search", Detail: request.Question}
	if !p.cfg.RepoSearch {
		part.Status = parentEvidencePartDisabled
		part.Reason = "repo search is disabled for this repository"
		return p.recordPart(part, state.ParentEvidenceSurfaceSearch)
	}
	scoped := repoSearchRequest(request)
	report, err := reposearch.Search(context.Background(), p.cfg.RepoRoot, request.Question, reposearch.Options{
		DisableCache: true,
		MaxResults:   workflow.RepoSearchMaxResults,
		PathPrefixes: scoped.pathPrefixes(),
		Symbols:      scoped.symbols(),
	})
	if err != nil {
		part.Status = parentEvidencePartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceSearch)
	}
	results := repoSearchResults(report.Results)
	output := buildRepoSearchOutput(scoped, report, results)
	part.Digest = parentEvidenceStringDigest(
		state.ParentEvidenceSurfaceSearch, request.Question,
		fmt.Sprintf("%v", request.Scopes), repoSearchResultsDigest(results),
	)
	body := parentEvidenceSearchBody{
		Question:    request.Question,
		Scopes:      request.Scopes,
		Candidates:  report.Candidates,
		ResultCount: len(results),
		Results:     []repoSearchResult{},
	}
	if output.Status == repoSearchResultRequired {
		part.Status = parentEvidencePartRefinement
		part.Reason = output.Reason
	} else {
		part.Status = parentEvidencePartProjected
		body.Results = results
		rendered, _ := json.Marshal(results)
		part.Bytes = len(rendered)
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Search = &body
	part.Locator = "reposearch:" + request.Question
	p.saveLedger(state.ParentEvidenceSurfaceSearch, part.Digest)
	return p.recordPart(part, state.ParentEvidenceSurfaceSearch)
}

func (p *parentEvidenceProjector) projectDiff(request parentEvidenceDiffRequest) parentEvidencePart {
	part := parentEvidencePart{Kind: "diff", Detail: request.Question}
	files, body, err := captureParentEvidenceDiff(p.cfg.RepoRoot, request.Paths)
	if err != nil {
		part.Status = parentEvidencePartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceDiff)
	}
	identity := make([]string, 0, len(files)*2)
	for _, file := range files {
		identity = append(identity, file.Path, file.HeadBlob, file.IndexBlob, file.WorktreeSHA)
	}
	part.Digest = parentEvidenceStringDigest(append([]string{request.Question}, identity...)...)
	diffBody := parentEvidenceDiffBody{
		Question: request.Question,
		Paths:    request.Paths,
		Files:    files,
	}
	if len(body) > request.BudgetBytes {
		part.Status = parentEvidencePartRefinement
		part.Reason = fmt.Sprintf(
			"diff body needs %d bytes but the budget is %d; narrow paths or raise budget_bytes; per-file identity is preserved",
			len(body), request.BudgetBytes,
		)
	} else {
		part.Status = parentEvidencePartProjected
		diffBody.Body = body
		part.Bytes = len(body)
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Diff = &diffBody
	part.Locator = "git diff HEAD -- " + strings.Join(request.Paths, " ")
	return p.recordPart(part, state.ParentEvidenceSurfaceDiff)
}

func captureParentEvidenceDiff(repoRoot string, paths []string) ([]parentEvidenceDiffFile, string, error) {
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
	files := make([]parentEvidenceDiffFile, 0, len(paths))
	for _, path := range paths {
		file := parentEvidenceDiffFile{Path: path, Status: diffFileStatusLetter(string(status), path)}
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
	abs, err := parentEvidenceJoinRoot(repoRoot, path)
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

func (p *parentEvidenceProjector) projectSource(request parentEvidenceSourceRequest) parentEvidencePart {
	part := parentEvidencePart{
		Kind:    "source",
		Detail:  request.Path,
		Locator: fmt.Sprintf("%s:%d-%d", request.Path, request.LineStart, request.LineEnd),
	}
	abs, err := parentEvidenceJoinRoot(p.cfg.RepoRoot, request.Path)
	if err != nil {
		part.Status = parentEvidencePartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceSource)
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		part.Status = parentEvidencePartError
		part.Reason = err.Error()
		return p.recordPart(part, state.ParentEvidenceSurfaceSource)
	}
	lines := strings.Split(string(content), "\n")
	if request.LineEnd > len(lines) {
		part.Status = parentEvidencePartError
		part.Reason = fmt.Sprintf("requested range ends at line %d but the file has %d lines", request.LineEnd, len(lines))
		return p.recordPart(part, state.ParentEvidenceSurfaceSource)
	}
	extracted := strings.Join(lines[request.LineStart-1:request.LineEnd], "\n")
	sum := sha256.Sum256([]byte(extracted))
	part.Digest = hex.EncodeToString(sum[:])
	body := parentEvidenceSourceBody{
		Question:  request.Question,
		Path:      request.Path,
		LineStart: request.LineStart,
		LineEnd:   request.LineEnd,
	}
	if len(extracted) > request.BudgetBytes {
		part.Status = parentEvidencePartRefinement
		part.Reason = fmt.Sprintf(
			"source range needs %d bytes but the budget is %d; narrow the line range or raise budget_bytes",
			len(extracted), request.BudgetBytes,
		)
	} else {
		part.Status = parentEvidencePartProjected
		body.Content = extracted
		part.Bytes = len(extracted)
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Source = &body
	return p.recordPart(part, state.ParentEvidenceSurfaceSource)
}

func parentEvidenceJoinRoot(repoRoot string, rel string) (string, error) {
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

func applyParentEvidenceTotalBudget(output *parentEvidenceOutput) {
	for parentEvidenceOutputSize(*output) > parentEvidenceMaxOutputBytes {
		if !stripParentEvidenceBody(output) {
			return
		}
	}
	output.Status = parentEvidenceAggregateStatus(output.Parts)
}

func parentEvidenceOutputSize(output parentEvidenceOutput) int {
	data, err := json.Marshal(output)
	if err != nil {
		return parentEvidenceMaxOutputBytes + 1
	}
	return len(data)
}

func stripParentEvidenceBody(output *parentEvidenceOutput) bool {
	for index := len(output.Parts) - 1; index >= 0; index-- {
		if !stripParentEvidencePartBody(&output.Parts[index]) {
			continue
		}
		return true
	}
	return false
}

func stripParentEvidencePartBody(part *parentEvidencePart) bool {
	stripped := false
	for _, strip := range parentEvidenceBodyStrippers {
		if strip(part) {
			stripped = true
			break
		}
	}
	if !stripped {
		return false
	}
	clearParentEvidencePartBody(part)
	part.Status = parentEvidencePartRefinement
	part.Reason = "total evidence output budget exceeded; body omitted while counts, digests and locators are preserved"
	part.Bytes = 0
	part.TokenProxy = 0
	return true
}

func clearParentEvidencePartBody(part *parentEvidencePart) {
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
