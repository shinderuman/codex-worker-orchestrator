package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ParentReviewBinding struct {
	ID           string                     `json:"id"`
	PacketSHA256 string                     `json:"packet_sha256"`
	Targets      []string                   `json:"targets"`
	SolQuestion  string                     `json:"sol_question"`
	Snapshot     SnapshotDigest             `json:"snapshot"`
	Proof        *ParentReviewEvidenceProof `json:"proof,omitempty"`
}

type ParentReviewEvidenceProof struct {
	ReviewID    string                      `json:"review_id"`
	OwnerCallID string                      `json:"owner_call_id"`
	Snapshot    SnapshotDigest              `json:"snapshot"`
	Claims      []ParentReviewEvidenceClaim `json:"claims"`
}

type ParentReviewEvidenceClaim struct {
	Kind    string `json:"kind"`
	Digest  string `json:"digest"`
	Locator string `json:"locator"`
}

func newParentReviewBinding(value packet.Result, snapshot SnapshotDigest) (*ParentReviewBinding, error) {
	if value.Status != packet.StatusNeedsSolReview {
		return nil, fmt.Errorf("review evidence binding requires %s, got %s", packet.StatusNeedsSolReview, value.Status)
	}
	if !completeParentReviewSnapshot(snapshot) {
		return nil, fmt.Errorf("review evidence binding requires a complete Git snapshot")
	}
	if len(value.Targets) == 0 || strings.TrimSpace(value.SolQuestion) == "" {
		return nil, fmt.Errorf("review evidence binding requires targets and sol_question")
	}
	machine, err := value.MachineJSON()
	if err != nil {
		return nil, err
	}
	id, err := NewUUID()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(machine)
	return &ParentReviewBinding{
		ID:           id,
		PacketSHA256: hex.EncodeToString(sum[:]),
		Targets:      append([]string(nil), value.Targets...),
		SolQuestion:  value.SolQuestion,
		Snapshot:     snapshot,
	}, nil
}

func validateParentReviewBindingState(state ParentReviewState) error {
	if state.Review == nil {
		return nil
	}
	if err := validateParentReviewBindingIdentity(state); err != nil {
		return err
	}
	return validateParentReviewEvidenceProof(state.Review)
}

func validateParentReviewBindingIdentity(state ParentReviewState) error {
	if state.Open == nil || state.Open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return fmt.Errorf("parent review evidence binding exists without an open %s review", packet.StatusNeedsSolReview)
	}
	binding := state.Review
	if binding.ID == "" || !ValidUUIDFormat(binding.ID) || len(binding.PacketSHA256) != sha256.Size*2 {
		return fmt.Errorf("parent review evidence binding schema is invalid")
	}
	if len(binding.Targets) == 0 || strings.TrimSpace(binding.SolQuestion) == "" || !completeParentReviewSnapshot(binding.Snapshot) {
		return fmt.Errorf("parent review evidence binding schema is invalid")
	}
	if _, err := hex.DecodeString(binding.PacketSHA256); err != nil {
		return fmt.Errorf("parent review packet digest is invalid")
	}
	return nil
}

func validateParentReviewEvidenceProof(binding *ParentReviewBinding) error {
	if binding.Proof == nil {
		return nil
	}
	proof := binding.Proof
	if proof.ReviewID != binding.ID || proof.OwnerCallID == "" || !sameParentReviewSnapshot(proof.Snapshot, binding.Snapshot) || len(proof.Claims) == 0 {
		return fmt.Errorf("parent review evidence proof schema is invalid")
	}
	for _, claim := range proof.Claims {
		if (claim.Kind != "diff" && claim.Kind != "source") || claim.Digest == "" || claim.Locator == "" {
			return fmt.Errorf("parent review evidence claim schema is invalid")
		}
	}
	return nil
}

