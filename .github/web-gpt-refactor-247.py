from pathlib import Path

path = Path("glm-worker/internal/app/bundle.go")
text = path.read_text()

old_types = '''type bundleOutput struct {
\tTaskID             string   `json:"task_id"`
\tTaskStatus         string   `json:"task_status"`
\tArchivePath        string   `json:"archive_path"`
\tEvidenceStatus     string   `json:"evidence_status"`
\tCoverage           string   `json:"coverage"`
\tCoverageReasons    []string `json:"coverage_reasons,omitempty"`
\tCoverageScope      []string `json:"coverage_scope"`
\tClaudeSessionIDs   []string `json:"claude_session_ids"`
\tInFlightModelCalls int      `json:"in_flight_model_calls,omitempty"`
\tMissing            []string `json:"missing"`
\tUnattributed       []string `json:"unattributed,omitempty"`
\tUnreadable         []string `json:"unreadable,omitempty"`
}

type bundleManifest struct {
\tFormat             string              `json:"format"`
\tTaskID             string              `json:"task_id"`
\tTaskStatus         string              `json:"task_status"`
\tCurrentTask        bool                `json:"current_task"`
\tEvidenceStatus     string              `json:"evidence_status"`
\tCoverage           string              `json:"coverage"`
\tCoverageReasons    []string            `json:"coverage_reasons,omitempty"`
\tCoverageScope      []string            `json:"coverage_scope"`
\tClaudeSessionIDs   []string            `json:"claude_session_ids"`
\tInFlightModelCalls int                 `json:"in_flight_model_calls,omitempty"`
\tIncluded           []string            `json:"included"`
\tMissing            []string            `json:"missing"`
\tUnattributed       []string            `json:"unattributed,omitempty"`
\tUnreadable         []string            `json:"unreadable,omitempty"`
\tCodexEvidence      []bundleCodexSource `json:"codex_evidence,omitempty"`
\tCollectionIndex    string              `json:"collection_index,omitempty"`
\tAnalysisIndex      string              `json:"analysis_index,omitempty"`
\tCreatedAt          string              `json:"created_at"`
}
'''
new_types = '''type bundleEvidenceProjection struct {
\tTaskID             string   `json:"task_id"`
\tTaskStatus         string   `json:"task_status"`
\tEvidenceStatus     string   `json:"evidence_status"`
\tCoverage           string   `json:"coverage"`
\tCoverageReasons    []string `json:"coverage_reasons,omitempty"`
\tCoverageScope      []string `json:"coverage_scope"`
\tClaudeSessionIDs   []string `json:"claude_session_ids"`
\tInFlightModelCalls int      `json:"in_flight_model_calls,omitempty"`
\tMissing            []string `json:"missing"`
\tUnattributed       []string `json:"unattributed,omitempty"`
\tUnreadable         []string `json:"unreadable,omitempty"`
}

type bundleOutput struct {
\tbundleEvidenceProjection
\tArchivePath string `json:"archive_path"`
}

type bundleManifest struct {
\tFormat string `json:"format"`
\tbundleEvidenceProjection
\tCurrentTask     bool                `json:"current_task"`
\tIncluded        []string            `json:"included"`
\tCodexEvidence   []bundleCodexSource `json:"codex_evidence,omitempty"`
\tCollectionIndex string              `json:"collection_index,omitempty"`
\tAnalysisIndex   string              `json:"analysis_index,omitempty"`
\tCreatedAt       string              `json:"created_at"`
}
'''
if old_types not in text:
    raise SystemExit("bundle transport type block not found")
text = text.replace(old_types, new_types, 1)

old_output = '''\treturn writeJSON(stdout, bundleOutput{
\t\tTaskID:             task.ID,
\t\tTaskStatus:         task.Status,
\t\tArchivePath:        archivePath,
\t\tEvidenceStatus:     summary.evidenceStatus,
\t\tCoverage:           summary.coverage,
\t\tCoverageReasons:    summary.coverageReasons,
\t\tCoverageScope:      bundleCoverageScope,
\t\tClaudeSessionIDs:   sessionIDs,
\t\tInFlightModelCalls: summary.inFlightModelCalls,
\t\tMissing:            summary.missing,
\t\tUnattributed:       summary.unattributed,
\t\tUnreadable:         summary.unreadable,
\t})
}
'''
new_output = '''\treturn writeJSON(stdout, bundleOutput{
\t\tbundleEvidenceProjection: summary.projection(task, sessionIDs),
\t\tArchivePath:              archivePath,
\t})
}
'''
if old_output not in text:
    raise SystemExit("bundle output projection block not found")
text = text.replace(old_output, new_output, 1)

marker = '''func (s *bundleEvidenceSummary) mergeReadability(index bundleCollectionIndex, task bundleTask) {
'''
projection = '''func (s bundleEvidenceSummary) projection(task bundleTask, sessionIDs []string) bundleEvidenceProjection {
\treturn bundleEvidenceProjection{
\t\tTaskID:             task.ID,
\t\tTaskStatus:         task.Status,
\t\tEvidenceStatus:     s.evidenceStatus,
\t\tCoverage:           s.coverage,
\t\tCoverageReasons:    s.coverageReasons,
\t\tCoverageScope:      bundleCoverageScope,
\t\tClaudeSessionIDs:   sessionIDs,
\t\tInFlightModelCalls: s.inFlightModelCalls,
\t\tMissing:            s.missing,
\t\tUnattributed:       s.unattributed,
\t\tUnreadable:         s.unreadable,
\t}
}

'''
if marker not in text:
    raise SystemExit("mergeReadability marker not found")
text = text.replace(marker, projection + marker, 1)

old_manifest = '''\treturn bundleManifest{
\t\tFormat:             bundleFormat,
\t\tTaskID:             task.ID,
\t\tTaskStatus:         task.Status,
\t\tCurrentTask:        task.Current,
\t\tEvidenceStatus:     summary.evidenceStatus,
\t\tCoverage:           summary.coverage,
\t\tCoverageReasons:    summary.coverageReasons,
\t\tCoverageScope:      bundleCoverageScope,
\t\tClaudeSessionIDs:   sessionIDs,
\t\tInFlightModelCalls: summary.inFlightModelCalls,
\t\tIncluded:           collector.includedListWithManifest(),
\t\tMissing:            summary.missing,
\t\tUnattributed:       summary.unattributed,
\t\tUnreadable:         summary.unreadable,
\t\tCodexEvidence:      codexEvidence,
\t\tCollectionIndex:    bundleCollectionEntryPath,
\t\tAnalysisIndex:      bundleAnalysisEntryPath,
\t\tCreatedAt:          time.Now().UTC().Format(time.RFC3339Nano),
\t}
}
'''
new_manifest = '''\treturn bundleManifest{
\t\tFormat:                   bundleFormat,
\t\tbundleEvidenceProjection: summary.projection(task, sessionIDs),
\t\tCurrentTask:              task.Current,
\t\tIncluded:                 collector.includedListWithManifest(),
\t\tCodexEvidence:            codexEvidence,
\t\tCollectionIndex:          bundleCollectionEntryPath,
\t\tAnalysisIndex:            bundleAnalysisEntryPath,
\t\tCreatedAt:                time.Now().UTC().Format(time.RFC3339Nano),
\t}
}
'''
if old_manifest not in text:
    raise SystemExit("bundle manifest projection block not found")
text = text.replace(old_manifest, new_manifest, 1)

path.write_text(text)
