package taskcontract

import (
	"fmt"
	"strings"
)

const DarwinAcceptanceHeading = "## Darwin acceptance"

func ParseDarwinAcceptance(content []byte) ([]string, error) {
	lines := strings.Split(string(content), "\n")
	heading, err := findUniqueTaskSection(lines, DarwinAcceptanceHeading)
	if err != nil || heading < 0 {
		return nil, err
	}
	var tests []string
	for _, line := range lines[heading+1:] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry, ok := strings.CutPrefix(line, "- `")
		if !ok || !strings.HasSuffix(entry, "`") {
			return nil, fmt.Errorf("%s requires a list of package:TestName entries", DarwinAcceptanceHeading)
		}
		tests = append(tests, strings.TrimSuffix(entry, "`"))
	}
	if len(tests) == 0 {
		return nil, fmt.Errorf("%s does not declare a test", DarwinAcceptanceHeading)
	}
	return tests, nil
}
