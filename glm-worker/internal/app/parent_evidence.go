package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/authoritybootstrapcmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type parentEvidenceManifest = parentevidence.Manifest
type parentEvidenceAuthorityRequest = parentevidence.AuthorityRequest
type parentEvidenceHandoffRequest = parentevidence.HandoffRequest
type parentEvidenceStatusRequest = parentevidence.StatusRequest
type parentEvidenceValidationsRequest = parentevidence.ValidationsRequest
type parentEvidenceTelemetryRequest = parentevidence.TelemetryRequest
type parentEvidenceSearchRequest = parentevidence.SearchRequest
type parentEvidenceDiffRequest = parentevidence.DiffRequest
type parentEvidenceSourceRequest = parentevidence.SourceRequest
type parentEvidenceOutput = parentevidence.Output
type parentEvidencePart = parentevidence.Part
type parentEvidenceDiffFile = parentevidence.DiffFile
type parentEvidenceDiffBody = parentevidence.DiffBody
type parentEvidenceSourceBody = parentevidence.SourceBody

type parentEvidenceProjector struct {
	cfg         config.AppConfig
	st          *state.StateStore
	ownerCallID string
	inner       *parentevidence.Projector
	output      parentEvidenceOutput
}

const (
	parentEvidenceStatusOK         = parentevidence.StatusOK
	parentEvidenceStatusRequired   = parentevidence.StatusRequired
	parentEvidenceStatusError      = parentevidence.StatusError
	parentEvidencePartProjected    = parentevidence.PartProjected
	parentEvidencePartUnchanged    = parentevidence.PartUnchanged
	parentEvidencePartChanged      = parentevidence.PartChanged
	parentEvidencePartUnknown      = parentevidence.PartUnknown
	parentEvidencePartRefinement   = parentevidence.PartRefinement
	parentEvidencePartError        = parentevidence.PartError
	parentEvidencePartDisabled     = parentevidence.PartDisabled
	parentEvidenceManifestVersion  = parentevidence.ManifestVersion
	parentEvidenceManifestMaxBytes = parentevidence.ManifestMaxBytes
	parentEvidenceMaxOutputBytes   = parentevidence.MaxOutputBytes
	parentEvidenceMaxDiffPaths     = parentevidence.MaxDiffPaths
	parentEvidenceMaxSourceLines   = parentevidence.MaxSourceLines
	parentEvidenceMaxBudgetBytes   = parentevidence.MaxBudgetBytes
	parentEvidenceTelemetryFile    = parentevidence.TelemetryFile
)

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
	return commitParentEvidenceProjection(projector, stdout, manifest.Reason)
}

func (p *parentEvidenceProjector) project(manifest parentEvidenceManifest) {
	p.inner = parentevidence.NewProjector(p.cfg.RepoRoot, p.st, p.ownerCallID, parentEvidenceProviders(p.cfg, p.st))
	p.inner.Project(manifest)
	p.output = p.inner.Output()
}

func commitParentEvidenceProjection(p *parentEvidenceProjector, stdout io.Writer, reason string) error {
	if p.inner == nil {
		return fmt.Errorf("parent evidence projector is not initialized")
	}
	err := p.inner.Commit(stdout, reason)
	p.output = p.inner.Output()
	return err
}

func loadParentEvidenceManifest(path string) (parentEvidenceManifest, error) {
	if path == "" {
		return parentEvidenceManifest{}, machinecli.UsageErrorf("usage: glm-worker --evidence <manifest.json>")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return parentEvidenceManifest{}, &machinecli.NotFoundError{Message: "evidence manifest file is not found: " + path}
		}
		return parentEvidenceManifest{}, fmt.Errorf("evidence manifestを確認できません: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return parentEvidenceManifest{}, machinecli.UsageErrorf("evidence manifestは通常fileだけを指定できます: " + path)
	}
	if info.Size() > parentEvidenceManifestMaxBytes {
		return parentEvidenceManifest{}, machinecli.UsageErrorf("evidence manifestが上限を超えています")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return parentEvidenceManifest{}, fmt.Errorf("evidence manifestを読めません: %w", err)
	}
	return parentevidence.DecodeManifest(data)
}

