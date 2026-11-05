package qualitygate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strings"
)

type DarwinTest struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

const darwinSupplementPackage = "./internal/observationexec"

var darwinPackageSegment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func NormalizeDarwinTests(tests []DarwinTest) ([]DarwinTest, error) {
	if len(tests) == 0 {
		return nil, nil
	}
	normalized := slices.Clone(tests)
	for _, test := range normalized {
		if !validDarwinPackage(test.Package) || !strings.HasPrefix(test.Name, "Test") || !token.IsIdentifier(test.Name) {
			return nil, fmt.Errorf("invalid required Darwin test %q in %q", test.Name, test.Package)
		}
	}
	slices.SortFunc(normalized, compareDarwinTests)
	return slices.Compact(normalized), nil
}

func validDarwinPackage(value string) bool {
	if value == "." {
		return true
	}
	relative, ok := strings.CutPrefix(value, "./")
	if !ok || path.Clean(relative) != relative || strings.Contains(relative, "...") {
		return false
	}
	for _, segment := range strings.Split(relative, "/") {
		if !darwinPackageSegment.MatchString(segment) {
			return false
		}
	}
	return true
}

func compareDarwinTests(left, right DarwinTest) int {
	if order := strings.Compare(left.Package, right.Package); order != 0 {
		return order
	}
	return strings.Compare(left.Name, right.Name)
}

func DarwinSupplementArguments(form string, tests ...DarwinTest) []string {
	args := GoTestArguments(form)
	if len(args) == 0 {
		return nil
	}
	args[len(args)-1] = darwinSupplementPackage
	for _, test := range tests {
		if !slices.Contains(args, test.Package) {
			args = append(args, test.Package)
		}
	}
	return args
}

func VerifyDarwinCoverage(log []byte, tests ...DarwinTest) error {
	required := []DarwinTest{
		{Package: darwinSupplementPackage, Name: "TestConfinementProfileBoundsHostCredentialReads"},
		{Package: darwinSupplementPackage, Name: "TestConfinementRuntimeDeniesCredentialReads"},
		{Package: darwinSupplementPackage, Name: "TestRunIsolatedGoTestConfinementBlocksWritesOutsideAllowedRoots"},
		{Package: darwinSupplementPackage, Name: "TestConfinementAdmissionDeclaresPlatformSupport"},
	}
	required = append(required, tests...)
	passed := darwinPassedTests(log)
	module, ok := darwinSupplementModule(passed[required[0].Name])
	if !ok {
		return fmt.Errorf("required Darwin test did not PASS: %s %s", required[0].Package, required[0].Name)
	}
	for _, test := range required {
		if !slices.Contains(passed[test.Name], module+strings.TrimPrefix(test.Package, ".")) {
			return fmt.Errorf("required Darwin test did not PASS: %s %s", test.Package, test.Name)
		}
	}
	return nil
}

func darwinSupplementModule(packages []string) (string, bool) {
	suffix := strings.TrimPrefix(darwinSupplementPackage, ".")
	for _, pkg := range packages {
		if module, ok := strings.CutSuffix(pkg, suffix); ok && module != "" {
			return module, true
		}
	}
	return "", false
}

func darwinPassedTests(log []byte) map[string][]string {
	passed := make(map[string][]string)
	for _, line := range bytes.Split(log, []byte("\n")) {
		var event struct {
			Action  string
			Test    string
			Package string
		}
		if json.Unmarshal(line, &event) == nil && event.Action == "pass" && event.Test != "" {
			passed[event.Test] = append(passed[event.Test], event.Package)
		}
	}
	return passed
}
