package parentaction

import (
	"bytes"
	"errors"
	"fmt"
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
		token, matched, err := liveTokenFromEntry(stageDir, entry)
		if err != nil {
			return nil, err
		}
		if matched {
			tokens = append(tokens, token)
		}
	}
	return tokens, nil
}

func liveTokenFromEntry(stageDir string, entry os.DirEntry) (string, bool, error) {
	name := entry.Name()
	action, token, matched := stagedTokenIdentity(name)
	if !matched {
		return "", false, nil
	}
	if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
		return "", false, fmt.Errorf("parent action staging record is not a regular file: %s", action)
	}
	raw, err := readRegularPayload(filepath.Join(stageDir, name), len(tokenHeader(token)))
	if err != nil {
		return "", false, fmt.Errorf("parent action staging record is unreadable: %s", action)
	}
	if !bytes.HasPrefix(raw, []byte(tokenHeader(token))) {
		return "", false, fmt.Errorf("parent action staging token binding is invalid: %s", action)
	}
	return token, true, nil
}

func stagedTokenIdentity(name string) (string, string, bool) {
	if !strings.HasSuffix(name, ".txt") {
		return "", "", false
	}
	base := strings.TrimSuffix(name, ".txt")
	for action := range payloadActions {
		prefix := string(action) + "-"
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		token := strings.TrimPrefix(base, prefix)
		if !validToken(token) {
			return "", "", false
		}
		return string(action), token, true
	}
	return "", "", false
}
