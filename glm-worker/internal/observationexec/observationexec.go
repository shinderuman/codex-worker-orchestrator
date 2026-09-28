package observationexec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type Operation string

type Request struct {
	Operation  Operation
	Reference  string
	WorkingDir string
	DeadlineMS int64
}

type RequestError struct {
	Reason string
}

const (
	OperationShadowEval Operation = "shadow-eval"
	OperationGoTest     Operation = "go-test"
	OperationGoTestRace Operation = "go-test-race"
)

const (
	slotOperation  = "OPERATION"
	slotReference  = "REFERENCE"
	slotWorkingDir = "WORKING_DIR"
	slotDeadlineMS = "DEADLINE_MS"

	slotUnsetValue  = "-"
	deadlineDefault = "default"

	DefaultDeadlineMS = 10 * 60 * 1000
	MinDeadlineMS     = 30 * 1000
	MaxDeadlineMS     = 30 * 60 * 1000
)

var requestSlots = []string{slotOperation, slotReference, slotWorkingDir, slotDeadlineMS}

func (e *RequestError) Error() string { return e.Reason }

func Operations() []string {
	return []string{string(OperationShadowEval), string(OperationGoTest), string(OperationGoTestRace)}
}

func (o Operation) Valid() bool {
	switch o {
	case OperationShadowEval, OperationGoTest, OperationGoTestRace:
		return true
	default:
		return false
	}
}

func (o Operation) IsGoTest() bool {
	return o == OperationGoTest || o == OperationGoTestRace
}

func ParseRequest(payload []byte) (Request, error) {
	values, err := parsePayloadSlots(payload)
	if err != nil {
		return Request{}, err
	}
	return buildValidatedRequest(values)
}

func parsePayloadSlots(payload []byte) (map[string]string, error) {
	lines := strings.Split(strings.TrimRight(string(payload), "\n"), "\n")
	if len(lines) != len(requestSlots) {
		return nil, &RequestError{Reason: fmt.Sprintf("observation-execute payloadは%s/%s/%s/%sの4行形式である必要があります(行数: %d)", requestSlots[0], requestSlots[1], requestSlots[2], requestSlots[3], len(lines))}
	}
	values := make(map[string]string, len(requestSlots))
	for _, line := range lines {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			return nil, &RequestError{Reason: fmt.Sprintf("observation-execute payloadの行 %qが\"KEY: value\"形式ではありません", line)}
		}
		if _, duplicate := values[key]; duplicate {
			return nil, &RequestError{Reason: fmt.Sprintf("observation-execute payloadのkey %qが重複しています", key)}
		}
		values[key] = strings.TrimSpace(value)
	}
	for _, slot := range requestSlots {
		if values[slot] == "" {
			return nil, &RequestError{Reason: fmt.Sprintf("observation-execute payloadのslot %sがありません", slot)}
		}
	}
	return values, nil
}

func buildValidatedRequest(values map[string]string) (Request, error) {
	operation := Operation(values[slotOperation])
	if !operation.Valid() {
		return Request{}, &RequestError{Reason: fmt.Sprintf("operation %qは閉集合(%s)の外です", values[slotOperation], strings.Join(Operations(), "/"))}
	}
	request := Request{Operation: operation, Reference: values[slotReference], WorkingDir: values[slotWorkingDir]}
	if err := validateSlotUsage(request); err != nil {
		return Request{}, err
	}
	deadline, err := parseDeadline(values[slotDeadlineMS])
	if err != nil {
		return Request{}, err
	}
	request.DeadlineMS = deadline
	return request, nil
}

func validateSlotUsage(request Request) error {
	if request.Reference != slotUnsetValue && request.Operation != OperationShadowEval {
		return &RequestError{Reason: "reference slotはshadow-eval以外では使えません(\"-\"を指定してください)"}
	}
	if request.WorkingDir != slotUnsetValue && !request.Operation.IsGoTest() {
		return &RequestError{Reason: "working-dir slotはgo-test系operation以外では使えません(\"-\"を指定してください)"}
	}
	if request.Reference != slotUnsetValue {
		if err := validateRelativeLocator(request.Reference); err != nil {
			return err
		}
	}
	if request.WorkingDir != slotUnsetValue {
		if err := validateRelativeLocator(request.WorkingDir); err != nil {
			return err
		}
	}
	return nil
}

func parseDeadline(value string) (int64, error) {
	if value == deadlineDefault {
		return DefaultDeadlineMS, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, &RequestError{Reason: fmt.Sprintf("deadline-ms %qは\"default\"または整数である必要があります", value)}
	}
	if parsed < MinDeadlineMS || parsed > MaxDeadlineMS {
		return 0, &RequestError{Reason: fmt.Sprintf("deadline-msは%d..%dの範囲にしてください(%d)", MinDeadlineMS, MaxDeadlineMS, parsed)}
	}
	return parsed, nil
}

func validateRelativeLocator(locator string) error {
	if locator == "" || filepath.IsAbs(locator) || locator == "." || locator == ".." {
		return &RequestError{Reason: fmt.Sprintf("locator %qは相対pathである必要があります", locator)}
	}
	for _, segment := range strings.Split(filepath.ToSlash(locator), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return &RequestError{Reason: fmt.Sprintf("locator %qに空または親遷移のsegmentがあります", locator)}
		}
	}
	return nil
}

func (r Request) Digest() string {
	canonical := fmt.Sprintf("operation=%s\nreference=%s\nworking-dir=%s\ndeadline-ms=%d",
		r.Operation,
		filepath.ToSlash(strings.TrimSpace(r.Reference)),
		filepath.ToSlash(strings.TrimSpace(r.WorkingDir)),
		r.ResolvedDeadlineMS(),
	)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func (r Request) ResolvedDeadlineMS() int64 {
	if r.DeadlineMS > 0 {
		return r.DeadlineMS
	}
	return DefaultDeadlineMS
}
