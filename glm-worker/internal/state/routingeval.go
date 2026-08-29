package state

import (
	"fmt"
	"slices"
	"strings"
)

type ModelRoutingMetrics struct {
	TreeUsage string `json:"tree_usage"`
	Cost      string `json:"cost"`
	Quality   string `json:"quality"`
}

type ModelRoutingSufficiency struct {
	MinQualityCalls int    `json:"min_quality_calls_per_group"`
	MinQualityTasks int    `json:"min_quality_tasks_per_group"`
	Rule            string `json:"rule"`
}

type ModelRoutingCell struct {
	Role              string         `json:"role"`
	Phase             string         `json:"phase"`
	EffectiveRisk     string         `json:"effective_risk"`
	ConvergenceDelta  string         `json:"convergence_delta"`
	ModelAlias        string         `json:"model_alias"`
	ResolvedModel     string         `json:"resolved_model"`
	Calls             int            `json:"calls"`
	TopLevelCalls     int            `json:"top_level_calls"`
	Tasks             int            `json:"tasks"`
	Sessions          int            `json:"sessions"`
	Usage             TokenUsage     `json:"usage"`
	UsageUnknownCalls int            `json:"usage_unknown_calls"`
	Outcomes          map[string]int `json:"outcomes"`
	PacketStatuses    map[string]int `json:"packet_statuses"`
}

type ModelRoutingQualityGroup struct {
	Role             string         `json:"role"`
	Phase            string         `json:"phase"`
	EffectiveRisk    string         `json:"effective_risk"`
	ConvergenceDelta string         `json:"convergence_delta"`
	ResolvedModel    string         `json:"resolved_model"`
	Calls            int            `json:"calls"`
	Tasks            int            `json:"tasks"`
	Outcomes         map[string]int `json:"outcomes"`
	Sufficient       bool           `json:"sufficient"`
}

type ModelRoutingAliasLink struct {
	Role           string         `json:"role"`
	ModelAlias     string         `json:"model_alias"`
	ResolvedModels map[string]int `json:"resolved_models"`
}

type ModelRoutingEvaluation struct {
	QualityDelta     string   `json:"quality_delta"`
	Reasons          []string `json:"reasons"`
	ComparableGroups []string `json:"comparable_groups,omitempty"`
}

type ModelRoutingReport struct {
	Metrics       ModelRoutingMetrics        `json:"metrics"`
	Sufficiency   ModelRoutingSufficiency    `json:"sufficiency"`
	Records       CallRecordCounts           `json:"records"`
	Cells         []ModelRoutingCell         `json:"cells"`
	QualityGroups []ModelRoutingQualityGroup `json:"quality_groups"`
	AliasLinks    []ModelRoutingAliasLink    `json:"alias_links"`
	Evaluation    ModelRoutingEvaluation     `json:"evaluation"`
}

type modelRoutingCellKey struct {
	role  string
	phase string
	risk  string
	delta string
	alias string
	model string
}

type modelRoutingCellSamples struct {
	cell     ModelRoutingCell
	tasks    map[string]bool
	sessions map[string]bool
}

type modelRoutingGroupKey struct {
	role  string
	phase string
	risk  string
	delta string
}

type modelRoutingModelSamples struct {
	calls    int
	tasks    map[string]bool
	outcomes map[string]int
}

type modelRoutingBuilder struct {
	report        ModelRoutingReport
	cells         map[modelRoutingCellKey]*modelRoutingCellSamples
	aliasLinks    map[string]map[string]map[string]int
	qualityGroups map[modelRoutingGroupKey]map[string]*modelRoutingModelSamples
	taskCalls     int
}

const (
	ModelRoutingUnknownModel             = "unknown"
	ModelRoutingUnknownRisk              = "unknown"
	ModelRoutingMinQualityCallsPerGroup  = 20
	ModelRoutingMinQualityTasksPerGroup  = 5
	ModelRoutingQualityReviewPass        = "review_pass"
	ModelRoutingQualityReviewFixRequired = "review_fix_required"
)

