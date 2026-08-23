package state

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

// parent review outcome / fix originの有限集合。親Codexがterminal packetへ取った行動を
// glm-worker側の確定境界だけで記録し、GLM modelによる意味推定とraw本文保存を行わない。
// これら集計はglm-worker側の親行動観測であってCodex actual token usageそのものではなく、
// Direct/orchestrated A/B評価の代替metricでもない。
const (
	ParentOutcomeAccepted = "accepted"
	ParentOutcomeFix      = "fix"
	ParentOutcomeDecision = "decision"
	ParentOutcomeUnknown  = "unknown"
)

// fix originの意味: codex-reviewは親Codexがterminal packet受領後の最終reviewで新たに検出した
// 差戻しだけ。GLM reviewerのterminal resultへ既に記載された指摘を親が差し戻す場合は
// glm-reviewerへ分離する。どちらにも確定できないときだけunknownとし、codex-reviewへ
// fail-open推定しない。
const (
	ParentOriginCodexReview    = "codex-review"
	ParentOriginGLMReviewer    = "glm-reviewer"
	ParentOriginUserAmendment  = "user-amendment"
	ParentOriginExternalReview = "external-review"
	ParentOriginMetadataRepair = "metadata-repair"
	ParentOriginUnknown        = "unknown"
)

var parentOutcomeKinds = map[string]bool{
	ParentOutcomeAccepted: true,
	ParentOutcomeFix:      true,
	ParentOutcomeDecision: true,
	ParentOutcomeUnknown:  true,
}

// ValidParentOriginは--origin値がfix originの有限集合へ一致するかだけを判定する。
// 未宣言(空)はunknown originとして記録するため、ここでは受理しない。
func ValidParentOrigin(value string) bool {
	switch value {
	case ParentOriginCodexReview, ParentOriginGLMReviewer, ParentOriginUserAmendment, ParentOriginExternalReview, ParentOriginMetadataRepair:
		return true
	}
	return false
}

// ParentReviewOpenStateはterminal packet emit時に開く未確定opportunityの識別。
// Role/ModelAlias/Riskはそのpacketを生成した直近のTask Work Callから持ち、
// outcome確定時のmodel/risk別集計とevent recordへの対応付けへ使う。
type ParentReviewOpenState struct {
	PacketStatus string `json:"packet_status"`
	Role         string `json:"role,omitempty"`
	ModelAlias   string `json:"model_alias,omitempty"`
	Risk         string `json:"risk,omitempty"`
}

// ParentReviewProducerはterminal packetを生成した直近のTask Work Callの識別。
type ParentReviewProducer struct {
	Role  string
	Model string
}

// openParentReviewはterminal packet 1件分のopportunityを開く。既に未確定opportunityが
// 残っている場合は新packetへ上書きされるため、破棄される側をtask closeと同じ
// resolveParentOutcomeのunknown確定へ渡し、内訳model/risk集計への帰属も同じ形で行って
// opportunity総数とoutcome総数の加法整合を保つ(fail-openなaccepted推定は行わない)。
func (stats *TaskStats) openParentReview(status string, risk string, producer ParentReviewProducer) {
	stats.resolveParentOutcome(ParentOutcomeUnknown, "")
	stats.ParentReviewOpen = &ParentReviewOpenState{
		PacketStatus: status,
		Role:         producer.Role,
		ModelAlias:   producer.Model,
		Risk:         risk,
	}
}

// resolveParentOutcomeは未確定opportunityをoutcome 1件へ確定する。未確定が無い場合は
// 何もせずfalseを返す(同じfix/decision/acceptの再実行での二重計上防止)。acceptedは
// 採用可能なreview系packetだけへ限定し、decision packetへの--acceptをfail closedする。
func (stats *TaskStats) resolveParentOutcome(kind, origin string) (ParentReviewOpenState, bool, error) {
	if !parentOutcomeKinds[kind] {
		return ParentReviewOpenState{}, false, fmt.Errorf("unknown parent outcome kind: %s", kind)
	}
	if kind == ParentOutcomeFix && origin != "" && !ValidParentOrigin(origin) {
		return ParentReviewOpenState{}, false, fmt.Errorf("unknown parent fix origin: %s", origin)
	}
	open := stats.ParentReviewOpen
	if open == nil {
		return ParentReviewOpenState{}, false, nil
	}
	if kind == ParentOutcomeAccepted && open.PacketStatus == string(packet.StatusNeedsSolDecision) {
		return ParentReviewOpenState{}, false, fmt.Errorf("pending Sol decision must be resolved with --decision before --accept")
	}
	resolved := *open
	addInt(&stats.ParentOutcomes, kind, 1)
	if kind == ParentOutcomeFix {
		if origin == "" {
			origin = ParentOriginUnknown
		}
		addInt(&stats.ParentFixOrigins, origin, 1)
	}
	unknownLabel := ParentOriginUnknown
	model := resolved.ModelAlias
	if model == "" {
		model = unknownLabel
	}
	addInt(&stats.ParentOutcomesByModel, model, 1)
	risk := resolved.Risk
	if risk == "" {
		risk = unknownLabel
	}
	addInt(&stats.ParentOutcomesByRisk, risk, 1)
	stats.ParentReviewOpen = nil
	return resolved, true, nil
}

