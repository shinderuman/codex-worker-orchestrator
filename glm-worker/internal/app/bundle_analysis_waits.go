package app

import (
	"sort"
	"time"
)

func analysisWaitCalls(association codexAssociation, scan bundleRolloutScan, scanErr error, start time.Time, execution analysisExecutionBoundary, collectionEnd time.Time) bundleAnalysisWaitCalls {
	waits := bundleAnalysisWaitCalls{Status: analysisStatusCounted}
	if association.ParentStatus != codexStatusIncluded {
		waits.Status = association.ParentStatus
		return waits
	}
	if scanErr != nil {
		waits.Status = analysisStatusUnreadable
		return waits
	}
	if execution.status == analysisStatusUnknown {
		waits.Status = analysisStatusUnknown
		return waits
	}
	if !scan.hasWindow {
		waits.Status = analysisStatusNoObservation
		return waits
	}
	endBound := collectionEnd
	if execution.status == analysisStatusAvailable {
		endBound = execution.end
	}
	waits.Calls, waits.DuplicateCallIDs = analysisWaitCallEntries(scan, start, endBound)
	waits.Count = len(waits.Calls)
	if execution.status == analysisStatusOpen {
		waits.Status = analysisStatusOpen
	}
	return waits
}

func analysisWaitCallEntries(scan bundleRolloutScan, start, end time.Time) ([]bundleAnalysisWaitCall, []bundleAnalysisWaitDuplicate) {
	returnLines := analysisWaitReturnLines(scan.waitReturns)
	requestsByCall, order, anonymous := analysisGroupWaitRequests(scan.waits, start, end)
	calls := make([]bundleAnalysisWaitCall, 0, len(order)+len(anonymous))
	duplicates := make([]bundleAnalysisWaitDuplicate, 0)
	for _, callID := range order {
		entry, conflict := analysisWaitCallRecord(callID, requestsByCall[callID])
		if conflict || len(returnLines[callID]) > 1 {
			duplicates = append(duplicates, bundleAnalysisWaitDuplicate{
				CallID:       callID,
				RequestLines: entry.RequestLines,
				ReturnLines:  returnLines[callID],
			})
			continue
		}
		entry.ReturnLines = returnLines[callID]
		calls = append(calls, entry)
	}
	for _, request := range anonymous {
		calls = append(calls, bundleAnalysisWaitCall{
			RequestedYieldMS: request.YieldMS,
			YieldClass:       analysisWaitYieldClass(request.YieldMS),
			RequestLines:     []int{request.Line},
		})
	}
	sort.Slice(calls, func(i, j int) bool {
		return calls[i].RequestLines[0] < calls[j].RequestLines[0]
	})
	return calls, duplicates
}

func analysisWaitReturnLines(waitReturns []analysisRolloutWaitReturn) map[string][]int {
	returnLines := map[string][]int{}
	for _, waitReturn := range waitReturns {
		returnLines[waitReturn.CallID] = append(returnLines[waitReturn.CallID], waitReturn.Line)
	}
	return returnLines
}

func analysisGroupWaitRequests(requests []analysisRolloutWaitRequest, start, end time.Time) (map[string][]analysisRolloutWaitRequest, []string, []analysisRolloutWaitRequest) {
	requestsByCall := map[string][]analysisRolloutWaitRequest{}
	order := make([]string, 0)
	anonymous := make([]analysisRolloutWaitRequest, 0)
	for _, request := range requests {
		if request.At.Before(start) || request.At.After(end) {
			continue
		}
		if request.CallID == "" {
			anonymous = append(anonymous, request)
			continue
		}
		if _, known := requestsByCall[request.CallID]; !known {
			order = append(order, request.CallID)
		}
		requestsByCall[request.CallID] = append(requestsByCall[request.CallID], request)
	}
	return requestsByCall, order, anonymous
}

func analysisWaitCallRecord(callID string, requests []analysisRolloutWaitRequest) (bundleAnalysisWaitCall, bool) {
	entry := bundleAnalysisWaitCall{
		CallID:           callID,
		RequestedYieldMS: requests[0].YieldMS,
		YieldClass:       analysisWaitYieldClass(requests[0].YieldMS),
		RequestLines:     []int{requests[0].Line},
	}
	conflict := false
	for _, request := range requests[1:] {
		entry.RequestLines = append(entry.RequestLines, request.Line)
		if !analysisWaitYieldEqual(requests[0].YieldMS, request.YieldMS) {
			conflict = true
		}
	}
	return entry, conflict
}

func analysisWaitYieldClass(yieldMS *float64) string {
	switch {
	case yieldMS == nil:
		return analysisStatusUnknown
	case *yieldMS < analysisWaitShortBoundMS:
		return analysisWaitYieldClassShort
	case *yieldMS < analysisWaitLongBoundMS:
		return analysisWaitYieldClassBounded
	default:
		return analysisWaitYieldClassLong
	}
}

func analysisWaitYieldEqual(left, right *float64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
