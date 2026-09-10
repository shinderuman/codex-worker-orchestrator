package repositoryharness

import "testing"

func TestModuleDirectivePathAcceptsEquivalentModuleForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{name: "plain", line: "module " + repositoryModulePath},
		{name: "trailing-comment", line: "module " + repositoryModulePath + " // repository module"},
		{name: "quoted", line: "module \"" + repositoryModulePath + "\""},
		{name: "quoted-comment", line: "module \"" + repositoryModulePath + "\" // repository module"},
		{name: "raw-quoted", line: "module `" + repositoryModulePath + "`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := moduleDirectivePath(tc.line)
			if !ok || got != repositoryModulePath {
				t.Fatalf("moduleDirectivePath(%q) = %q, %v", tc.line, got, ok)
			}
		})
	}
}

func TestModuleDirectivePathRejectsDifferentOrMalformedDirective(t *testing.T) {
	for _, line := range []string{
		"module example.com/foreign",
		"module",
		"modulex " + repositoryModulePath,
		"module \"" + repositoryModulePath,
		"module " + repositoryModulePath + " unexpected",
	} {
		got, ok := moduleDirectivePath(line)
		if ok && got == repositoryModulePath {
			t.Fatalf("moduleDirectivePath(%q) accepted repository identity", line)
		}
	}
}
