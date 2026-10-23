package bundleanalysis

import (
	"bytes"
	"time"
)

type ParentUsageEvidence struct {
	Source  string
	Content []byte
}

type ParentUsageObservation struct {
	Tokens   parentUsageTokens   `json:"tokens"`
	Activity parentUsageActivity `json:"activity"`
	Sources  []string            `json:"sources,omitempty"`
	Unknown  string              `json:"unknown,omitempty"`
}

func ObserveParentUsage(sources []ParentUsageEvidence, start, end time.Time) ParentUsageObservation {
	result := ParentUsageObservation{
		Tokens:   parentUsageTokens{Status: analysisStatusUnknown},
		Activity: parentUsageActivity{Status: analysisStatusUnknown},
	}
	if len(sources) == 0 || start.IsZero() || end.IsZero() || end.Before(start) {
		result.Unknown = "parent rollout or observation window unavailable"
		return result
	}
	scan := bundleRolloutScan{turnIndex: map[string]int{}}
	for _, source := range sources {
		if err := scanCodexRolloutReader(&scan, bytes.NewReader(source.Content), source.Source, false, start, end); err != nil {
			result.Unknown = "parent rollout is unreadable: " + source.Source
			return result
		}
		if len(result.Sources) < 16 {
			result.Sources = append(result.Sources, source.Source)
		}
	}
	scan.finalizeTurns()
	result.Tokens = parentUsageAnchoredTokens(scan, start, end, "exported-parent-rollouts")
	result.Activity = parentUsageIntervalActivity(scan, start, end, "exported-parent-rollouts", true)
	return result
}
