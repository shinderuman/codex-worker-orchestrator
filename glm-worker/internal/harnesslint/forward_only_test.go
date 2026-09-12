package harnesslint

import "testing"

func TestForwardOnlyCompatibilityRejectsGoReintroductionPatterns(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		source string
	}{
		{
			name: "old schema promotion",
			path: "glm-worker/internal/example/decoder.go",
			source: `package example
const currentVersion = 3
type marker struct { Version int }
func decodeMarker(m marker) marker {
	if m.Version == 1 { m.Version = currentVersion }
	return m
}
`,
		},
		{
			name: "multi version bridge",
			path: "glm-worker/internal/example/decoder.go",
			source: `package example
const currentVersion = 3
type marker struct { Version int }
func decodeMarker(m marker) marker {
	switch m.Version { case 1, 2: m.Version = currentVersion }
	return m
}
`,
		},
		{
			name: "schema range acceptance",
			path: "glm-worker/internal/example/evidence.go",
			source: `package example
const currentRevision = 3
type record struct { SchemaRevision int }
func readEvidence(r record) error {
	if r.SchemaRevision > currentRevision { return errUnsupported }
	return nil
}
var errUnsupported error
`,
		},
		{
			name: "legacy ownership promotion",
			path: "glm-worker/internal/example/install.go",
			source: `package example
type legacyManifest struct { Present bool; State installState }
type installState struct{}
func install(stateExists bool, legacy legacyManifest) error {
	var currentState installState
	if !stateExists && legacy.Present {
		currentState = legacy.State
		return saveState(currentState)
	}
	return nil
}
func saveState(installState) error { return nil }
`,
		},
		{
			name: "legacy path promotion",
			path: "glm-worker/internal/example/install.go",
			source: `package example
func ensureCanonical(canonicalExists bool, legacyPath, canonicalPath string) error {
	if !canonicalExists { return copyFile(legacyPath, canonicalPath) }
	return nil
}
func copyFile(string, string) error { return nil }
`,
		},
		{
			name: "explicit legacy migration helper",
			path: "glm-worker/internal/example/install.go",
			source: `package example
func MigrateLegacyInstall() error { return nil }
func install() error { return MigrateLegacyInstall() }
`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := fixtureRoot(t)
			writeFixture(t, root, testCase.path, testCase.source)
			requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, testCase.path)
		})
	}
}

func TestForwardOnlyCompatibilityRejectsPATHBinaryPromotion(t *testing.T) {
	root := fixtureRoot(t)
	path := "install.sh"
	writeFixture(t, root, path, `#!/bin/sh
set -eu
source_path=$(command -v "$tool")
cp "$source_path" "$canonical_path"
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlyCompatibilityRejectsOldAcceptanceTests(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/decoder_test.go"
	writeFixture(t, root, path, `package example
import "testing"
func TestDecoderAcceptsOldVersion(t *testing.T) {}
func TestInstallMigratesLegacyOwnership(t *testing.T) {}
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlyCompatibilityAllowsCurrentOnlyAndLegitimateFallbacks(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/decoder.go", `package example
const currentVersion = 3
type marker struct { Version int }
func decodeMarker(m marker) error {
	if m.Version != currentVersion { return errUnsupported }
	return nil
}
var errUnsupported error
const legacyEvidenceLabel = "legacy-evidence:lifecycle"
func stopEndpointPath(primary, fallback string) string {
	if primary != "" { return primary }
	return fallback
}
`)
	writeFixture(t, root, "glm-worker/internal/example/decoder_test.go", `package example
import "testing"
func TestDecoderRejectsOldVersion(t *testing.T) {}
func TestInstallDoesNotMigrateLegacyOwnership(t *testing.T) {}
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func TestForwardOnlyCompatibilityIgnoresOwnNegativeFixtures(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/harnesslint/fixtures/negative.go", `package fixtures
const currentVersion = 3
type marker struct { Version int }
func decodeMarker(m marker) marker {
	if m.Version == 1 { m.Version = currentVersion }
	return m
}
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func assertNoForwardOnlyViolations(t *testing.T, violations []Violation) {
	t.Helper()
	for _, violation := range violations {
		if violation.Rule == forwardOnlyCompatibilityRule {
			t.Fatalf("forward-only rule rejected legitimate fixture: %+v", violation)
		}
	}
}