func (s *StateStore) openBoundParentReviewState(value packet.Result, producer ParentReviewProducer, snapshot SnapshotDigest) error {
	binding, err := newParentReviewBinding(value, snapshot)
	if err != nil {
		return err
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return err
	}
	state.Open = &ParentReviewOpenState{
		PacketStatus: string(value.Status),
		Role:         producer.Role,
		ModelAlias:   producer.Model,
		Risk:         string(value.Risk),
	}
	state.Review = binding
	return s.writeParentReviewState(state)
}

func (s *StateStore) CurrentParentReviewBinding() (*ParentReviewBinding, error) {
	open, err := s.CurrentParentReview()
	if err != nil {
		return nil, err
	}
	if open == nil || open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return nil, nil
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return nil, err
	}
	if state.Review == nil {
		return nil, nil
	}
	binding := *state.Review
	binding.Targets = append([]string(nil), binding.Targets...)
	if binding.Proof != nil {
		proof := *binding.Proof
		proof.Claims = append([]ParentReviewEvidenceClaim(nil), proof.Claims...)
		binding.Proof = &proof
	}
	return &binding, nil
}

func (s *StateStore) MarkParentReviewEvidence(reviewID, ownerCallID string, claims []ParentReviewEvidenceClaim) error {
	if reviewID == "" || ownerCallID == "" || len(claims) == 0 {
		return fmt.Errorf("parent review evidence proof is incomplete")
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return err
	}
	if state.Open == nil || state.Open.PacketStatus != string(packet.StatusNeedsSolReview) || state.Review == nil || state.Review.ID != reviewID {
		return fmt.Errorf("parent review evidence no longer matches the open review")
	}
	current, err := s.captureCurrentParentReviewSnapshot()
	if err != nil {
		return err
	}
	if !sameParentReviewSnapshot(current, state.Review.Snapshot) {
		return fmt.Errorf("parent review evidence snapshot changed; request fresh review evidence")
	}
	state.Review.Proof = &ParentReviewEvidenceProof{
		ReviewID:    reviewID,
		OwnerCallID: ownerCallID,
		Snapshot:    current,
		Claims:      append([]ParentReviewEvidenceClaim(nil), claims...),
	}
	return s.writeParentReviewState(state)
}

func (s *StateStore) ParentReviewAcceptReady() (bool, error) {
	open, err := s.CurrentParentReview()
	if err != nil {
		return false, err
	}
	if open == nil || open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return true, nil
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return false, err
	}
	if state.Review == nil || state.Review.Proof == nil || state.Review.Proof.ReviewID != state.Review.ID {
		return false, nil
	}
	current, err := s.captureCurrentParentReviewSnapshot()
	if err != nil {
		return false, err
	}
	return sameParentReviewSnapshot(current, state.Review.Snapshot) && sameParentReviewSnapshot(current, state.Review.Proof.Snapshot), nil
}

func (s *StateStore) RequireParentReviewAcceptanceEvidence() error {
	ready, err := s.ParentReviewAcceptReady()
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("%s accept requires matching current target evidence for the open review", packet.StatusNeedsSolReview)
	}
	return nil
}

func (s *StateStore) captureCurrentParentReviewSnapshot() (SnapshotDigest, error) {
	repoRoot := s.ReadOr("repo-root", "")
	if repoRoot == "" {
		return SnapshotDigest{}, fmt.Errorf("parent review evidence requires repository root")
	}
	snapshot, err := CaptureGitSnapshot(repoRoot)
	if err != nil {
		return SnapshotDigest{}, fmt.Errorf("parent review evidence cannot capture current Git snapshot: %w", err)
	}
	return SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}, nil
}

func completeParentReviewSnapshot(snapshot SnapshotDigest) bool {
	return snapshot.Head != "" && snapshot.IndexDigest != "" && snapshot.WorktreeDigest != ""
}

func sameParentReviewSnapshot(left, right SnapshotDigest) bool {
	return left.Head == right.Head && left.IndexDigest == right.IndexDigest && left.WorktreeDigest == right.WorktreeDigest
}
