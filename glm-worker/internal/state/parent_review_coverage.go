package state

import "fmt"

type ParentReviewEvidenceCoverage struct {
	ReviewID string                            `json:"review_id"`
	Lease    int64                             `json:"lease"`
	Snapshot SnapshotDigest                    `json:"snapshot"`
	Claims   []ParentReviewTargetCoverageClaim `json:"claims"`
}

type ParentReviewTargetCoverageClaim struct {
	Target      string                    `json:"target"`
	Evidence    ParentReviewEvidenceClaim `json:"evidence"`
	OwnerCallID string                    `json:"owner_call_id"`
}

func validateParentReviewEvidenceCoverage(binding *ParentReviewBinding) error {
	coverage := binding.Coverage
	if coverage == nil {
		return nil
	}
	if coverage.ReviewID != binding.ID || coverage.Lease < 0 || !sameParentReviewSnapshot(coverage.Snapshot, binding.Snapshot) || len(coverage.Claims) == 0 {
		return fmt.Errorf("parent review evidence coverage schema is invalid")
	}
	allowed := parentReviewTargetSet(binding.Targets)
	seen := make(map[string]struct{}, len(coverage.Claims))
	for _, covered := range coverage.Claims {
		if _, ok := allowed[covered.Target]; !ok || covered.OwnerCallID == "" || !validParentReviewEvidenceClaim(covered.Evidence) {
			return fmt.Errorf("parent review evidence coverage claim schema is invalid")
		}
		if _, duplicate := seen[covered.Target]; duplicate {
			return fmt.Errorf("parent review evidence coverage has duplicate target %q", covered.Target)
		}
		seen[covered.Target] = struct{}{}
	}
	return nil
}

func validParentReviewEvidenceClaim(claim ParentReviewEvidenceClaim) bool {
	return (claim.Kind == "diff" || claim.Kind == "source") && claim.Digest != "" && claim.Locator != ""
}

func parentReviewTargetSet(targets []string) map[string]struct{} {
	set := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		set[target] = struct{}{}
	}
	return set
}

func parentReviewCoverageComplete(binding *ParentReviewBinding) bool {
	if binding == nil || binding.Coverage == nil {
		return false
	}
	covered := make(map[string]struct{}, len(binding.Coverage.Claims))
	for _, claim := range binding.Coverage.Claims {
		covered[claim.Target] = struct{}{}
	}
	for target := range parentReviewTargetSet(binding.Targets) {
		if _, ok := covered[target]; !ok {
			return false
		}
	}
	return len(covered) > 0
}

func orderedCoverageClaims(targets []string, claims map[string]ParentReviewTargetCoverageClaim) []ParentReviewTargetCoverageClaim {
	ordered := make([]ParentReviewTargetCoverageClaim, 0, len(claims))
	seen := make(map[string]struct{}, len(claims))
	for _, target := range targets {
		if _, duplicate := seen[target]; duplicate {
			continue
		}
		claim, ok := claims[target]
		if !ok {
			continue
		}
		seen[target] = struct{}{}
		ordered = append(ordered, claim)
	}
	return ordered
}

func (s *StateStore) AccumulateParentReviewEvidenceCoverage(
	reviewID string,
	ownerCallID string,
	lease int64,
	claims []ParentReviewTargetCoverageClaim,
) (bool, error) {
	if reviewID == "" || ownerCallID == "" || lease < 0 {
		return false, fmt.Errorf("parent review evidence coverage identity is incomplete")
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return false, err
	}
	if state.Open == nil || state.Open.PacketStatus != "NEEDS_SOL_REVIEW" || state.Review == nil || state.Review.ID != reviewID {
		return false, fmt.Errorf("parent review evidence coverage no longer matches the open review")
	}
	currentLease, err := s.ParentEvidenceLeaseEpoch()
	if err != nil {
		return false, err
	}
	if currentLease != lease {
		return false, fmt.Errorf("parent review evidence lease changed; request fresh review evidence")
	}
	current, err := s.captureCurrentParentReviewSnapshot()
	if err != nil {
		return false, err
	}
	if !sameParentReviewSnapshot(current, state.Review.Snapshot) {
		return false, fmt.Errorf("parent review evidence snapshot changed; request fresh review evidence")
	}

	coverage := state.Review.Coverage
	if coverage == nil || coverage.ReviewID != reviewID || coverage.Lease != lease || !sameParentReviewSnapshot(coverage.Snapshot, current) {
		coverage = &ParentReviewEvidenceCoverage{ReviewID: reviewID, Lease: lease, Snapshot: current}
	}
	byTarget := make(map[string]ParentReviewTargetCoverageClaim, len(coverage.Claims)+len(claims))
	for _, covered := range coverage.Claims {
		byTarget[covered.Target] = covered
	}
	allowed := parentReviewTargetSet(state.Review.Targets)
	for _, claim := range claims {
		if _, ok := allowed[claim.Target]; !ok || !validParentReviewEvidenceClaim(claim.Evidence) {
			return false, fmt.Errorf("parent review evidence coverage claim is invalid for target %q", claim.Target)
		}
		claim.OwnerCallID = ownerCallID
		byTarget[claim.Target] = claim
	}
	coverage.Claims = orderedCoverageClaims(state.Review.Targets, byTarget)
	state.Review.Coverage = coverage

	complete := parentReviewCoverageComplete(state.Review)
	if complete {
		proofClaims := make([]ParentReviewEvidenceClaim, 0, len(coverage.Claims))
		for _, covered := range coverage.Claims {
			proofClaims = append(proofClaims, covered.Evidence)
		}
		state.Review.Proof = &ParentReviewEvidenceProof{
			ReviewID: reviewID, OwnerCallID: ownerCallID, Snapshot: current, Claims: proofClaims,
		}
	} else {
		state.Review.Proof = nil
	}
	if len(coverage.Claims) == 0 {
		state.Review.Coverage = nil
	}
	if err := s.writeParentReviewState(state); err != nil {
		return false, err
	}
	return complete, nil
}

func (s *StateStore) parentReviewCoverageLeaseCurrent(binding *ParentReviewBinding) (bool, error) {
	if binding == nil || binding.Coverage == nil {
		return true, nil
	}
	lease, err := s.ParentEvidenceLeaseEpoch()
	if err != nil {
		return false, err
	}
	return binding.Coverage.Lease == lease && parentReviewCoverageComplete(binding), nil
}
