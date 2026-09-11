package runner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSensitiveArtifactAdmissionPropagatesInvalidArtifactPath(t *testing.T) {
	const secret = "provider-auth-secret-outside-640"
	cfg, st, _ := sensitiveArtifactTestState(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret="+secret), 0o600); err != nil {
		t.Fatal(err)
	}

	err := validateSensitiveResultArtifacts(
		&ClaudeRunner{config: cfg, state: st},
		sensitiveArtifactResult(t, outside),
		[]SensitiveArtifactValue{{Category: SensitiveArtifactProviderAuthToken, Value: secret}},
	)
	if err == nil {
		t.Fatal("outside-root artifact validation error was discarded")
	}
	var sensitive *SensitiveArtifactError
	if errors.As(err, &sensitive) {
		t.Fatalf("outside-root artifact must not be scanned as an admitted artifact: %v", err)
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("outside-root artifact was modified: %v", statErr)
	}
}

func TestSensitiveArtifactAdmissionFindsSecretAcrossScanChunks(t *testing.T) {
	const secret = "provider-auth-secret-cross-chunk-640"
	cfg, st, artifact := sensitiveArtifactTestState(t)
	prefix := make([]byte, sensitiveArtifactScanChunkBytes-len(secret)/2)
	for i := range prefix {
		prefix[i] = 'x'
	}
	content := make([]byte, 0, len(prefix)+len(secret)+len("tail"))
	content = append(content, prefix...)
	content = append(content, []byte(secret)...)
	content = append(content, []byte("tail")...)
	if err := os.WriteFile(artifact, content, 0o600); err != nil {
		t.Fatal(err)
	}

	err := validateSensitiveResultArtifacts(
		&ClaudeRunner{config: cfg, state: st},
		sensitiveArtifactResult(t, artifact),
		[]SensitiveArtifactValue{{Category: SensitiveArtifactProviderAuthToken, Value: secret}},
	)
	var sensitive *SensitiveArtifactError
	if !errors.As(err, &sensitive) || sensitive.Category != SensitiveArtifactProviderAuthToken {
		t.Fatalf("cross-chunk secret was not rejected: %v", err)
	}
	if _, statErr := os.Stat(artifact); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cross-chunk sensitive artifact remained: %v", statErr)
	}
}
