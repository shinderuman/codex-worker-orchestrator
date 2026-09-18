package parentactioncmd

import "testing"

func TestPublicationReadinessRemainsReadyAfterRuntimeCandidatePromotion(t *testing.T) {
	cfg, st, candidate := preparePublicationRuntimeCandidate(t)
	writePublicationInstalledWorkerStub(t, candidate.CommitOID)

	install := installPublicationCandidate(cfg, st)
	if install.Status != publicationInstallStatusInstalled || install.Failure != nil {
		t.Fatalf("install = %#v", install)
	}
	promoted := promotePublicationCandidate(cfg, st)
	if promoted.Status != publicationPromotionStatusPromoted || promoted.Failure != nil {
		t.Fatalf("promotion = %#v", promoted)
	}

	readiness := projectPublicationReadiness(cfg, st)
	if readiness.Status != publicationReadinessReady || readiness.CandidateOID != candidate.CommitOID || readiness.Failure != nil {
		t.Fatalf("post-promotion readiness = %#v", readiness)
	}
	gate := publicationGateNamed(t, readiness.Gates, "runtime-install")
	if !gate.Required || gate.Status != publicationGatePass {
		t.Fatalf("post-promotion runtime install gate = %#v", gate)
	}
}