// RecordParentOutcomeは現在taskの未確定parent review opportunityをoutcomeへ確定する。
// --accept・--fix・--decisionの各gate通過後に呼ぶ。未確定が無い再実行はno-opでfalseを返す。
// mirror書込失敗は他のstats観測と同じbest-effort警告へ留め、正規workflowを止めない。
func (s *StateStore) RecordParentOutcome(kind, origin string) (bool, error) {
	stats, err := s.loadTaskStats()
	if err != nil {
		stats, err = s.recoverTaskStats(err)
		if err != nil {
			return false, nil
		}
	}
	resolved, ok, resolveErr := stats.resolveParentOutcome(kind, origin)
	if !ok || resolveErr != nil {
		return ok, resolveErr
	}
	if err := s.writeTaskStats(stats); err != nil {
		warnStatsFailure("parent outcome更新", err)
		return false, nil
	}
	s.appendParentOutcomeEvent(stats.TaskID, parentPhaseOfKind(kind), kind, origin, resolved)
	return true, nil
}

// OpenParentReviewLabelは--status表示用に現在taskの未確定opportunity種別を返す。
func (s *StateStore) OpenParentReviewLabel() string {
	stats, err := s.loadTaskStats()
	if err != nil || stats.ParentReviewOpen == nil {
		return "none"
	}
	return stats.ParentReviewOpen.PacketStatus
}

// 親行動event recordの固定phase。parent-closeはtask close(new task開始・--reset)での
// 未確定opportunityのunknown確定を表す。
const (
	ParentPhaseAccept   = "parent-accept"
	ParentPhaseFix      = "parent-fix"
	ParentPhaseDecision = "parent-decision"
	ParentPhaseClose    = "parent-close"
)

func parentPhaseOfKind(kind string) string {
	switch kind {
	case ParentOutcomeAccepted:
		return ParentPhaseAccept
	case ParentOutcomeFix:
		return ParentPhaseFix
	case ParentOutcomeDecision:
		return ParentPhaseDecision
	default:
		return ParentPhaseClose
	}
}

func (s *StateStore) appendParentOutcomeEvent(taskID string, phase string, kind string, origin string, resolved ParentReviewOpenState) {
	now := time.Now().UTC()
	s.RecordModelCallLog(ModelCallLog{
		TaskID:             taskID,
		CallType:           CallTypeEvent,
		StartedAt:          now,
		CompletedAt:        now,
		Phase:              phase,
		Role:               SessionRole(resolved.Role),
		Outcome:            kind,
		PacketStatus:       resolved.PacketStatus,
		ModelAlias:         resolved.ModelAlias,
		WorkerReportedRisk: resolved.Risk,
		ParentOrigin:       origin,
	})
}

// rework集計のcoverage label。task呼出recordが読めない・足りないtaskがあるときunknownへ
// 落とし、部分logからの増分を完全値として扱わない。
const (
	ParentReworkCoverageComplete = "complete"
	ParentReworkCoverageUnknown  = "unknown"
)

// ParentReworkOriginはfix origin別の差し戻し後追加消費。該当originのfix outcome eventより
// 後・次の親行動outcome eventより前のTask Work Callだけを数える。
type ParentReworkOrigin struct {
	Calls            int
	WorkerCalls      int
	ReviewerCalls    int
	Turns            int
	TreeInputTokens  int64
	TreeOutputTokens int64
	WallDurationMS   int64
}

// ParentReworkSummaryは--stats用のorigin別rework集計。Coverageはtask call記録と
// TaskStats model_callsの対応が取れないtaskがある場合にunknownへ落とし、部分logからの
// 増分を完全値として扱わない。
type ParentReworkSummary struct {
	ByOrigin map[string]ParentReworkOrigin
	Coverage string
}

func isParentOutcomePhase(phase string) bool {
	switch phase {
	case ParentPhaseAccept, ParentPhaseFix, ParentPhaseDecision, ParentPhaseClose:
		return true
	}
	return false
}

// ComputeParentReworkは集計対象taskのtelemetry JSONLからfix origin別の差し戻し後追加消費を
// 導出する。追加AI callも新規snapshot保存も行わず、既存task呼出recordのusage/turn/durationを
// 親行動eventでのみ区切る。record欠損taskはCoverageをunknownへ落とすだけで補完しない。
func (s *StateStore) ComputeParentRework(tasks []TaskStats) ParentReworkSummary {
	summary := ParentReworkSummary{ByOrigin: make(map[string]ParentReworkOrigin), Coverage: ParentReworkCoverageComplete}
	for _, task := range tasks {
		logs, err := s.ReadModelCallLogs(task.TaskID)
		taskCalls := 0
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) || task.ModelCalls > 0 {
				summary.Coverage = ParentReworkCoverageUnknown
			}
			continue
		}
		origin := ""
		for _, record := range logs {
			if record.CallType == CallTypeEvent && isParentOutcomePhase(record.Phase) {
				if record.Outcome == ParentOutcomeFix {
					origin = record.ParentOrigin
					if origin == "" {
						origin = ParentOriginUnknown
					}
				} else {
					origin = ""
				}
				continue
			}
			if record.CallType != CallTypeTask {
				continue
			}
			taskCalls++
			if origin == "" {
				continue
			}
			entry := summary.ByOrigin[origin]
			entry.Calls++
			switch record.Role {
			case WorkerRole:
				entry.WorkerCalls++
			case ReviewerRole:
				entry.ReviewerCalls++
			}
			entry.Turns += record.TopLevelTurns
			usage := modelCallTreeUsage(record)
			entry.TreeInputTokens += usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
			entry.TreeOutputTokens += usage.OutputTokens
			entry.WallDurationMS += record.WallDurationMS
			summary.ByOrigin[origin] = entry
		}
		if taskCalls != task.ModelCalls {
			summary.Coverage = ParentReworkCoverageUnknown
		}
	}
	return summary
}
