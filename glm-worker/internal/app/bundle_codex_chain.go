package app

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

type codexRolloutCounterSignal struct {
	HasAnchor bool
	Total     *int64
	LastTotal *int64
}

func resolveCodexRolloutChain(matches []codexRollout) ([]codexRollout, string) {
	ordered := append([]codexRollout(nil), matches...)
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
	if reason := codexChainDuplicateContent(ordered); reason != "" {
		return nil, reason
	}
	if reason := codexChainIdentityMismatch(ordered); reason != "" {
		return nil, reason
	}
	if reason := codexChainOverlappingRanges(ordered); reason != "" {
		return nil, reason
	}
	if reason := codexChainCounterBoundaries(ordered); reason != "" {
		return nil, reason
	}
	return ordered, ""
}

func codexChainDuplicateContent(ordered []codexRollout) string {
	sizes, reason := codexChainRolloutSizes(ordered)
	if reason != "" {
		return reason
	}
	for i := range ordered {
		for j := i + 1; j < len(ordered); j++ {
			if reason := codexChainDuplicatePair(ordered[i], ordered[j], sizes[i], sizes[j]); reason != "" {
				return reason
			}
		}
	}
	return ""
}

func codexChainRolloutSizes(ordered []codexRollout) ([]int64, string) {
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

func codexChainDuplicatePair(left, right codexRollout, leftSize, rightSize int64) string {
	if leftSize != rightSize {
		return ""
	}
	equal, err := codexRolloutFilesIdentical(left.AbsolutePath, right.AbsolutePath)
	if err != nil {
		return "rollout chain candidate cannot be read for duplicate comparison: " + left.HomeRelative + ": " + err.Error()
	}
	if !equal {
		return ""
	}
	return "rollout chain candidates duplicate identical content: " + left.HomeRelative + ", " + right.HomeRelative
}

func codexRolloutFilesIdentical(left, right string) (bool, error) {
	leftSum, err := codexRolloutFileDigest(left)
	if err != nil {
		return false, err
	}
	rightSum, err := codexRolloutFileDigest(right)
	if err != nil {
		return false, err
	}
	return leftSum == rightSum, nil
}

func codexRolloutFileDigest(path string) ([sha256.Size]byte, error) {
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

func codexChainIdentityMismatch(ordered []codexRollout) string {
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

func codexChainOverlappingRanges(ordered []codexRollout) string {
	for i, member := range ordered {
		last, ok := codexRolloutLastTimestamp(member.AbsolutePath)
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

func codexChainCounterBoundaries(ordered []codexRollout) string {
	for _, member := range ordered {
		lastTotal, err := codexRolloutLastCounterTotal(member.AbsolutePath)
		if err != nil {
			return "rollout chain candidate cannot be read: " + member.HomeRelative + ": " + err.Error()
		}
		if lastTotal == nil {
			return "rollout chain candidate has no usable token counter anchor: " + member.HomeRelative
		}
	}
	for i := 1; i < len(ordered); i++ {
		signal, err := codexRolloutFirstCounterSignal(ordered[i].AbsolutePath)
		if err != nil {
			return "rollout chain candidate cannot be read: " + ordered[i].HomeRelative + ": " + err.Error()
		}
		if !signal.HasAnchor {
			return "rollout chain candidate has no usable token counter anchor: " + ordered[i].HomeRelative
		}
		if codexChainCounterRestarted(signal, nil) {
			continue
		}
		previousTotal, prevErr := codexRolloutLastCounterTotal(ordered[i-1].AbsolutePath)
		if prevErr != nil {
			return "rollout chain candidate cannot be read: " + ordered[i-1].HomeRelative + ": " + prevErr.Error()
		}
		if !codexChainCounterRestarted(signal, previousTotal) {
			return "rollout chain boundary does not restart a self-contained token counter: " + ordered[i].HomeRelative
		}
	}
	return ""
}

func codexChainCounterRestarted(signal codexRolloutCounterSignal, previousTotal *int64) bool {
	if signal.Total != nil && signal.LastTotal != nil && *signal.Total == *signal.LastTotal {
		return true
	}
	if signal.Total != nil && previousTotal != nil && *signal.Total < *previousTotal {
		return true
	}
	return false
}

func codexRolloutFirstCounterSignal(path string) (codexRolloutCounterSignal, error) {
	var first codexRolloutCounterSignal
	err := codexRolloutScanCounterSignals(path, func(signal codexRolloutCounterSignal) bool {
		first = signal
		return true
	})
	if err != nil {
		return codexRolloutCounterSignal{}, err
	}
	return first, nil
}

func codexRolloutLastCounterTotal(path string) (*int64, error) {
	var lastTotal *int64
	err := codexRolloutScanCounterSignals(path, func(signal codexRolloutCounterSignal) bool {
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

func codexRolloutScanCounterSignals(path string, observe func(codexRolloutCounterSignal) bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReaderSize(file, 64*1024)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			stop, observeErr := codexRolloutObserveCounterLine(line, observe)
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

func codexRolloutObserveCounterLine(line []byte, observe func(codexRolloutCounterSignal) bool) (bool, error) {
	signal, matched, parseErr := codexRolloutLineCounterSignal(line)
	if parseErr != nil {
		return false, parseErr
	}
	return matched && observe(signal), nil
}

func codexRolloutLineCounterSignal(line []byte) (codexRolloutCounterSignal, bool, error) {
	trimmed := string(line)
	if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if trimmed == "" {
		return codexRolloutCounterSignal{}, false, nil
	}
	var record codexRolloutScanLine
	if err := json.Unmarshal([]byte(trimmed), &record); err != nil {
		return codexRolloutCounterSignal{}, false, fmt.Errorf("rollout JSON行の解析に失敗しました: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, record.Timestamp); err != nil {
		return codexRolloutCounterSignal{}, false, fmt.Errorf("rollout timestampの解析に失敗しました: %w", err)
	}
	if record.Type != "event_msg" {
		return codexRolloutCounterSignal{}, false, nil
	}
	var payload codexRolloutEventPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return codexRolloutCounterSignal{}, false, nil
	}
	if payload.Type != codexRolloutTokenCountType || payload.Info == nil {
		return codexRolloutCounterSignal{}, false, nil
	}
	signal := codexRolloutCounterSignal{HasAnchor: true}
	if payload.Info.TotalTokenUsage != nil {
		total := payload.Info.TotalTokenUsage.TotalTokens
		signal.Total = total
	}
	if payload.Info.LastTokenUsage != nil {
		signal.LastTotal = payload.Info.LastTokenUsage.TotalTokens
	}
	return signal, true, nil
}