func parentEvidenceProviders(cfg config.AppConfig, st *state.StateStore) parentevidence.Providers {
	return parentevidence.Providers{
		Authority: func(request parentevidence.AuthorityRequest) parentevidence.Part {
			return projectParentEvidenceAuthority(cfg, request)
		},
		Handoff: func(request parentevidence.HandoffRequest) parentevidence.Part {
			return projectParentEvidenceHandoff(cfg, st, request)
		},
		Status: func() parentevidence.Part {
			return projectParentEvidenceStatus(st)
		},
		Validations: func() parentevidence.Part {
			return projectParentEvidenceValidations(st)
		},
		Telemetry: func() parentevidence.Part {
			return projectParentEvidenceTelemetry(st)
		},
		Search: func(request parentevidence.SearchRequest) parentevidence.Part {
			return projectParentEvidenceSearch(cfg, request)
		},
	}
}

func projectParentEvidenceAuthority(cfg config.AppConfig, request parentevidence.AuthorityRequest) parentevidence.Part {
	output, err := authoritybootstrapcmd.BuildFromRoot(cfg.RepoRoot, request.Kind, request.KnownContentSHA256)
	part := parentevidence.Part{Kind: "authority", Detail: request.Kind}
	if err != nil {
		part.Status = parentevidence.PartError
		part.Reason = err.Error()
		return part
	}
	body := parentevidence.AuthorityBody{
		Kind:           output.AuthorityKind,
		SnapshotSHA256: output.AuthoritySnapshotSHA256,
		ActiveTask:     output.ActiveTask,
		ContentSHA256:  output.ContentSHA256,
	}
	part.Digest = output.ContentSHA256
	switch output.ContentMatch {
	case authoritybootstrapcmd.ContentMatchUnchanged:
		part.Status = parentevidence.PartUnchanged
	case authoritybootstrapcmd.ContentMatchChanged:
		part.Status = parentevidence.PartChanged
		body.Content = output.Content
	default:
		part.Status = parentevidence.PartProjected
		body.Content = output.Content
	}
	if body.Content != "" && len(body.Content) > request.BudgetBytes {
		part.Status = parentevidence.PartRefinement
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
	return part
}

func projectParentEvidenceHandoff(cfg config.AppConfig, st *state.StateStore, request parentevidence.HandoffRequest) parentevidence.Part {
	value := buildParentHandoffWithConfig(cfg, st)
	digest, _ := parentEvidenceDigest(value)
	part := parentevidence.Part{Kind: "handoff", Digest: digest, Locator: "handoff:current-state"}
	if !request.Force && request.KnownDigest != "" && request.KnownDigest == digest {
		part.Status = parentevidence.PartUnchanged
		return part
	}
	data, err := json.Marshal(value)
	if err != nil {
		part.Status = parentevidence.PartError
		part.Reason = err.Error()
		return part
	}
	part.Status = parentevidence.PartProjected
	part.Handoff = json.RawMessage(data)
	part.Bytes = len(data)
	part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	return part
}

func projectParentEvidenceStatus(st *state.StateStore) parentevidence.Part {
	taskID := st.ReadOr("task.id", "")
	logs, logErr := taskview.ReadStatusTelemetry(st, taskID)
	value := buildStatusOutput(st, taskID, logs, logErr)
	data, err := json.Marshal(value)
	part := parentevidence.Part{Kind: "status", Detail: "status_read"}
	if err != nil {
		part.Status = parentevidence.PartError
		part.Reason = err.Error()
		return part
	}
	part.Digest = parentStatusReadDigest(st)
	part.Status = parentevidence.PartProjected
	part.StatusRead = json.RawMessage(data)
	part.Bytes = len(data)
	part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	part.Locator = "status:current-state"
	return part
}

func projectParentEvidenceValidations(st *state.StateStore) parentevidence.Part {
	part := parentevidence.Part{Kind: "validations", Locator: "quality-gate-runs"}
	repoRoot := st.ReadOr("repo-root", "")
	if repoRoot == "" {
		part.Status = parentevidence.PartUnknown
		part.Reason = "repository root is unavailable"
		return part
	}
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		part.Status = parentevidence.PartError
		part.Reason = err.Error()
		return part
	}
	digest := &state.SnapshotDigest{
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
	}
	records := currentParentValidations(st, repoRoot, digest)
	routing := currentParentRoutingEvidence(st, repoRoot, st.ReadOr("task.id", ""), digest)
	part.Validations = make([]parentevidence.Validation, 0, len(records))
	for _, record := range records {
		part.Validations = append(part.Validations, parentevidence.Validation{
			ValidationRunID: record.ValidationRunID,
			Form:            record.Form,
			Status:          record.Status,
			WorkingDir:      record.WorkingDir,
			Log:             record.Log,
			Head:            record.Head,
			IndexDigest:     record.IndexDigest,
			WorktreeDigest:  record.WorktreeDigest,
		})
	}
	part.Detail = fmt.Sprintf("%d runs, routing %d", len(records), len(routing))
	if len(records) == 0 {
		part.Status = parentevidence.PartUnknown
		part.Reason = "no validation run matches the current snapshot"
	} else {
		rendered, marshalErr := json.Marshal(records)
		if marshalErr != nil {
			part.Status = parentevidence.PartError
			part.Reason = marshalErr.Error()
			return part
		}
		part.Status = parentevidence.PartProjected
		part.Bytes = len(rendered)
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Digest = parentEvidenceStringDigest(fmt.Sprintf("%v", records))
	return part
}

func projectParentEvidenceTelemetry(st *state.StateStore) parentevidence.Part {
	part := parentevidence.Part{Kind: "telemetry", Locator: st.Path(parentEvidenceTelemetryFile)}
	records, err := st.ReadParentEvidence()
	if err != nil {
		part.Status = parentevidence.PartError
		part.Reason = err.Error()
		return part
	}
	logs, logErr := taskview.ReadStatusTelemetry(st, st.ReadOr("task.id", ""))
	if logErr != nil && len(logs) == 0 {
		part.Status = parentevidence.PartUnknown
		part.Reason = "model call telemetry is unavailable"
	} else {
		part.Status = parentevidence.PartProjected
	}
	body := parentevidence.TelemetryBody{
		Records:    len(records),
		ModelCalls: len(logs),
		Summary:    state.SummarizeParentEvidence(records),
	}
	part.Telemetry = &body
	rendered, marshalErr := json.Marshal(body)
	if marshalErr != nil {
		part.Status = parentevidence.PartError
		part.Reason = marshalErr.Error()
		return part
	}
	part.Bytes = len(rendered)
	part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	part.Digest = parentEvidenceStringDigest(fmt.Sprintf("%d:%d", body.Records, body.ModelCalls))
	return part
}

func projectParentEvidenceSearch(cfg config.AppConfig, request parentevidence.SearchRequest) parentevidence.Part {
	part := parentevidence.Part{Kind: "search", Detail: request.Question}
	if !cfg.RepoSearch {
		part.Status = parentevidence.PartDisabled
		part.Reason = "repo search is disabled for this repository"
		return part
	}
	scoped := repoSearchRequest{Question: request.Question, Scopes: request.Scopes, BudgetBytes: request.BudgetBytes}
	report, err := reposearch.Search(context.Background(), cfg.RepoRoot, request.Question, reposearch.Options{
		DisableCache: true,
		MaxResults:   workflow.RepoSearchMaxResults,
		PathPrefixes: scoped.pathPrefixes(),
		Symbols:      scoped.symbols(),
	})
	if err != nil {
		part.Status = parentevidence.PartError
		part.Reason = err.Error()
		return part
	}
	results := repoSearchResults(report.Results)
	output := buildRepoSearchOutput(scoped, report, results)
	part.Digest = parentEvidenceStringDigest(
		state.ParentEvidenceSurfaceSearch, request.Question,
		fmt.Sprintf("%v", request.Scopes), repoSearchResultsDigest(results),
	)
	body := parentevidence.SearchBody{
		Question:    request.Question,
		Scopes:      request.Scopes,
		Candidates:  report.Candidates,
		ResultCount: len(results),
		Results:     []parentevidence.SearchResult{},
	}
	if output.Status == repoSearchResultRequired {
		part.Status = parentevidence.PartRefinement
		part.Reason = output.Reason
	} else {
		part.Status = parentevidence.PartProjected
		for _, result := range results {
			body.Results = append(body.Results, parentevidence.SearchResult{Path: result.Path, Line: result.Line, Score: result.Score})
		}
		rendered, _ := json.Marshal(results)
		part.Bytes = len(rendered)
		part.TokenProxy = state.ParentEvidenceTokenProxy(part.Bytes)
	}
	part.Search = &body
	part.Locator = "reposearch:" + request.Question
	return part
}
