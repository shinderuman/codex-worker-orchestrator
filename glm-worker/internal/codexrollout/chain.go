package codexrollout

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

type rolloutCounterSignal struct {
	HasAnchor bool
	Total     *int64
	LastTotal *int64
}

type rolloutScanLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type rolloutEventPayload struct {
	Type   string               `json:"type"`
	TurnID string               `json:"turn_id"`
	Info   *rolloutTokenPayload `json:"info"`
}

type rolloutTokenPayload struct {
	TotalTokenUsage *rolloutTokenUsage `json:"total_token_usage"`
	LastTokenUsage  *rolloutTokenUsage `json:"last_token_usage"`
}

type rolloutTokenUsage struct {
	TotalTokens *int64 `json:"total_tokens"`
}

const rolloutTokenCountType = "token_count"

func ResolveChain(matches []Rollout) ([]Rollout, string) {
	ordered := append([]Rollout(nil), matches...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].FirstTimestamp.Equal(ordered[j].FirstTimestamp) {
			return ordered[i].AbsolutePath < ordered[j].AbsolutePath
		}
		return ordered[i].FirstTimestamp.Before(ordered[j].FirstTimestamp)
	})
	for _, member := range ordered {
		if member.FirstTimestamp.IsZero() {
			return nil, "rollout chain candidate has an unreadable session metadata timestamp: " + member.HomeRelative
		}
	}
	if reason := duplicateContent(ordered); reason != "" {
		return nil, reason
	}
	if reason := identityMismatch(ordered); reason != "" {
		return nil, reason
	}
	if reason := overlappingRanges(ordered); reason != "" {
		return nil, reason
	}
	if reason := counterBoundaries(ordered); reason != "" {
		return nil, reason
	}
	return ordered, ""
}

func duplicateContent(ordered []Rollout) string {
	sizes, reason := rolloutSizes(ordered)
	if reason != "" {
		return reason
	}
	for i := range ordered {
		for j := i + 1; j < len(ordered); j++ {
			if reason := duplicatePair(ordered[i], ordered[j], sizes[i], sizes[j]); reason != "" {
				return reason
			}
		}
	}
	return ""
}

func rolloutSizes(ordered []Rollout) ([]int64, string) {
	sizes := make([]int64, len(ordered))
	for i, member := range ordered {
		info, err := os.Stat(member.AbsolutePath)
		if err != nil || !info.Mode().IsRegular() {
			return nil, "rollout chain candidate is not a readable regular file: " + member.HomeRelative
		}
		sizes[i] = info.Size()
	}
	return sizes, ""
}

func duplicatePair(left, right Rollout, leftSize, rightSize int64) string {
	if leftSize != rightSize {
		return ""
	}
	equal, err := filesIdentical(left.AbsolutePath, right.AbsolutePath)
	if err != nil {
		return "rollout chain candidate cannot be read for duplicate comparison: " + left.HomeRelative + ": " + err.Error()
	}
	if !equal {
		return ""
	}
	return "rollout chain candidates duplicate identical content: " + left.HomeRelative + ", " + right.HomeRelative
}

func filesIdentical(left, right string) (bool, error) {
	leftSum, err := fileDigest(left)
	if err != nil {
		return false, err
	}
	rightSum, err := fileDigest(right)
	if err != nil {
		return false, err
	}
	return leftSum == rightSum, nil
}

func fileDigest(path string) ([sha256.Size]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return [sha256.Size]byte{}, err
	}
	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return sum, nil
}

func identityMismatch(ordered []Rollout) string {
	for _, member := range ordered[1:] {
		if member.Cwd == ordered[0].Cwd && member.Originator == ordered[0].Originator && member.SourceRaw == ordered[0].SourceRaw {
			continue
		}
		field := "cwd, source, or originator"
		switch {
		case member.Cwd != ordered[0].Cwd:
			field = "cwd"
		case member.SourceRaw != ordered[0].SourceRaw:
			field = "source"
		case member.Originator != ordered[0].Originator:
			field = "originator"
		}
		return fmt.Sprintf("rollout chain candidates disagree on session identity (%s): %s, %s", field, ordered[0].HomeRelative, member.HomeRelative)
	}
	return ""
}

func overlappingRanges(ordered []Rollout) string {
	for i, member := range ordered {
		last, ok := LastTimestamp(member.AbsolutePath)
		if !ok || last.Before(member.FirstTimestamp) {
			return "rollout chain candidate has no readable event range: " + member.HomeRelative
		}
		ordered[i].LastTimestamp = last
	}
	for i := 1; i < len(ordered); i++ {
		if !ordered[i].FirstTimestamp.After(ordered[i-1].LastTimestamp) {
			return fmt.Sprintf("rollout chain candidates have overlapping event ranges: %s, %s", ordered[i-1].HomeRelative, ordered[i].HomeRelative)
		}
	}
	return ""
}

