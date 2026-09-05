package state

import (
	"path/filepath"
	"strings"
)

const (
	FixCategoryProduction    = "production"
	FixCategoryTest          = "test"
	FixCategoryInstruction   = "instruction"
	FixCategoryTelemetry     = "telemetry"
	FixCategoryDocumentation = "documentation"
	FixCategoryMetadata      = "metadata"
	FixCategoryOther         = "other"
)

var fixCategoryTestSegments = map[string]bool{
	"test":      true,
	"tests":     true,
	"__tests__": true,
	"testdata":  true,
	"scenarios": true,
	"fixtures":  true,
}

var fixCategoryInstructionBases = map[string]bool{
	"agents.md":       true,
	"agents.local.md": true,
	"claude.md":       true,
}

var fixCategoryMetadataBases = map[string]bool{
	"implementation_plan.local.md": true,
	"implementation_rules.md":      true,
	"implementation_history.md":    true,
}

var fixCategoryTestSuffixes = []string{
	"_test.go", "_test.py", "_test.ts", "_test.tsx", "_test.js", "_test.jsx",
	".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx",
	".test.ts", ".test.tsx", ".test.js", ".test.jsx",
}

func FixPathCategory(relPath string) string {
	normalized := strings.ToLower(strings.TrimPrefix(filepath.ToSlash(relPath), "./"))
	base := filepath.Base(normalized)
	if fixCategoryMetadataBases[base] || strings.HasPrefix(normalized, "implementation_tasks/") {
		return FixCategoryMetadata
	}
	if fixCategoryInstructionBases[base] ||
		strings.HasPrefix(normalized, "codex/instructions/") ||
		strings.HasPrefix(normalized, ".codex/instructions/") {
		return FixCategoryInstruction
	}
	if fixPathIsTest(base) {
		return FixCategoryTest
	}
	for _, segment := range strings.Split(normalized, "/") {
		if fixCategoryTestSegments[segment] {
			return FixCategoryTest
		}
		if segment == "telemetry" {
			return FixCategoryTelemetry
		}
	}
	switch RoundPathClass(relPath) {
	case RoundPathClassDoc:
		return FixCategoryDocumentation
	case RoundPathClassCode:
		return FixCategoryProduction
	}
	return FixCategoryOther
}

func fixPathIsTest(base string) bool {
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return true
	}
	for _, suffix := range fixCategoryTestSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}
