package app

import "time"

func analysisExecutionTokenDelta(association codexAssociation, scan bundleRolloutScan, scanErr error, start time.Time, execution analysisExecutionBoundary, collectionEnd time.Time) bundleAnalysisTokenDelta {
	delta := bundleAnalysisTokenDelta{Status: analysisStatusAvailable}
	if association.ParentStatus != codexStatusIncluded {
		delta.Status = association.ParentStatus
		return delta
	}
	if scanErr != nil {
		delta.Status = analysisStatusUnreadable
		return delta
	}
	if execution.status == analysisStatusUnknown {
		delta.Status = analysisStatusUnknown
		return delta
	}
	endBound := collectionEnd
	if execution.status == analysisStatusAvailable {
		endBound = execution.end
	}
	delta = analysisAnchoredTokenDelta(scan, start, endBound)
	if execution.status == analysisStatusOpen && delta.Status == analysisStatusAvailable {
		delta.Status = analysisStatusOpen
	}
	return delta
}

func analysisFinalizationTokenDelta(association codexAssociation, scan bundleRolloutScan, execution analysisExecutionBoundary, owning analysisOwningTurn, interval bundleAnalysisInterval) bundleAnalysisTokenDelta {
	delta := bundleAnalysisTokenDelta{Status: interval.Status}
	if association.ParentStatus != codexStatusIncluded || interval.Status != analysisStatusAvailable {
		return delta
	}
	return analysisAnchoredTokenDelta(scan, execution.end, owning.turn.CompletedAt)
}

func analysisAnchoredTokenDelta(scan bundleRolloutScan, baselineBound, endBound time.Time) bundleAnalysisTokenDelta {
	baseline, hasBaseline := lastTokenAnchorAtOrBefore(scan, baselineBound)
	end, hasEnd := lastTokenAnchorAtOrBefore(scan, endBound)
	delta := bundleAnalysisTokenDelta{Status: analysisStatusAvailable}
	switch {
	case !hasBaseline || !hasEnd:
		delta.Status = analysisStatusMissing
	case end.Offset <= baseline.Offset:
		delta.Status = analysisStatusNoObservation
		delta.BaselineAt = baseline.RawAt
	}
	if delta.Status != analysisStatusAvailable {
		return delta
	}
	segments := analysisTokenSegments(scan, baseline, end)
	if analysisSegmentsCounterReset(scan, segments) {
		delta.Status = analysisStatusCounterReset
		delta.BaselineAt = baseline.RawAt
		delta.EndAt = end.RawAt
		return delta
	}
	inputTokens, inputKnown := analysisSegmentFieldSum(segments, analysisAnchorInput)
	cachedInputTokens, cachedInputKnown := analysisSegmentFieldSum(segments, analysisAnchorCached)
	delta.BaselineAt = baseline.RawAt
	delta.EndAt = end.RawAt
	if !inputKnown || !cachedInputKnown {
		delta.Status = analysisStatusUnknown
		return delta
	}
	delta.InputTokens = inputTokens
	delta.CachedInputTokens = cachedInputTokens
	return delta
}

func analysisTokenSegments(scan bundleRolloutScan, baseline, end analysisRolloutTokenAnchor) []analysisTokenSegment {
	if baseline.File == end.File {
		return []analysisTokenSegment{{file: baseline.File, baseline: &baseline, end: &end}}
	}
	segments := make([]analysisTokenSegment, 0, end.File-baseline.File+1)
	for file := baseline.File; file <= end.File; file++ {
		_, last := analysisFileTokenAnchorBounds(scan, file)
		if file == baseline.File {
			if last == nil {
				continue
			}
			segments = append(segments, analysisTokenSegment{file: file, baseline: &baseline, end: last})
			continue
		}
		if file == end.File {
			segments = append(segments, analysisTokenSegment{file: file, baseline: nil, end: &end})
			continue
		}
		if last == nil {
			continue
		}
		segments = append(segments, analysisTokenSegment{file: file, baseline: nil, end: last})
	}
	return segments
}

