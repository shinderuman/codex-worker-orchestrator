package taskcontract

import (
	"fmt"
	"strings"
)

const (
	TasksDir                   = "IMPLEMENTATION_TASKS"
	ExternalFeasibilityHeading = "## External feasibility"
)

type ExternalFeasibility struct {
	Status         string
	Assumption     string
	EvidenceSource string
	Evidence       string
	GoDecision     string
}

type FeasibilityRejectKind int

const (
	FeasibilityRejectMissing FeasibilityRejectKind = iota + 1
	FeasibilityRejectMalformed
	FeasibilityRejectUnverified
)

type FeasibilityError struct {
	Kind   FeasibilityRejectKind
	Reason string
}

const (
	StatusNotApplicable  = "not-applicable"
	StatusPoC            = "poc"
	StatusObservation    = "observation"
	StatusImplementation = "implementation"
)

const evidenceProducer = "producer"

var feasibilityFieldKeys = []string{"status", "assumption", "evidence-source", "evidence", "go"}

func (e *FeasibilityError) Error() string { return e.Reason }

func ActiveSectionEntries(planContent string) ([]string, error) {
	lines := strings.Split(planContent, "\n")
	inSection := false
	var entries []string
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			inSection = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == "ACTIVE"
			continue
		}
		if !inSection {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			return nil, fmt.Errorf("IMPLEMENTATION_PLAN.local.mdのACTIVE欄の行 %qがschedule list記法(`- `bulletとblank行のみ)へ違反しています", trimmed)
		}
		path, err := activeEntryPath(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		if err != nil {
			return nil, err
		}
		entries = append(entries, path)
	}
	return entries, nil
}

func ValidateActiveTaskPath(path string) error {
	prefix := TasksDir + "/"
	if !strings.HasPrefix(path, prefix) {
		return fmt.Errorf("ACTIVE task path %qは%s配下である必要があります", path, TasksDir)
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" || strings.Contains(path, `\`) || strings.Contains(rest, "//") {
		return fmt.Errorf("ACTIVE task path %qが配置契約に違反しています", path)
	}
	if !strings.HasSuffix(path, ".md") {
		return fmt.Errorf("ACTIVE task path %qが配置契約に違反しています(.md fileに限定)", path)
	}
	for _, segment := range strings.Split(rest, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("ACTIVE task path %qが配置契約に違反しています", path)
		}
	}
	return nil
}

func activeEntryPath(item string) (string, error) {
	switch strings.Count(item, "`") {
	case 0:
		return item, nil
	case 2:
		if strings.HasPrefix(item, "`") && strings.HasSuffix(item, "`") {
			return item[1 : len(item)-1], nil
		}
	}
	return "", fmt.Errorf("ACTIVE欄の項目 %qがbullet構文(逆引用符1組で囲まれた単一task path、または逆引用符なしの直書き)へ違反しています", item)
}

func ParseExternalFeasibility(content []byte) (ExternalFeasibility, error) {
	var declaration ExternalFeasibility
	lines := strings.Split(string(content), "\n")
	headingAt, err := findFeasibilitySection(lines)
	if err != nil {
		return declaration, err
	}
	values, err := parseFeasibilityValues(lines, headingAt)
	if err != nil {
		return declaration, err
	}
	return validateFeasibilityFields(values)
}

func findFeasibilitySection(lines []string) (int, error) {
	headingAt := -1
	sections := 0
	fence := 0
	for index, line := range lines {
		if !lineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") && strings.TrimSpace(line) == ExternalFeasibilityHeading {
			sections++
			if headingAt < 0 {
				headingAt = index
			}
		}
	}
	switch {
	case sections == 0:
		return -1, feasibilityError(FeasibilityRejectMissing, "External feasibility節("+ExternalFeasibilityHeading+")がありません")
	case sections > 1:
		return -1, feasibilityError(FeasibilityRejectMalformed, fmt.Sprintf("External feasibility節が複数あります(%d節)", sections))
	default:
		return headingAt, nil
	}
}