const (
	ModelRoutingQualityDeltaUnknown      = "unknown"
	ModelRoutingQualityDeltaInsufficient = "insufficient"
	ModelRoutingQualityDeltaComparable   = "comparable"
)

const modelRoutingTreeUsageMetric = "existing ModelCallLog v3 tree_usage: input_tokens, cache_creation_input_tokens, cache_read_input_tokens, output_tokens summed over resolved_model_usage models, falling back to top_level_usage when the map is empty; no new metric is defined"

const modelRoutingCostMetric = "per-cell token totals attributed with the existing definitions: calls with resolved_model_usage attribute each model's own tokens to that model's cell, calls without it attribute the tree_usage fallback value to the resolved_model_id cell (or unknown), and calls with neither source count only in usage_unknown_calls"

const modelRoutingQualityMetric = "quality_groups contain only deterministic downstream review outcomes attributed to one producing worker call and its top-level resolved model: review_pass or review_fix_required; operational outcome and packet_status remain in cells for reliability/debugging and are not model-quality evidence; unresolved Sol routing, provider failures, ambiguous producing calls, and unattributable mixed-model results remain absent from quality_groups; no composite quality score is defined"

const modelRoutingSufficiencyRule = "within one repository, controlled model quality comparison uses alias-independent quality_groups only: effective risk and convergence delta must both be known, and at least 2 distinct resolved models in the same role+normalized-phase+effective-risk+convergence-delta group must each have at least min_quality_calls_per_group attributable downstream quality calls across at least min_quality_tasks_per_group distinct tasks; operational calls without attributable quality do not count, meeting the minimums only lists a comparison candidate and is neither statistical proof nor a routing/downgrade criterion"

func BuildModelRoutingReport(tasks []TaskCallLogs) ModelRoutingReport {
	builder := newModelRoutingBuilder()
	builder.absorbTasks(tasks)
	builder.buildCells()
	builder.buildQualityGroups()
	builder.buildAliasLinks()
	builder.buildEvaluation()
	return builder.report
}

func newModelRoutingBuilder() *modelRoutingBuilder {
	return &modelRoutingBuilder{
		report: ModelRoutingReport{
			Metrics: ModelRoutingMetrics{
				TreeUsage: modelRoutingTreeUsageMetric,
				Cost:      modelRoutingCostMetric,
				Quality:   modelRoutingQualityMetric,
			},
			Sufficiency: ModelRoutingSufficiency{
				MinQualityCalls: ModelRoutingMinQualityCallsPerGroup,
				MinQualityTasks: ModelRoutingMinQualityTasksPerGroup,
				Rule:            modelRoutingSufficiencyRule,
			},
			Records:       CallRecordCounts{},
			Cells:         []ModelRoutingCell{},
			QualityGroups: []ModelRoutingQualityGroup{},
			AliasLinks:    []ModelRoutingAliasLink{},
			Evaluation:    ModelRoutingEvaluation{QualityDelta: ModelRoutingQualityDeltaUnknown, Reasons: []string{}},
		},
		cells:         make(map[modelRoutingCellKey]*modelRoutingCellSamples),
		aliasLinks:    make(map[string]map[string]map[string]int),
		qualityGroups: make(map[modelRoutingGroupKey]map[string]*modelRoutingModelSamples),
	}
}

func (b *modelRoutingBuilder) absorbTasks(tasks []TaskCallLogs) {
	for _, task := range tasks {
		for _, log := range task.Logs {
			b.absorbLog(task, log)
		}
	}
}

