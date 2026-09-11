from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one match, got {count}")
    p.write_text(text.replace(old, new, 1))


state_path = Path("glm-worker/internal/state/parent_review_evidence.go")
if state_path.exists():
    raise SystemExit("parent_review_evidence.go already exists")
state_path.write_text(r'''package state

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
	if state.Open == nil || state.Open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return fmt.Errorf("parent review evidence binding exists without an open %s review", packet.StatusNeedsSolReview)
	}
	binding := state.Review
	if binding.ID == "" || !ValidUUIDFormat(binding.ID) || len(binding.PacketSHA256) != sha256.Size*2 || len(binding.Targets) == 0 || strings.TrimSpace(binding.SolQuestion) == "" || !completeParentReviewSnapshot(binding.Snapshot) {
		return fmt.Errorf("parent review evidence binding schema is invalid")
	}
	if _, err := hex.DecodeString(binding.PacketSHA256); err != nil {
		return fmt.Errorf("parent review packet digest is invalid")
	}
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
	state, err := s.loadParentReviewState()
	if err != nil {
		return false, err
	}
	if state.Open == nil || state.Open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return true, nil
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
''')

replace_once(
    "glm-worker/internal/state/parent_review_state.go",
    '''type ParentReviewState struct {
	Version int                    `json:"version"`
	TaskID  string                 `json:"task_id"`
	Open    *ParentReviewOpenState `json:"open,omitempty"`
}''',
    '''type ParentReviewState struct {
	Version int                    `json:"version"`
	TaskID  string                 `json:"task_id"`
	Open    *ParentReviewOpenState `json:"open,omitempty"`
	Review  *ParentReviewBinding   `json:"review,omitempty"`
}''',
)
replace_once(
    "glm-worker/internal/state/parent_review_state.go",
    '''	if state.Open != nil && !validParentReviewPacketStatus(state.Open.PacketStatus) {
		return ParentReviewState{}, fmt.Errorf("parent review stateのpacket statusが不正です: %s", state.Open.PacketStatus)
	}
	return state, nil''',
    '''	if state.Open != nil && !validParentReviewPacketStatus(state.Open.PacketStatus) {
		return ParentReviewState{}, fmt.Errorf("parent review stateのpacket statusが不正です: %s", state.Open.PacketStatus)
	}
	if err := validateParentReviewBindingState(state); err != nil {
		return ParentReviewState{}, err
	}
	return state, nil''',
)
replace_once(
    "glm-worker/internal/state/parent_review_state.go",
    '''	state.Open = &ParentReviewOpenState{
		PacketStatus: status,
		Role:         producer.Role,
		ModelAlias:   producer.Model,
		Risk:         risk,
	}
	return s.writeParentReviewState(state)''',
    '''	state.Open = &ParentReviewOpenState{
		PacketStatus: status,
		Role:         producer.Role,
		ModelAlias:   producer.Model,
		Risk:         risk,
	}
	state.Review = nil
	return s.writeParentReviewState(state)''',
)
replace_once(
    "glm-worker/internal/state/parent_review_state.go",
    '''	resolved := *state.Open
	state.Open = nil
	if err := s.writeParentReviewState(state); err != nil {''',
    '''	resolved := *state.Open
	state.Open = nil
	state.Review = nil
	if err := s.writeParentReviewState(state); err != nil {''',
)

replace_once(
    "glm-worker/internal/state/stats.go",
    '''func (s *StateStore) RecordSolResult(value packet.Result, producer ParentReviewProducer) error {
	if err := s.openParentReviewState(string(value.Status), string(value.Risk), producer); err != nil {
		return err
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.SolPacketBytes += value.ByteSize()
		switch value.Status {
		case packet.StatusNeedsSolDecision:
			stats.NeedsSolDecisionPackets++
		case packet.StatusNeedsSolReview:
			stats.NeedsSolReviewPackets++
		case packet.StatusPass:
			stats.PassPackets++
		}
		stats.openParentReview(string(value.Status), string(value.Risk), producer)
	})
	return nil
}''',
    '''func (s *StateStore) RecordSolResult(value packet.Result, producer ParentReviewProducer) error {
	return s.recordSolResult(value, producer, nil)
}

func (s *StateStore) RecordSolResultWithReviewSnapshot(value packet.Result, producer ParentReviewProducer, snapshot SnapshotDigest) error {
	return s.recordSolResult(value, producer, &snapshot)
}

func (s *StateStore) recordSolResult(value packet.Result, producer ParentReviewProducer, reviewSnapshot *SnapshotDigest) error {
	var err error
	if value.Status == packet.StatusNeedsSolReview && reviewSnapshot != nil {
		err = s.openBoundParentReviewState(value, producer, *reviewSnapshot)
	} else {
		err = s.openParentReviewState(string(value.Status), string(value.Risk), producer)
	}
	if err != nil {
		return err
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.SolPacketBytes += value.ByteSize()
		switch value.Status {
		case packet.StatusNeedsSolDecision:
			stats.NeedsSolDecisionPackets++
		case packet.StatusNeedsSolReview:
			stats.NeedsSolReviewPackets++
		case packet.StatusPass:
			stats.PassPackets++
		}
		stats.openParentReview(string(value.Status), string(value.Risk), producer)
	})
	return nil
}''',
)

replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '''func (w *Workflow) emitResult(value packet.Result) error {
	report, err := machineReport(value)
	if err != nil {
		return err
	}
	if err := w.state.RecordSolResult(value, w.lastProducer); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w.output, report)
	return err
}''',
    '''func (w *Workflow) emitResult(value packet.Result) error {
	report, err := machineReport(value)
	if err != nil {
		return err
	}
	if value.Status == packet.StatusNeedsSolReview {
		snapshot, captureErr := w.captureSnapshot(w.config.RepoRoot)
		if captureErr != nil {
			return fmt.Errorf("parent review snapshotを取得できません: %w", captureErr)
		}
		digest := state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
		if err := w.state.RecordSolResultWithReviewSnapshot(value, w.lastProducer, digest); err != nil {
			return err
		}
	} else if err := w.state.RecordSolResult(value, w.lastProducer); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w.output, report)
	return err
}''',
)

replace_once(
    "glm-worker/internal/state/parent_accept.go",
    '''func (s *StateStore) AcceptParentReview() (bool, error) {
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)''',
    '''func (s *StateStore) AcceptParentReview() (bool, error) {
	if err := s.RequireParentReviewAcceptanceEvidence(); err != nil {
		return false, err
	}
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)''',
)

replace_once(
    "glm-worker/internal/app/parent_evidence.go",
    '''	written, err := writeMeasuredJSON(stdout, output)
	if err != nil {
		return err
	}
	saveSurvivingParentEvidenceClaims(p, output.Parts)''',
    '''	written, err := writeMeasuredJSON(stdout, output)
	if err != nil {
		return err
	}
	if err := markParentReviewEvidenceProof(p, output.Parts); err != nil {
		return err
	}
	saveSurvivingParentEvidenceClaims(p, output.Parts)''',
)
marker = '''func applyParentEvidenceTotalBudget(output *parentEvidenceOutput) {'''
helper = r'''func markParentReviewEvidenceProof(p *parentEvidenceProjector, parts []parentEvidencePart) error {
	binding, err := p.st.CurrentParentReviewBinding()
	if err != nil {
		return err
	}
	if binding == nil {
		return nil
	}
	claims, complete := parentReviewEvidenceClaims(binding.Targets, parts)
	if !complete {
		return nil
	}
	return p.st.MarkParentReviewEvidence(binding.ID, p.ownerCallID, claims)
}

func parentReviewEvidenceClaims(targets []string, parts []parentEvidencePart) ([]state.ParentReviewEvidenceClaim, bool) {
	claims := make([]state.ParentReviewEvidenceClaim, 0, len(targets))
	seen := make(map[string]struct{})
	for _, target := range targets {
		claim, ok := parentReviewEvidenceClaimForTarget(target, parts)
		if !ok {
			return nil, false
		}
		key := claim.Kind + "\x00" + claim.Digest + "\x00" + claim.Locator
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		claims = append(claims, claim)
	}
	return claims, len(claims) > 0
}

func parentReviewEvidenceClaimForTarget(target string, parts []parentEvidencePart) (state.ParentReviewEvidenceClaim, bool) {
	for _, part := range parts {
		if part.Digest == "" {
			continue
		}
		if part.Source != nil && part.Source.Content != "" && parentReviewSourceCoversTarget(target, *part.Source) {
			return state.ParentReviewEvidenceClaim{Kind: "source", Digest: part.Digest, Locator: part.Locator}, true
		}
		if part.Diff != nil && part.Diff.Body != "" && parentReviewDiffCoversTarget(target, *part.Diff) {
			return state.ParentReviewEvidenceClaim{Kind: "diff", Digest: part.Digest, Locator: part.Locator}, true
		}
	}
	return state.ParentReviewEvidenceClaim{}, false
}

func parentReviewSourceCoversTarget(target string, source parentEvidenceSourceBody) bool {
	if !parentReviewTargetMatchesPath(target, source.Path) {
		return false
	}
	suffix := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(target), source.Path))
	if !strings.HasPrefix(suffix, ":") {
		return false
	}
	locator := strings.TrimSpace(strings.TrimPrefix(suffix, ":"))
	start, end, ok := parentReviewNumericRange(locator)
	return ok && source.LineStart <= start && source.LineEnd >= end
}

func parentReviewDiffCoversTarget(target string, diff parentEvidenceDiffBody) bool {
	for _, file := range diff.Files {
		if !parentReviewTargetMatchesPath(target, file.Path) || file.Status == "unknown" {
			continue
		}
		if file.HeadBlob != "" || file.IndexBlob != "" || file.WorktreeSHA != "" {
			return true
		}
	}
	return false
}

func parentReviewTargetMatchesPath(target, path string) bool {
	target = strings.TrimSpace(target)
	if target == path {
		return true
	}
	for _, separator := range []string{":", " ", ","} {
		if strings.HasPrefix(target, path+separator) {
			return true
		}
	}
	return false
}

func parentReviewNumericRange(locator string) (int, int, bool) {
	endIndex := 0
	for endIndex < len(locator) {
		c := locator[endIndex]
		if (c < '0' || c > '9') && c != '-' {
			break
		}
		endIndex++
	}
	if endIndex == 0 {
		return 0, 0, false
	}
	token := locator[:endIndex]
	values := strings.Split(token, "-")
	if len(values) > 2 || values[0] == "" {
		return 0, 0, false
	}
	start, err := strconv.Atoi(values[0])
	if err != nil || start < 1 {
		return 0, 0, false
	}
	end := start
	if len(values) == 2 {
		if values[1] == "" {
			return 0, 0, false
		}
		end, err = strconv.Atoi(values[1])
		if err != nil || end < start {
			return 0, 0, false
		}
	}
	return start, end, true
}

'''
p = Path("glm-worker/internal/app/parent_evidence.go")
text = p.read_text()
if text.count(marker) != 1:
    raise SystemExit("parent_evidence.go: total budget marker mismatch")
p.write_text(text.replace(marker, helper + marker, 1))
