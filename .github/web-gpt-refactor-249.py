from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    if old not in text:
        raise SystemExit(f"replacement target missing in {path}:\n{old}")
    file.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/state/telemetry.go",
    '''import (\n\t"bufio"\n\t"encoding/json"\n\t"fmt"''',
    '''import (\n\t"bufio"\n\t"encoding/json"\n\t"errors"\n\t"fmt"''',
)

replace_once(
    "glm-worker/internal/state/telemetry.go",
    '''func (s *StateStore) ReadModelCallLogs(taskID string) ([]ModelCallLog, error) {\n\tfile, err := os.Open(s.ModelCallLogPath(taskID))\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tdefer func() { _ = file.Close() }()\n\n\tvar result []ModelCallLog\n\tscanner := bufio.NewScanner(file)\n\tbuffer := make([]byte, 64*1024)\n\tscanner.Buffer(buffer, 4*1024*1024)\n\tfor scanner.Scan() {\n\t\tvar value ModelCallLog\n\t\tif err := json.Unmarshal(scanner.Bytes(), &value); err != nil {\n\t\t\treturn nil, fmt.Errorf("telemetryを読めません: %w", err)\n\t\t}\n\t\tif value.Version != modelCallLogVersion {\n\t\t\tcontinue\n\t\t}\n\t\tresult = append(result, value)\n\t}\n\tif err := scanner.Err(); err != nil {\n\t\treturn nil, err\n\t}\n\treturn result, nil\n}\n''',
    '''func (s *StateStore) ReadModelCallLogs(taskID string) ([]ModelCallLog, error) {\n\tfile, err := os.Open(s.ModelCallLogPath(taskID))\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tdefer func() { _ = file.Close() }()\n\n\tvar result []ModelCallLog\n\tscanner := bufio.NewScanner(file)\n\tbuffer := make([]byte, 64*1024)\n\tscanner.Buffer(buffer, 4*1024*1024)\n\tfor scanner.Scan() {\n\t\tvar value ModelCallLog\n\t\tif err := json.Unmarshal(scanner.Bytes(), &value); err != nil {\n\t\t\treturn nil, fmt.Errorf("telemetryを読めません: %w", err)\n\t\t}\n\t\tif value.Version != modelCallLogVersion {\n\t\t\tcontinue\n\t\t}\n\t\tresult = append(result, value)\n\t}\n\tif err := scanner.Err(); err != nil {\n\t\treturn nil, err\n\t}\n\treturn result, nil\n}\n\nfunc (s *StateStore) CountFinalizedTaskCalls(taskID string) (int, error) {\n\tlogs, err := s.ReadModelCallLogs(taskID)\n\tif err != nil {\n\t\tif errors.Is(err, os.ErrNotExist) {\n\t\t\treturn 0, nil\n\t\t}\n\t\treturn 0, err\n\t}\n\tcount := 0\n\tfor _, log := range logs {\n\t\tif log.CallType == CallTypeTask {\n\t\t\tcount++\n\t\t}\n\t}\n\treturn count, nil\n}\n''',
)

replace_once(
    "glm-worker/internal/state/coverage.go",
    '''import (\n\t"bufio"\n\t"encoding/json"\n\t"errors"\n\t"fmt"\n\t"os"\n\t"path/filepath"\n\t"strings"\n)''',
    '''import (\n\t"path/filepath"\n\t"strings"\n)''',
)
replace_once(
    "glm-worker/internal/state/coverage.go",
    '''\t\trecords, err := s.countTaskCallRecords(task.TaskID)''',
    '''\t\trecords, err := s.CountFinalizedTaskCalls(task.TaskID)''',
)
replace_once(
    "glm-worker/internal/state/coverage.go",
    '''func (s *StateStore) countTaskCallRecords(taskID string) (int, error) {\n\tfile, err := os.Open(s.ModelCallLogPath(taskID))\n\tif err != nil {\n\t\tif errors.Is(err, os.ErrNotExist) {\n\t\t\treturn 0, nil\n\t\t}\n\t\treturn 0, err\n\t}\n\tdefer func() { _ = file.Close() }()\n\n\tcount := 0\n\tscanner := bufio.NewScanner(file)\n\tscanner.Buffer(make([]byte, 64*1024), 4*1024*1024)\n\tfor scanner.Scan() {\n\t\tvar record struct {\n\t\t\tVersion  int    `json:"version"`\n\t\t\tCallType string `json:"call_type"`\n\t\t}\n\t\tif err := json.Unmarshal(scanner.Bytes(), &record); err != nil {\n\t\t\treturn 0, fmt.Errorf("telemetryを読めません: %w", err)\n\t\t}\n\t\tif record.Version == modelCallLogVersion && record.CallType == CallTypeTask {\n\t\t\tcount++\n\t\t}\n\t}\n\tif err := scanner.Err(); err != nil {\n\t\treturn 0, err\n\t}\n\treturn count, nil\n}\n\n''',
    "",
)

replace_once(
    "glm-worker/internal/app/bundle.go",
    '''func bundleInFlightModelCalls(st *state.StateStore, task bundleTask) int {\n\tif !task.Current || task.Stats.ModelCalls <= 0 {\n\t\treturn 0\n\t}\n\tlogs, err := st.ReadModelCallLogs(task.ID)\n\tif err != nil && !errors.Is(err, os.ErrNotExist) {\n\t\treturn 0\n\t}\n\tfinalized := 0\n\tfor _, log := range logs {\n\t\tif log.CallType == "" || log.CallType == state.CallTypeTask {\n\t\t\tfinalized++\n\t\t}\n\t}\n\tpending := task.Stats.ModelCalls - finalized\n\tif pending < 0 {\n\t\treturn 0\n\t}\n\treturn pending\n}\n''',
    '''func bundleInFlightModelCalls(st *state.StateStore, task bundleTask) int {\n\tif !task.Current || task.Stats.ModelCalls <= 0 {\n\t\treturn 0\n\t}\n\tfinalized, err := st.CountFinalizedTaskCalls(task.ID)\n\tif err != nil {\n\t\treturn task.Stats.ModelCalls\n\t}\n\tpending := task.Stats.ModelCalls - finalized\n\tif pending < 0 {\n\t\treturn 0\n\t}\n\treturn pending\n}\n''',
)