func (b *modelRoutingBuilder) absorbLog(task TaskCallLogs, log ModelCallLog) {
	b.report.Records.Read++
	switch log.CallType {
	case CallTypeTask:
		b.report.Records.Task++
	case CallTypeEvent:
		b.report.Records.Event++
	case CallTypeProbe:
		b.report.Records.Probe++
	default:
		b.report.Records.Other++
	}
	if log.CallType != CallTypeTask {
		return
	}
	b.taskCalls++
	phase := routingPhaseCategory(log.Role, log.Phase)
	group := modelRoutingGroupKey{
		role:  string(log.Role),
		phase: phase,
		risk:  routingEffectiveRiskClass(log),
		delta: routingConvergenceDeltaClass(task, log),
	}
	owner := routingResultOwner(log)
	for _, model := range routingResolvedModels(log) {
		b.absorbCell(task.TaskID, log, group, model, owner == model)
	}
	if outcome := routingQualityOutcome(task, log); outcome != "" && owner != "" && owner != ModelRoutingUnknownModel {
		b.absorbQualityGroup(task.TaskID, group, owner, outcome)
	}
}

func routingEffectiveRiskClass(log ModelCallLog) string {
	if log.EffectiveRisk == "" {
		return ModelRoutingUnknownRisk
	}
	return log.EffectiveRisk
}

func routingConvergenceDeltaClass(task TaskCallLogs, log ModelCallLog) string {
	if log.CallID == "" {
		return RoundDeltaUnknown
	}
	if class, ok := task.ConvergenceDeltas[log.CallID]; ok && class != "" {
		return class
	}
	return RoundDeltaUnknown
}

func routingPhaseCategory(role SessionRole, phase string) string {
	switch role {
	case WorkerRole:
		return WorkerPhaseCategory(phase)
	case ReviewerRole:
		return ReviewerPhaseCategory(phase)
	default:
		return phase
	}
}

