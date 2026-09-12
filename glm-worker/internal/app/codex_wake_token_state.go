package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type codexWakeTokenLease struct {
	activePath   string
	leasePath    string
	deliveryPath string
	lock         *repoLockLease
}

const codexWakeTokenStateDir = "glm-worker-wake-transactions"

var removeCodexWakeLeaseFile = os.Remove

func persistCodexWakeToken(codexConfigDir, token string) error {
	if token == "" {
		return fmt.Errorf("wake transaction token is empty")
	}
	dir := filepath.Join(codexConfigDir, codexWakeTokenStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create wake transaction state: %w", err)
	}
	path := codexWakeTokenPath(dir, token)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("persist wake transaction token: %w", err)
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("persist wake transaction token: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("persist wake transaction token: %w", err)
	}
	return nil
}

func beginCodexWakeToken(codexConfigDir, token string) (codexWakeTokenLease, error) {
	dir := filepath.Join(codexConfigDir, codexWakeTokenStateDir)
	active := codexWakeTokenPath(dir, token)
	lock, err := AcquireRepoLockLease(active + ".lock")
	if err != nil {
		return codexWakeTokenLease{}, fmt.Errorf("claim wake transaction token lock: %w", err)
	}
	lease := codexWakeTokenLease{
		activePath:   active,
		leasePath:    active + ".inflight",
		deliveryPath: active + ".delivering",
		lock:         lock,
	}
	claimPath, err := claimableCodexWakeTokenPath(lease, token)
	if err != nil {
		lease.releaseLock()
		return codexWakeTokenLease{}, err
	}
	if claimPath == lease.leasePath {
		return lease, nil
	}
	if err := os.Rename(lease.activePath, lease.leasePath); err != nil {
		lease.releaseLock()
		return codexWakeTokenLease{}, fmt.Errorf("claim wake transaction token: %w", err)
	}
	return lease, nil
}

func claimableCodexWakeTokenPath(lease codexWakeTokenLease, token string) (string, error) {
	for _, path := range []string{lease.activePath, lease.leasePath} {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := validateCodexWakeTokenState(data, token); err != nil {
				return "", err
			}
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("wake transaction token is not active: %w", err)
		}
	}
	if data, err := os.ReadFile(lease.deliveryPath); err == nil {
		if err := validateCodexWakeTokenState(data, token); err != nil {
			return "", err
		}
		return "", fmt.Errorf("wake transaction token response delivery is unresolved")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("wake transaction token is not active: %w", err)
	}
	return "", fmt.Errorf("wake transaction token is not active: %w", os.ErrNotExist)
}

func validateCodexWakeTokenState(data []byte, token string) error {
	stored := trimSingleTrailingNewline(data)
	if subtle.ConstantTimeCompare(stored, []byte(token)) != 1 {
		return fmt.Errorf("wake transaction token does not match trusted state")
	}
	return nil
}

func (lease codexWakeTokenLease) markDelivering() error {
	if err := os.Rename(lease.leasePath, lease.deliveryPath); err != nil {
		return fmt.Errorf("mark wake transaction token delivering: %w", err)
	}
	return nil
}

func (lease codexWakeTokenLease) commit() error {
	defer lease.releaseLock()
	if err := removeCodexWakeLeaseFile(lease.deliveryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("finalize wake transaction token: %w", err)
	}
	return nil
}

func (lease codexWakeTokenLease) rollback() {
	_ = os.Rename(lease.leasePath, lease.activePath)
	lease.releaseLock()
}

func (lease codexWakeTokenLease) rollbackDelivering() {
	_ = os.Rename(lease.deliveryPath, lease.activePath)
	lease.releaseLock()
}

func (lease codexWakeTokenLease) releaseLock() {
	if lease.lock != nil {
		lease.lock.Release()
	}
}

func removeCodexWakeToken(codexConfigDir, token string) {
	if token == "" {
		return
	}
	dir := filepath.Join(codexConfigDir, codexWakeTokenStateDir)
	_ = os.Remove(codexWakeTokenPath(dir, token))
}

func codexWakeTokenPath(dir, token string) string {
	sum := sha256.Sum256([]byte(token))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".token")
}

func trimSingleTrailingNewline(value []byte) []byte {
	if len(value) != 0 && value[len(value)-1] == '\n' {
		return value[:len(value)-1]
	}
	return value
}
