package parentevidence

import "io"

const MaxOutputBytes = 96 * 1024

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

func applyTotalBudget(output *Output) bool {
	output.Status = aggregateStatus(output.Parts)
	for outputSize(*output) > MaxOutputBytes && stripBody(output) {
		output.Status = aggregateStatus(output.Parts)
	}
	if outputSize(*output) > MaxOutputBytes {
		*output = Output{
			Version:     ManifestVersion,
			Status:      StatusError,
			OwnerCallID: output.OwnerCallID,
			Reason:      "parent evidence metadata exceeds output budget; narrow the manifest and retry",
			Parts: []Part{{
				Kind:   "manifest",
				Status: PartError,
				Reason: "evidence metadata cannot fit within the serialized output budget",
			}},
		}
		if outputSize(*output) > MaxOutputBytes {
			output.OwnerCallID = ""
		}
		return false
	}
	output.Status = aggregateStatus(output.Parts)
	return true
}

func outputSize(output Output) int {
	size, err := WriteMeasuredJSON(io.Discard, output)
	if err != nil {
		return MaxOutputBytes + 1
	}
	return size
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
	part.ClaimRefinement = false
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