func routingResolvedModels(log ModelCallLog) []string {
	seen := make(map[string]bool)
	for model := range log.ResolvedModelUsage {
		seen[model] = true
	}
	if log.ResolvedModelID != "" {
		seen[log.ResolvedModelID] = true
	}
	if len(seen) == 0 {
		return []string{ModelRoutingUnknownModel}
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	slices.Sort(models)
	return models
}

func routingResultOwner(log ModelCallLog) string {
	if log.ResolvedModelID != "" {
		return log.ResolvedModelID
	}
	if len(log.ResolvedModelUsage) == 1 {
		for model := range log.ResolvedModelUsage {
			return model
		}
	}
	if len(log.ResolvedModelUsage) == 0 {
		return ModelRoutingUnknownModel
	}
	return ""
}

func routingQualityOutcome(task TaskCallLogs, log ModelCallLog) string {
	if log.Role != WorkerRole || log.CallID == "" {
		return ""
	}
	outcome := task.QualityOutcomes[log.CallID]
	switch outcome {
	case ModelRoutingQualityReviewPass, ModelRoutingQualityReviewFixRequired:
		return outcome
	default:
		return ""
	}
}

func (b *modelRoutingBuilder) absorbCell(taskID string, log ModelCallLog, group modelRoutingGroupKey, model string, ownsTopLevel bool) {
	key := modelRoutingCellKey{role: group.role, phase: group.phase, risk: group.risk, delta: group.delta, alias: log.ModelAlias, model: model}
	samples := b.cells[key]
	if samples == nil {
		samples = &modelRoutingCellSamples{
			cell: ModelRoutingCell{
				Role: group.role, Phase: group.phase, EffectiveRisk: group.risk, ConvergenceDelta: group.delta,
				ModelAlias: log.ModelAlias, ResolvedModel: model,
				Outcomes: map[string]int{}, PacketStatuses: map[string]int{},
			},
			tasks:    make(map[string]bool),
			sessions: make(map[string]bool),
		}
		b.cells[key] = samples
	}
	samples.cell.Calls++
	samples.tasks[taskID] = true
	if log.SessionID != "" {
		samples.sessions[log.SessionID] = true
	}
	if ownsTopLevel {
		samples.cell.TopLevelCalls++
		addInt(&samples.cell.Outcomes, log.Outcome, 1)
		addInt(&samples.cell.PacketStatuses, log.PacketStatus, 1)
	}
	absorbModelRoutingCellUsage(samples, log, model)
	b.absorbAliasLink(group.role, log.ModelAlias, model)
}

func (b *modelRoutingBuilder) absorbQualityGroup(taskID string, group modelRoutingGroupKey, model string, outcome string) {
	models := b.qualityGroups[group]
	if models == nil {
		models = make(map[string]*modelRoutingModelSamples)
		b.qualityGroups[group] = models
	}
	samples := models[model]
	if samples == nil {
		samples = &modelRoutingModelSamples{tasks: make(map[string]bool), outcomes: make(map[string]int)}
		models[model] = samples
	}
	samples.calls++
	samples.tasks[taskID] = true
	addInt(&samples.outcomes, outcome, 1)
}

func (b *modelRoutingBuilder) absorbAliasLink(role string, alias string, model string) {
	if alias == "" {
		return
	}
	aliasModels := b.aliasLinks[role]
	if aliasModels == nil {
		aliasModels = make(map[string]map[string]int)
		b.aliasLinks[role] = aliasModels
	}
	if aliasModels[alias] == nil {
		aliasModels[alias] = make(map[string]int)
	}
	aliasModels[alias][model]++
}

func absorbModelRoutingCellUsage(samples *modelRoutingCellSamples, log ModelCallLog, model string) {
	switch {
	case len(log.ResolvedModelUsage) > 0:
		usage, ok := log.ResolvedModelUsage[model]
		if !ok {
			samples.cell.UsageUnknownCalls++
			return
		}
		samples.cell.Usage.InputTokens += usage.InputTokens
		samples.cell.Usage.CacheCreationInputTokens += usage.CacheCreationInputTokens
		samples.cell.Usage.CacheReadInputTokens += usage.CacheReadInputTokens
		samples.cell.Usage.OutputTokens += usage.OutputTokens
	case log.TopLevelUsage != (TokenUsage{}):
		usage := modelCallTreeUsage(log)
		samples.cell.Usage.InputTokens += usage.InputTokens
		samples.cell.Usage.CacheCreationInputTokens += usage.CacheCreationInputTokens
		samples.cell.Usage.CacheReadInputTokens += usage.CacheReadInputTokens
		samples.cell.Usage.OutputTokens += usage.OutputTokens
	default:
		samples.cell.UsageUnknownCalls++
	}
}

func (b *modelRoutingBuilder) buildCells() {
	for _, samples := range b.cells {
		samples.cell.Tasks = len(samples.tasks)
		samples.cell.Sessions = len(samples.sessions)
		b.report.Cells = append(b.report.Cells, samples.cell)
	}
	slices.SortFunc(b.report.Cells, compareModelRoutingCell)
}

func (b *modelRoutingBuilder) buildQualityGroups() {
	for _, group := range sortedModelRoutingGroups(b.qualityGroups) {
		models := make([]string, 0, len(b.qualityGroups[group]))
		for model := range b.qualityGroups[group] {
			models = append(models, model)
		}
		slices.Sort(models)
		for _, model := range models {
			samples := b.qualityGroups[group][model]
			b.report.QualityGroups = append(b.report.QualityGroups, ModelRoutingQualityGroup{
				Role: group.role, Phase: group.phase, EffectiveRisk: group.risk, ConvergenceDelta: group.delta,
				ResolvedModel: model, Calls: samples.calls, Tasks: len(samples.tasks), Outcomes: samples.outcomes,
				Sufficient: routingQualityGroupSufficient(group, samples),
			})
		}
	}
}

func routingQualityGroupSufficient(group modelRoutingGroupKey, samples *modelRoutingModelSamples) bool {
	return group.risk != ModelRoutingUnknownRisk && group.delta != RoundDeltaUnknown &&
		samples.calls >= ModelRoutingMinQualityCallsPerGroup && len(samples.tasks) >= ModelRoutingMinQualityTasksPerGroup
}

func (b *modelRoutingBuilder) buildAliasLinks() {
	for role, aliasModels := range b.aliasLinks {
		for alias, models := range aliasModels {
			b.report.AliasLinks = append(b.report.AliasLinks, ModelRoutingAliasLink{
				Role: role, ModelAlias: alias, ResolvedModels: models,
			})
		}
	}
	slices.SortFunc(b.report.AliasLinks, compareModelRoutingAliasLink)
}

func (b *modelRoutingBuilder) buildEvaluation() {
	models := b.distinctResolvedModels()
	if len(models) < 2 {
		b.buildUnknownEvaluation(models)
		return
	}
	b.buildContrastEvaluation(models)
}

func (b *modelRoutingBuilder) distinctResolvedModels() []string {
	seen := make(map[string]bool)
	for _, samples := range b.qualityGroups {
		for model := range samples {
			if model != ModelRoutingUnknownModel {
				seen[model] = true
			}
		}
	}
	return sortedKeys(seen)
}

func (b *modelRoutingBuilder) buildUnknownEvaluation(models []string) {
	reasons := []string{}
	switch {
	case b.taskCalls == 0:
		reasons = append(reasons, "no task-call telemetry recorded")
	case len(models) == 0:
		reasons = append(reasons, "no attributable downstream quality evidence; operational outcome and packet_status are not model-quality evidence")
	default:
		reasons = append(reasons, fmt.Sprintf(
			"resolved-model quality contrast requires at least 2 distinct resolved models with attributable downstream evidence; observed only %s",
			strings.Join(models, ", ")))
	}
	if reason, ok := b.aliasOnlyReason(models); ok {
		reasons = append(reasons, reason)
	}
	b.report.Evaluation = ModelRoutingEvaluation{
		QualityDelta:     ModelRoutingQualityDeltaUnknown,
		Reasons:          reasons,
		ComparableGroups: []string{},
	}
}

func (b *modelRoutingBuilder) buildContrastEvaluation(models []string) {
	reasons := make([]string, 0)
	comparable := make([]string, 0)
	for _, group := range sortedModelRoutingGroups(b.qualityGroups) {
		sufficient, shortfalls := routingGroupSufficientModels(group, b.qualityGroups[group], models)
		reasons = append(reasons, shortfalls...)
		if len(sufficient) < 2 {
			continue
		}
		comparable = append(comparable, fmt.Sprintf("%s: %s", modelRoutingGroupLabel(group), strings.Join(sufficient, ", ")))
	}
	qualityDelta := ModelRoutingQualityDeltaInsufficient
	if len(comparable) > 0 {
		qualityDelta = ModelRoutingQualityDeltaComparable
	}
	if len(comparable) == 0 && len(reasons) == 0 {
		reasons = append(reasons, "no role+normalized-phase+effective-risk+convergence-delta group contains at least 2 of the observed resolved models")
	}
	if reason, ok := b.aliasOnlyReason(models); ok {
		reasons = append(reasons, reason)
	}
	b.report.Evaluation = ModelRoutingEvaluation{
		QualityDelta:     qualityDelta,
		Reasons:          reasons,
		ComparableGroups: comparable,
	}
}

func modelRoutingGroupLabel(group modelRoutingGroupKey) string {
	return fmt.Sprintf("%s/%s/risk=%s/delta=%s", group.role, group.phase, group.risk, group.delta)
}

func routingGroupSufficientModels(group modelRoutingGroupKey, groupModels map[string]*modelRoutingModelSamples, models []string) ([]string, []string) {
	candidates := routingGroupCandidates(groupModels, models)
	if len(candidates) < 2 {
		return nil, nil
	}
	if group.risk == ModelRoutingUnknownRisk || group.delta == RoundDeltaUnknown {
		return nil, []string{fmt.Sprintf("%s: controlled quality comparison requires known effective risk and convergence delta", modelRoutingGroupLabel(group))}
	}
	sufficient := make([]string, 0, len(candidates))
	shortfalls := make([]string, 0)
	for _, model := range candidates {
		samples := groupModels[model]
		if routingQualityGroupSufficient(group, samples) {
			sufficient = append(sufficient, model)
			continue
		}
		shortfalls = append(shortfalls, fmt.Sprintf(
			"%s: %s has %d attributable quality calls across %d tasks, below min %d calls / %d tasks",
			modelRoutingGroupLabel(group), model, samples.calls, len(samples.tasks),
			ModelRoutingMinQualityCallsPerGroup, ModelRoutingMinQualityTasksPerGroup))
	}
	return sufficient, shortfalls
}

func routingGroupCandidates(groupModels map[string]*modelRoutingModelSamples, models []string) []string {
	candidates := make([]string, 0, len(groupModels))
	for _, model := range models {
		if _, ok := groupModels[model]; ok {
			candidates = append(candidates, model)
		}
	}
	return candidates
}

func (b *modelRoutingBuilder) aliasOnlyReason(models []string) (string, bool) {
	aliases := routingObservedAliases(b.aliasLinks)
	if len(aliases) < 2 || !routingAliasesShareModels(b.aliasLinks, models) {
		return "", false
	}
	return fmt.Sprintf(
		"%d model aliases resolve to the same resolved model set (%s); alias-level differences are not model quality evidence",
		len(aliases), strings.Join(models, ", ")), true
}

func routingObservedAliases(aliasLinks map[string]map[string]map[string]int) map[string]bool {
	aliases := make(map[string]bool)
	for _, aliasModels := range aliasLinks {
		for alias := range aliasModels {
			aliases[alias] = true
		}
	}
	return aliases
}

func routingAliasesShareModels(aliasLinks map[string]map[string]map[string]int, models []string) bool {
	for _, aliasModels := range aliasLinks {
		for _, linkModels := range aliasModels {
			if !routingLinkCoversModels(linkModels, models) {
				return false
			}
		}
	}
	return true
}

func routingLinkCoversModels(linkModels map[string]int, models []string) bool {
	known := 0
	for model := range linkModels {
		if model == ModelRoutingUnknownModel {
			continue
		}
		if !slices.Contains(models, model) {
			return false
		}
		known++
	}
	return known == len(models)
}

func sortedModelRoutingGroups(groups map[modelRoutingGroupKey]map[string]*modelRoutingModelSamples) []modelRoutingGroupKey {
	keys := make([]modelRoutingGroupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareModelRoutingGroup)
	return keys
}

func compareModelRoutingGroup(a, b modelRoutingGroupKey) int {
	if a.role != b.role {
		return strings.Compare(a.role, b.role)
	}
	if a.phase != b.phase {
		return strings.Compare(a.phase, b.phase)
	}
	if a.risk != b.risk {
		return strings.Compare(a.risk, b.risk)
	}
	return strings.Compare(a.delta, b.delta)
}

func compareModelRoutingCell(a, b ModelRoutingCell) int {
	if a.Role != b.Role {
		return strings.Compare(a.Role, b.Role)
	}
	if a.Phase != b.Phase {
		return strings.Compare(a.Phase, b.Phase)
	}
	if a.EffectiveRisk != b.EffectiveRisk {
		return strings.Compare(a.EffectiveRisk, b.EffectiveRisk)
	}
	if a.ConvergenceDelta != b.ConvergenceDelta {
		return strings.Compare(a.ConvergenceDelta, b.ConvergenceDelta)
	}
	if a.ModelAlias != b.ModelAlias {
		return strings.Compare(a.ModelAlias, b.ModelAlias)
	}
	return strings.Compare(a.ResolvedModel, b.ResolvedModel)
}

func compareModelRoutingAliasLink(a, b ModelRoutingAliasLink) int {
	if a.Role != b.Role {
		return strings.Compare(a.Role, b.Role)
	}
	return strings.Compare(a.ModelAlias, b.ModelAlias)
}