func counterBoundaries(ordered []Rollout) string {
	if reason := requireCounterAnchors(ordered); reason != "" {
		return reason
	}
	for i := 1; i < len(ordered); i++ {
		if reason := counterBoundaryReason(ordered[i-1], ordered[i]); reason != "" {
			return reason
		}
	}
	return ""
}

func requireCounterAnchors(ordered []Rollout) string {
	for _, member := range ordered {
		lastTotal, err := lastCounterTotal(member.AbsolutePath)
		if err != nil {
			return "rollout chain candidate cannot be read: " + member.HomeRelative + ": " + err.Error()
		}
		if lastTotal == nil {
			return "rollout chain candidate has no usable token counter anchor: " + member.HomeRelative
		}
	}
	return ""
}

func counterBoundaryReason(previous, current Rollout) string {
	signal, err := firstCounterSignal(current.AbsolutePath)
	if err != nil {
		return "rollout chain candidate cannot be read: " + current.HomeRelative + ": " + err.Error()
	}
	if !signal.HasAnchor {
		return "rollout chain candidate has no usable token counter anchor: " + current.HomeRelative
	}
	if counterRestarted(signal, nil) {
		return ""
	}
	previousTotal, err := lastCounterTotal(previous.AbsolutePath)
	if err != nil {
		return "rollout chain candidate cannot be read: " + previous.HomeRelative + ": " + err.Error()
	}
	if !counterRestarted(signal, previousTotal) {
		return "rollout chain boundary does not restart a self-contained token counter: " + current.HomeRelative
	}
	return ""
}

func counterRestarted(signal rolloutCounterSignal, previousTotal *int64) bool {
	if signal.Total != nil && signal.LastTotal != nil && *signal.Total == *signal.LastTotal {
		return true
	}
	if signal.Total != nil && previousTotal != nil && *signal.Total < *previousTotal {
		return true
	}
	return false
}

func firstCounterSignal(path string) (rolloutCounterSignal, error) {
	var first rolloutCounterSignal
	err := scanCounterSignals(path, func(signal rolloutCounterSignal) bool {
		first = signal
		return true
	})
	if err != nil {
		return rolloutCounterSignal{}, err
	}
	return first, nil
}

func lastCounterTotal(path string) (*int64, error) {
	var lastTotal *int64
	err := scanCounterSignals(path, func(signal rolloutCounterSignal) bool {
		if signal.Total != nil {
			total := *signal.Total
			lastTotal = &total
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	return lastTotal, nil
}

func scanCounterSignals(path string, observe func(rolloutCounterSignal) bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReaderSize(file, 64*1024)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			stop, observeErr := observeCounterLine(line, observe)
			if observeErr != nil {
				return observeErr
			}
			if stop {
				return nil
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func observeCounterLine(line []byte, observe func(rolloutCounterSignal) bool) (bool, error) {
	signal, matched, parseErr := lineCounterSignal(line)
	if parseErr != nil {
		return false, parseErr
	}
	return matched && observe(signal), nil
}

func lineCounterSignal(line []byte) (rolloutCounterSignal, bool, error) {
	trimmed := string(line)
	if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if trimmed == "" {
		return rolloutCounterSignal{}, false, nil
	}
	var record rolloutScanLine
	if err := json.Unmarshal([]byte(trimmed), &record); err != nil {
		return rolloutCounterSignal{}, false, fmt.Errorf("rollout JSON行の解析に失敗しました: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, record.Timestamp); err != nil {
		return rolloutCounterSignal{}, false, fmt.Errorf("rollout timestampの解析に失敗しました: %w", err)
	}
	if record.Type != "event_msg" {
		return rolloutCounterSignal{}, false, nil
	}
	var payload rolloutEventPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return rolloutCounterSignal{}, false, nil
	}
	if payload.Type != rolloutTokenCountType || payload.Info == nil {
		return rolloutCounterSignal{}, false, nil
	}
	signal := rolloutCounterSignal{HasAnchor: true}
	if payload.Info.TotalTokenUsage != nil {
		signal.Total = payload.Info.TotalTokenUsage.TotalTokens
	}
	if payload.Info.LastTokenUsage != nil {
		signal.LastTotal = payload.Info.LastTokenUsage.TotalTokens
	}
	return signal, true, nil
}