func parseFeasibilityValues(lines []string, headingAt int) (map[string]string, error) {
	values := map[string]string{}
	fence := 0
	for index := headingAt + 1; index < len(lines); index++ {
		line := lines[index]
		if !lineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if err := addFeasibilityValue(values, trimmed, index+1); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func addFeasibilityValue(values map[string]string, line string, lineNumber int) error {
	key, value, ok := strings.Cut(line, ":")
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if !ok || !knownFeasibilityKey(key) {
		return feasibilityError(FeasibilityRejectMalformed, fmt.Sprintf("External feasibility節の%d行目 %qがkey: value形式ではありません(使えるkey: %s)", lineNumber, line, strings.Join(feasibilityFieldKeys, ", ")))
	}
	if _, duplicate := values[key]; duplicate {
		return feasibilityError(FeasibilityRejectMalformed, "External feasibility節のkey \""+key+"\"が重複しています")
	}
	if value == "" {
		return feasibilityError(FeasibilityRejectMalformed, "External feasibility節のkey \""+key+"\"のvalueが空です")
	}
	values[key] = value
	return nil
}

func validateFeasibilityFields(values map[string]string) (ExternalFeasibility, error) {
	status := values["status"]
	if err := validateFeasibilityStatus(status, values); err != nil {
		return ExternalFeasibility{}, err
	}
	return ExternalFeasibility{
		Status:         status,
		Assumption:     values["assumption"],
		EvidenceSource: values["evidence-source"],
		Evidence:       values["evidence"],
		GoDecision:     values["go"],
	}, nil
}

func validateFeasibilityStatus(status string, values map[string]string) error {
	switch status {
	case "":
		return feasibilityError(FeasibilityRejectMalformed, "External feasibility節にstatusがありません(not-applicable/poc/observation/implementation)")
	case StatusNotApplicable:
		return validateNotApplicable(values)
	case StatusPoC, StatusObservation:
		return validateExploration(status, values)
	case StatusImplementation:
		return validateImplementation(values)
	default:
		return feasibilityError(FeasibilityRejectMalformed, fmt.Sprintf("External feasibilityのstatus %qは未知です(not-applicable/poc/observation/implementation)", status))
	}
}

func validateNotApplicable(values map[string]string) error {
	for _, key := range feasibilityFieldKeys[1:] {
		if values[key] != "" {
			return feasibilityError(FeasibilityRejectMalformed, "not-applicableではstatus以外のkey("+key+")を書けません。外部前提がある場合はpoc/observation/implementationを宣言してください")
		}
	}
	return nil
}

func validateExploration(status string, values map[string]string) error {
	if values["assumption"] == "" {
		return feasibilityError(FeasibilityRejectMalformed, status+"では未検証外部前提を表すassumptionが必須です")
	}
	for _, key := range []string{"evidence-source", "evidence", "go"} {
		if values[key] != "" {
			return feasibilityError(FeasibilityRejectMalformed, status+"では"+key+"を書けません。implementation昇格は親Codexが宣言全体を書き換えて行います")
		}
	}
	return nil
}

func validateImplementation(values map[string]string) error {
	if values["assumption"] == "" {
		return feasibilityError(FeasibilityRejectMalformed, "implementationでは前提とした外部成立性のassumptionが必須です")
	}
	if values["evidence-source"] == "" || values["evidence"] == "" || values["go"] == "" {
		return feasibilityError(FeasibilityRejectUnverified, "implementationはevidence-source・evidence・go(親Go判断)の全てが必須です。PoC結果をGLMだけでGoへ昇格させない")
	}
	if values["evidence-source"] != evidenceProducer {
		return feasibilityError(FeasibilityRejectUnverified, "implementationのevidence-sourceは実producer由来の \""+evidenceProducer+"\" だけです(人工fixture・scripted packet・worker/reviewer PASSは不可)")
	}
	return nil
}

func knownFeasibilityKey(key string) bool {
	for _, known := range feasibilityFieldKeys {
		if key == known {
			return true
		}
	}
	return false
}

func feasibilityError(kind FeasibilityRejectKind, reason string) error {
	return &FeasibilityError{Kind: kind, Reason: reason}
}

func lineOutsideFence(line string, fence *int) bool {
	backticks := leadingBackticks(line)
	if *fence > 0 {
		if backticks >= *fence {
			*fence = 0
		}
		return false
	}
	if backticks >= 3 {
		*fence = backticks
		return false
	}
	return true
}

func leadingBackticks(line string) int {
	count := 0
	for count < len(line) && line[count] == '`' {
		count++
	}
	return count
}
