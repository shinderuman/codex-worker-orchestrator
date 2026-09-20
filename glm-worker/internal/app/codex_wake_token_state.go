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

type transactionTokenLease struct {
	activePath   string
	leasePath    string
	deliveryPath string
	lock         *repoLockLease
}

const codexWakeTokenStateDir = "glm-worker-wake-transactions"

var removeCodexWakeLeaseFile = os.Remove

func persistCodexWakeToken(codexConfigDir, token string) error {
	return persistTransactionToken(codexConfigDir, codexWakeTokenStateDir, "wake", token)
}

func persistTransactionToken(codexConfigDir, stateDir, label, token string) error {
	if token == "" {
		return fmt.Errorf("%s transaction token is empty", label)
	}
	dir := filepath.Join(codexConfigDir, stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s transaction state: %w", label, err)
	}
	path := transactionTokenPath(dir, token)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("persist %s transaction token: %w", label, err)
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("persist %s transaction token: %w", label, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("persist %s transaction token: %w", label, err)
	}
	return nil
}

func beginCodexWakeToken(codexConfigDir, token string) (transactionTokenLease, error) {
	return beginTransactionToken(codexConfigDir, codexWakeTokenStateDir, "wake", token)
}

func beginTransactionToken(codexConfigDir, stateDir, label, token string) (transactionTokenLease, error) {
	dir := filepath.Join(codexConfigDir, stateDir)
	active := transactionTokenPath(dir, token)
	lock, err := AcquireRepoLockLease(active + ".lock")
	if err != nil {
		return transactionTokenLease{}, fmt.Errorf("claim %s transaction token lock: %w", label, err)
	}
	lease := transactionTokenLease{
		activePath:   active,
		leasePath:    active + ".inflight",
		deliveryPath: active + ".delivering",
		lock:         lock,
	}
	claimPath, err := claimableTransactionTokenPath(lease, token)
	if err != nil {
		lease.releaseLock()
		return transactionTokenLease{}, err
	}
	if claimPath == lease.leasePath {
		return lease, nil
	}
	if err := os.Rename(lease.activePath, lease.leasePath); err != nil {
		lease.releaseLock()
		return transactionTokenLease{}, fmt.Errorf("claim %s transaction token: %w", label, err)
	}
	return lease, nil
}

func claimableTransactionTokenPath(lease transactionTokenLease, token string) (string, error) {
	for _, path := range []string{lease.activePath, lease.leasePath} {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := validateTransactionTokenState(data, token); err != nil {
				return "", err
			}
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("transaction token is not active: %w", err)
		}
	}
	if data, err := os.ReadFile(lease.deliveryPath); err == nil {
		if err := validateTransactionTokenState(data, token); err != nil {
			return "", err
		}
		return "", fmt.Errorf("transaction token response delivery is unresolved")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("transaction token is not active: %w", err)
	}
	return "", fmt.Errorf("transaction token is not active: %w", os.ErrNotExist)
}

func validateTransactionTokenState(data []byte, token string) error {
	stored := trimSingleTrailingNewline(data)
	if subtle.ConstantTimeCompare(stored, []byte(token)) != 1 {
		return fmt.Errorf("transaction token does not match trusted state")
	}
	return nil
}

func (lease transactionTokenLease) markDelivering() error {
	if err := os.Rename(lease.leasePath, lease.deliveryPath); err != nil {
		return fmt.Errorf("mark transaction token delivering: %w", err)
	}
	return nil
}

func (lease transactionTokenLease) commit() error {
	defer lease.releaseLock()
	if err := removeCodexWakeLeaseFile(lease.deliveryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("finalize transaction token: %w", err)
	}
	return nil
}

func (lease transactionTokenLease) rollback() {
	_ = os.Rename(lease.leasePath, lease.activePath)
	lease.releaseLock()
}

func (lease transactionTokenLease) rollbackDelivering() {
	_ = os.Rename(lease.deliveryPath, lease.activePath)
	lease.releaseLock()
}

func (lease transactionTokenLease) releaseLock() {
	if lease.lock != nil {
		lease.lock.Release()
	}
}

func removeCodexWakeToken(codexConfigDir, token string) {
	removeTransactionToken(codexConfigDir, codexWakeTokenStateDir, token)
}

func removeTransactionToken(codexConfigDir, stateDir, token string) {
	if token == "" {
		return
	}
	dir := filepath.Join(codexConfigDir, stateDir)
	_ = os.Remove(transactionTokenPath(dir, token))
}

func transactionTokenPath(dir, token string) string {
	sum := sha256.Sum256([]byte(token))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".token")
}

func trimSingleTrailingNewline(value []byte) []byte {
	if len(value) != 0 && value[len(value)-1] == '\n' {
		return value[:len(value)-1]
	}
	return value
}