func analysisFileTokenAnchorBounds(scan bundleRolloutScan, file int) (*analysisRolloutTokenAnchor, *analysisRolloutTokenAnchor) {
	var first, last *analysisRolloutTokenAnchor
	for index := range scan.tokens {
		if scan.tokens[index].File < file {
			continue
		}
		if scan.tokens[index].File > file {
			break
		}
		if first == nil {
			first = &scan.tokens[index]
		}
		last = &scan.tokens[index]
	}
	return first, last
}

func analysisSegmentsCounterReset(scan bundleRolloutScan, segments []analysisTokenSegment) bool {
	for _, segment := range segments {
		from := segment.baseline
		if from == nil {
			first, _ := analysisFileTokenAnchorBounds(scan, segment.file)
			if first == nil {
				continue
			}
			from = first
		}
		if from.Offset == segment.end.Offset {
			continue
		}
		if analysisCountersResetBetween(scan, *from, *segment.end) {
			return true
		}
	}
	return false
}

func analysisAnchorInput(anchor *analysisRolloutTokenAnchor) *int64 {
	return anchor.Input
}

func analysisAnchorCached(anchor *analysisRolloutTokenAnchor) *int64 {
	return anchor.Cached
}

func analysisAnchorOutput(anchor *analysisRolloutTokenAnchor) *int64 {
	return anchor.Output
}

func analysisAnchorReasoning(anchor *analysisRolloutTokenAnchor) *int64 {
	return anchor.Reasoning
}

func analysisAnchorTotal(anchor *analysisRolloutTokenAnchor) *int64 {
	return anchor.Total
}

func analysisSegmentFieldSum(segments []analysisTokenSegment, field func(*analysisRolloutTokenAnchor) *int64) (int64, bool) {
	var total int64
	for _, segment := range segments {
		if segment.baseline != nil {
			delta := analysisCounterDeltaState(field(segment.baseline), field(segment.end))
			if !delta.Known {
				return 0, false
			}
			total += delta.Value
			continue
		}
		value := field(segment.end)
		if value == nil {
			return 0, false
		}
		total += *value
	}
	return total, true
}

func analysisCounterDeltaState(baseline, end *int64) analysisCounterDelta {
	switch {
	case baseline == nil && end == nil:
		return analysisCounterDelta{MissingInBaseline: true, MissingInEnd: true}
	case baseline == nil:
		return analysisCounterDelta{MissingInBaseline: true}
	case end == nil:
		return analysisCounterDelta{MissingInEnd: true}
	default:
		return analysisCounterDelta{Value: *end - *baseline, Known: true}
	}
}

func analysisAnchorsCounterReset(baseline, end analysisRolloutTokenAnchor) bool {
	pairs := [][2]*int64{
		{baseline.Input, end.Input},
		{baseline.Cached, end.Cached},
		{baseline.Output, end.Output},
		{baseline.Reasoning, end.Reasoning},
		{baseline.Total, end.Total},
	}
	for _, pair := range pairs {
		if pair[0] != nil && pair[1] != nil && *pair[1] < *pair[0] {
			return true
		}
	}
	return false
}

func analysisCountersResetBetween(scan bundleRolloutScan, baseline, end analysisRolloutTokenAnchor) bool {
	lastKnown := baseline
	for _, anchor := range scan.tokens {
		if anchor.Offset <= baseline.Offset {
			continue
		}
		if anchor.Offset > end.Offset {
			break
		}
		if analysisAnchorsCounterReset(lastKnown, anchor) {
			return true
		}
		lastKnown = analysisAnchorWithLastKnownFields(lastKnown, anchor)
	}
	return false
}

func analysisAnchorWithLastKnownFields(known, observed analysisRolloutTokenAnchor) analysisRolloutTokenAnchor {
	if observed.Input != nil {
		known.Input = observed.Input
	}
	if observed.Cached != nil {
		known.Cached = observed.Cached
	}
	if observed.Output != nil {
		known.Output = observed.Output
	}
	if observed.Reasoning != nil {
		known.Reasoning = observed.Reasoning
	}
	if observed.Total != nil {
		known.Total = observed.Total
	}
	return known
}
