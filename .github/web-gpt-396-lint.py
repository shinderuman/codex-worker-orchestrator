from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one match, got {count}")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/app/parent_evidence.go",
    'file.Status == "unknown"',
    'file.Status == analysisStatusUnknown',
)
replace_once(
    "glm-worker/internal/app/parent_evidence.go",
    '''func parentReviewNumericRange(locator string) (int, int, bool) {
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
}''',
    '''func parentReviewNumericRange(locator string) (int, int, bool) {
	token := parentReviewNumericRangeToken(locator)
	if token == "" {
		return 0, 0, false
	}
	values := strings.Split(token, "-")
	if len(values) > 2 || values[0] == "" {
		return 0, 0, false
	}
	start, ok := parentReviewPositiveLine(values[0])
	if !ok {
		return 0, 0, false
	}
	if len(values) == 1 {
		return start, start, true
	}
	end, ok := parentReviewPositiveLine(values[1])
	if !ok || end < start {
		return 0, 0, false
	}
	return start, end, true
}

func parentReviewNumericRangeToken(locator string) string {
	end := 0
	for end < len(locator) {
		c := locator[end]
		if (c < '0' || c > '9') && c != '-' {
			break
		}
		end++
	}
	return locator[:end]
}

func parentReviewPositiveLine(value string) (int, bool) {
	line, err := strconv.Atoi(value)
	return line, err == nil && line > 0
}''',
)

replace_once(
    "glm-worker/internal/state/parent_review_evidence.go",
    '''func validateParentReviewBindingState(state ParentReviewState) error {
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
}''',
    '''func validateParentReviewBindingState(state ParentReviewState) error {
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
}''',
)
