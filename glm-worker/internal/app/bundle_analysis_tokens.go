package app

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
