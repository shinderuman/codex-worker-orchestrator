package parentaction

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func LiveTokens(repoRoot string) ([]string, error) {
	stageDir := filepath.Join(repoRoot, StageDirName)
	if _, err := os.Lstat(stageDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if err := validateStageDir(stageDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return nil, err
	}
	tokens := make([]string, 0, len(entries))
	for _, entry := range entries {
		token, ok := liveTokenFromEntry(stageDir, entry)
		if !ok {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func liveTokenFromEntry(stageDir string, entry os.DirEntry) (string, bool) {
	if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
		return "", false
	}
	name := entry.Name()
	if !strings.HasSuffix(name, ".txt") {
		return "", false
	}
	base := strings.TrimSuffix(name, ".txt")
	for action := range payloadActions {
		prefix := string(action) + "-"
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		token := strings.TrimPrefix(base, prefix)
		if !validToken(token) {
			return "", false
		}
		raw, err := readRegularPayload(filepath.Join(stageDir, name), len(tokenHeader(token)))
		if err != nil || !bytes.HasPrefix(raw, []byte(tokenHeader(token))) {
			return "", false
		}
		return token, true
	}
	return "", false
}
