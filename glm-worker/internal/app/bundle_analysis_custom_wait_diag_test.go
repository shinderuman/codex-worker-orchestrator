package app

import "testing"

func TestAnalysisCustomWaitDirectHelpers(t *testing.T) {
	yield, ok := analysisCanonicalCustomWriteStdinWait(analysisObservedDirectWaitSource(46866, 300000, 20000))
	if !ok || yield == nil || *yield != 300000 {
		t.Fatalf("custom helper ok=%v yield=%v source=%q", ok, yield, analysisObservedDirectWaitSource(46866, 300000, 20000))
	}
	legacy := analysisWaitRequestedYield(analysisLegacyWaitArguments(300000))
	if legacy == nil || *legacy != 300000 {
		t.Fatalf("legacy helper yield=%v arguments=%q", legacy, analysisLegacyWaitArguments(300000))
	}
}
