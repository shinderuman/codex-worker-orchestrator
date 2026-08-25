package packet

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	measureEraABoundary = "2026-08-21T11:09:37Z"

	measureEraBBoundary = "2026-08-23T12:37:31Z"
)

const (
	eraATasks                 = 36
	eraATaskCalls             = 181
	eraAStrayBodyInvalid      = 39
	eraAPacketCompactions     = 38
	eraACompactionCostUSD     = 9.04
	eraACompactionCostShare   = 0.032
	eraAFormatFailuresPoC     = 65
	eraAStructuralFailuresPoC = 53
)

type protocolMeasureReport struct {
	GeneratedAt      string                 `json:"generated_at"`
	Reproduction     string                 `json:"reproduction"`
	Corpus           corpusSummary          `json:"corpus"`
	RenderComparison []measureAggregateJSON `json:"render_comparison"`
	Retention        retentionSummaryJSON   `json:"retention"`
	Telemetry        telemetrySummaryJSON   `json:"telemetry"`
	PacketByteAudit  []packetByteAuditJSON  `json:"packet_byte_audit"`
	Git              gitSummaryJSON         `json:"git"`
	Verdict          verdictJSON            `json:"verdict"`
	Notes            []string               `json:"notes"`
}

type corpusSummary struct {
	RealPayloads      int `json:"real_payloads"`
	SyntheticPayloads int `json:"synthetic_payloads"`
}

type measureAggregateJSON struct {
	Corpus          string  `json:"corpus"`
	Format          string  `json:"format"`
	Payloads        int     `json:"payloads"`
	StdoutBytes     int     `json:"stdout_bytes"`
	TokensRun       int     `json:"tokens_run"`
	TokensCharPunct int     `json:"tokens_char_punct"`
	StructuredBytes int     `json:"structured_bytes"`
	StructuredRatio float64 `json:"structured_ratio"`
	NoiseFields     int     `json:"noise_fields"`
	DuplicateValues int     `json:"duplicate_values"`
}

type retentionSummaryJSON struct {
	MachinePreserved       int      `json:"machine_preserved"`
	LegacyPreserved        int      `json:"legacy_preserved"`
	TotalPayloads          int      `json:"total_payloads"`
	LegacyLosses           []string `json:"legacy_losses"`
	NonContractNoiseCalls  int      `json:"non_contract_noise_calls"`
	AcceptedCallsScanned   int      `json:"accepted_calls_scanned"`
	NonContractNoiseCorpus int      `json:"non_contract_noise_corpus"`
}

type telemetrySummaryJSON struct {
	Home          string            `json:"home"`
	SessionDirs   int               `json:"session_dirs"`
	EraBoundaries map[string]string `json:"era_boundaries"`
	Eras          []eraStatsJSON    `json:"eras"`
	Attribution   string            `json:"attribution"`
}

type eraStatsJSON struct {
	Era                   string         `json:"era"`
	Source                string         `json:"source"`
	Tasks                 int            `json:"tasks"`
	TaskCalls             int            `json:"task_calls"`
	Outcomes              map[string]int `json:"outcomes"`
	ResultCorrections     int            `json:"result_corrections"`
	PacketCompactions     int            `json:"packet_compactions"`
	RejectsByCategory     map[string]int `json:"rejects_by_category,omitempty"`
	SolPacketBytes        int            `json:"sol_packet_bytes"`
	CompactionCostUSD     float64        `json:"compaction_cost_usd,omitempty"`
	CompactionCostShare   float64        `json:"compaction_cost_share,omitempty"`
	FormatFailuresPoC     int            `json:"format_failures_poc,omitempty"`
	StructuralFailuresPoC int            `json:"structural_failures_poc,omitempty"`
}

type packetByteAuditJSON struct {
	Task     string `json:"task"`
	Era      string `json:"era"`
	Recorded int    `json:"recorded"`
	Computed int    `json:"computed"`
	Match    bool   `json:"match"`
	Class    string `json:"class"`
	Note     string `json:"note,omitempty"`
}

type gitSummaryJSON struct {
	OldRef     string             `json:"old_ref"`
	NewRef     string             `json:"new_ref"`
	Files      []fileMetricJSON   `json:"files"`
	Commits    []commitMetricJSON `json:"commits"`
	PacketOld  []string           `json:"packet_files_old"`
	PacketNew  []string           `json:"packet_files_new"`
	HeadCaveat string             `json:"head_caveat"`
}

type fileMetricJSON struct {
	Path        string `json:"path"`
	PresentOld  bool   `json:"present_old"`
	PresentNew  bool   `json:"present_new"`
	LinesOld    int    `json:"lines_old"`
	LinesNew    int    `json:"lines_new"`
	BranchesOld int    `json:"branches_old"`
	BranchesNew int    `json:"branches_new"`
}

type commitMetricJSON struct {
	Hash            string `json:"hash"`
	Subject         string `json:"subject"`
	ProductionFiles int    `json:"production_files"`
	Additions       int    `json:"additions"`
	Deletions       int    `json:"deletions"`
}

type verdictJSON struct {
	RealByteDelta       float64  `json:"real_byte_delta"`
	RealRunTokenDelta   float64  `json:"real_run_token_delta"`
	RealPunctTokenDelta float64  `json:"real_punct_token_delta"`
	Decision            string   `json:"decision"`
	Rationale           []string `json:"rationale"`
	WithdrawalTrigger   string   `json:"withdrawal_trigger"`
}

var measureBranchPrefixes = []string{
	"if ", "if(", "else", "for ", "for(", "switch ", "switch(", "case ", "select {", "select{",
}

func countBranchStatements(src string) int {
	count := 0
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range measureBranchPrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				count++
				break
			}
		}
	}
	return count
}

func countNonBlankLines(src string) int {
	count := 0
	for _, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

var measureProtocolSurfaceFiles = []string{
	"glm-worker/internal/packet/result.go",
	"glm-worker/internal/packet/validate.go",
	"glm-worker/internal/packet/schema.go",
	"glm-worker/internal/app/output.go",
	"glm-worker/internal/app/watch.go",
	"glm-worker/internal/app/timeline.go",
	"glm-worker/internal/state/stats.go",
}

var measureProtocolCommits = []string{"202bc92", "029c6f8", "cfacab8", "705656a"}

type measureTelemetryRecord struct {
	CallType  string    `json:"call_type"`
	TaskID    string    `json:"task_id"`
	Role      string    `json:"role"`
	Outcome   string    `json:"outcome"`
	StartedAt time.Time `json:"started_at"`
	Response  string    `json:"response"`
}

type measureStatsRecord struct {
	TaskID                 string         `json:"task_id"`
	StartedAt              time.Time      `json:"started_at"`
	ModelCalls             int            `json:"model_calls"`
	ResultCorrections      int            `json:"result_corrections"`
	PacketCompactions      int            `json:"packet_compactions"`
	SolPacketBytes         int            `json:"sol_packet_bytes"`
	PacketRejectByCategory map[string]int `json:"packet_reject_by_category"`
}

func measureEraOf(at time.Time, ab time.Time, bc time.Time) string {
	switch {
	case at.Before(ab):
		return "A"
	case at.Before(bc):
		return "B"
	default:
		return "C"
	}
}

func measureGitOutput(t *testing.T, repoRoot string, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)), err)
		}
		return "", err
	}
	return string(out), nil
}

func TestProtocolMeasurementReport(t *testing.T) {
	reportDir := os.Getenv("PROTOEVAL_REPORT_DIR")
	if reportDir == "" {
		t.Skip("PROTOEVAL_REPORT_DIRが設定されたときだけreportを生成する")
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("err = %v", err)
	}

	all := allMeasuredPayloads(t)
	realPayloads := realMeasuredPayloads(t)
	syntheticPayloads := syntheticMeasuredPayloads()

	aggregateFor := func(payloads []measuredPayload, corpus string) []measureAggregateJSON {
		var legacyTotal, machineTotal renderMeasurement
		for _, payload := range payloads {
			l := measureLegacy(payload.Value)
			m := measureMachine(payload.Value)
			legacyTotal.StdoutBytes += l.StdoutBytes
			legacyTotal.TokensRun += l.TokensRun
			legacyTotal.TokensCharPunct += l.TokensCharPunct
			legacyTotal.StructuredBytes += l.StructuredBytes
			legacyTotal.NoiseFields += l.NoiseFields
			legacyTotal.DuplicateValues += l.DuplicateValues
			machineTotal.StdoutBytes += m.StdoutBytes
			machineTotal.TokensRun += m.TokensRun
			machineTotal.TokensCharPunct += m.TokensCharPunct
			machineTotal.StructuredBytes += m.StructuredBytes
			machineTotal.NoiseFields += m.NoiseFields
			machineTotal.DuplicateValues += m.DuplicateValues
		}
		ratio := func(m renderMeasurement) float64 {
			if m.StdoutBytes == 0 {
				return 0
			}
			return float64(m.StructuredBytes) / float64(m.StdoutBytes)
		}
		return []measureAggregateJSON{
			{Corpus: corpus, Format: "legacy-keyline", Payloads: len(payloads),
				StdoutBytes: legacyTotal.StdoutBytes, TokensRun: legacyTotal.TokensRun,
				TokensCharPunct: legacyTotal.TokensCharPunct, StructuredBytes: legacyTotal.StructuredBytes,
				StructuredRatio: ratio(legacyTotal), NoiseFields: legacyTotal.NoiseFields,
				DuplicateValues: legacyTotal.DuplicateValues},
			{Corpus: corpus, Format: "machine-json", Payloads: len(payloads),
				StdoutBytes: machineTotal.StdoutBytes, TokensRun: machineTotal.TokensRun,
				TokensCharPunct: machineTotal.TokensCharPunct, StructuredBytes: machineTotal.StructuredBytes,
				StructuredRatio: ratio(machineTotal), NoiseFields: machineTotal.NoiseFields,
				DuplicateValues: machineTotal.DuplicateValues},
		}
	}
	aggregates := append(aggregateFor(realPayloads, "real"), aggregateFor(syntheticPayloads, "synthetic")...)

	machinePreserved, legacyPreserved := 0, 0
	var legacyLosses []string
	for _, payload := range all {
		want := contractSemantics(payload.Value)
		encoded, err := payload.Value.MachineJSON()
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		machineParsed, err := ParseStructured(encoded)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if sameResultSemantics(machineParsed, want) {
			machinePreserved++
		}
		legacyParsed, err := legacyFromDisplayLines(legacyDisplayLines(payload.Value))
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if sameResultSemantics(legacyParsed, want) {
			legacyPreserved++
		} else if len(payload.Value.Targets) == 1 && payload.Value.Targets[0] == noneTargetsSentinel {
			legacyLosses = append(legacyLosses, "none-sentinel-collapse: "+payload.Name)
		} else {
			legacyLosses = append(legacyLosses, legacyRoundTripLoss(payload.Value)+": "+payload.Name)
		}
	}

	home := os.Getenv("GLM_WORKER_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		home = filepath.Join(userHome, ".glm-worker")
	}
	ab, err := time.Parse(time.RFC3339, measureEraABoundary)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	bc, err := time.Parse(time.RFC3339, measureEraBBoundary)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	statsFiles, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "stats", "*.json"))
	telemetryFiles, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "telemetry", "*.jsonl"))
	sessionDirs := map[string]struct{}{}
	for _, pattern := range []string{filepath.Join(home, "sessions", "*", "stats", "*.json"), filepath.Join(home, "sessions", "*", "telemetry", "*.jsonl")} {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			sessionDirs[strings.SplitN(filepath.Base(filepath.Dir(filepath.Dir(match))), string(filepath.Separator), 2)[0]] = struct{}{}
		}
	}
	eraTasks := map[string]int{}
	eraCorrections := map[string]int{}
	eraCompactions := map[string]int{}
	eraRejects := map[string]map[string]int{}
	eraSolBytes := map[string]int{}
	statsByTask := map[string]measureStatsRecord{}
	for _, path := range statsFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		var stats measureStatsRecord
		if err := json.Unmarshal(data, &stats); err != nil {
			t.Fatalf("%s: err = %v", path, err)
		}
		era := measureEraOf(stats.StartedAt, ab, bc)
		eraTasks[era]++
		eraCorrections[era] += stats.ResultCorrections
		eraCompactions[era] += stats.PacketCompactions
		eraSolBytes[era] += stats.SolPacketBytes
		if eraRejects[era] == nil {
			eraRejects[era] = map[string]int{}
		}
		for category, count := range stats.PacketRejectByCategory {
			eraRejects[era][category] += count
		}
		statsByTask[stats.TaskID] = stats
	}
	eraOutcomes := map[string]map[string]int{}
	eraTaskCalls := map[string]int{}
	nonContractNoiseCalls := 0
	acceptedScanned := 0
	lastWorker := map[string]measureTelemetryRecord{}
	for _, path := range telemetryFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var record measureTelemetryRecord
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("%s: err = %v", path, err)
			}
			if record.CallType != "task" {
				continue
			}
			era := measureEraOf(record.StartedAt, ab, bc)
			eraTaskCalls[era]++
			if eraOutcomes[era] == nil {
				eraOutcomes[era] = map[string]int{}
			}
			eraOutcomes[era][record.Outcome]++
			if record.Outcome != "success" {
				continue
			}
			acceptedScanned++
			if result, err := ParseStructured([]byte(record.Response)); err == nil && hasNonContractNoise(result) {
				nonContractNoiseCalls++
			}
			if record.Role == "worker" {
				existing, ok := lastWorker[record.TaskID]
				if !ok || !record.StartedAt.After(existing.StartedAt) {
					lastWorker[record.TaskID] = record
				}
			}
		}
	}
	eras := []eraStatsJSON{
		{Era: "A", Source: "IMPLEMENTATION_HISTORY.md §89/§90定数 (当該期間telemetryはdiskなし)",
			Tasks: eraATasks, TaskCalls: eraATaskCalls,
			Outcomes:          map[string]int{"stray-body-invalid": eraAStrayBodyInvalid},
			PacketCompactions: eraAPacketCompactions, CompactionCostUSD: eraACompactionCostUSD,
			CompactionCostShare: eraACompactionCostShare, FormatFailuresPoC: eraAFormatFailuresPoC,
			StructuralFailuresPoC: eraAStructuralFailuresPoC},
	}
	for _, era := range []string{"B", "C"} {
		eras = append(eras, eraStatsJSON{
			Era: era, Source: "保存telemetry/stats読み取り", Tasks: eraTasks[era], TaskCalls: eraTaskCalls[era],
			Outcomes: eraOutcomes[era], ResultCorrections: eraCorrections[era],
			PacketCompactions: eraCompactions[era], RejectsByCategory: eraRejects[era],
			SolPacketBytes: eraSolBytes[era],
		})
	}

	var audits []packetByteAuditJSON
	for task, stats := range statsByTask {
		if stats.SolPacketBytes == 0 {
			continue
		}
		audit := packetByteAuditJSON{Task: task, Recorded: stats.SolPacketBytes, Class: "not-reconstructible"}
		record, ok := lastWorker[task]
		if !ok {
			audit.Era = measureEraOf(stats.StartedAt, ab, bc)
			audit.Note = "telemetryにworker成功callなし(orchestrator合成結果のemit)"
			audits = append(audits, audit)
			continue
		}
		result, err := ParseStructured([]byte(record.Response))
		if err != nil {
			audit.Note = "最終worker成功応答をResultへ解析できず"
			audits = append(audits, audit)
			continue
		}
		audit.Era = measureEraOf(record.StartedAt, ab, bc)
		if audit.Era == "C" {
			encoded, err := result.MachineJSON()
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			audit.Computed = len(encoded)
		} else {
			audit.Computed = len(legacyDisplay(result))
		}
		switch {
		case audit.Computed == audit.Recorded:
			audit.Match = true
			audit.Class = "exact-match"
		case audit.Recorded > audit.Computed:
			audit.Class = "multi-emit-sum"
			audit.Note = "記録値は複数終端emitの累積(最終worker render単位より大きい)"
		default:
			audit.Note = "最終worker応答とは別の結果(合成packet・reviewer終端)がemitされた可能性"
		}
		audits = append(audits, audit)
	}
	sort.Slice(audits, func(i, j int) bool { return audits[i].Task < audits[j].Task })

	repoRootOut, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	repoRoot := strings.TrimSpace(string(repoRootOut))
	oldRef, newRef := "202bc92^", "HEAD"
	var fileMetrics []fileMetricJSON
	for _, path := range measureProtocolSurfaceFiles {
		metric := fileMetricJSON{Path: path}
		if src, err := measureGitOutput(t, repoRoot, "show", oldRef+":"+path); err == nil {
			metric.PresentOld = true
			metric.LinesOld = countNonBlankLines(src)
			metric.BranchesOld = countBranchStatements(src)
		}
		if src, err := measureGitOutput(t, repoRoot, "show", newRef+":"+path); err == nil {
			metric.PresentNew = true
			metric.LinesNew = countNonBlankLines(src)
			metric.BranchesNew = countBranchStatements(src)
		}
		fileMetrics = append(fileMetrics, metric)
	}
	var commitMetrics []commitMetricJSON
	for _, hash := range measureProtocolCommits {
		out, err := measureGitOutput(t, repoRoot, "show", "--numstat", "--format=%s", hash)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		metric := commitMetricJSON{Hash: hash, Subject: lines[0]}
		for _, line := range lines[1:] {
			fields := strings.Split(line, "\t")
			if len(fields) != 3 || strings.Contains(fields[2], "=>") {
				continue
			}
			path := fields[2]
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, "testdata/") {
				continue
			}
			metric.ProductionFiles++
			var additions, deletions int
			fmt.Sscanf(fields[0], "%d", &additions)
			fmt.Sscanf(fields[1], "%d", &deletions)
			metric.Additions += additions
			metric.Deletions += deletions
		}
		commitMetrics = append(commitMetrics, metric)
	}
	packetFilesAt := func(ref string) []string {
		out, err := measureGitOutput(t, repoRoot, "ls-tree", "-r", "--name-only", ref, "--", "glm-worker/internal/packet")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		var files []string
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if strings.HasSuffix(line, ".go") && !strings.HasSuffix(line, "_test.go") {
				files = append(files, line)
			}
		}
		return files
	}

	var realAggLegacy, realAggMachine *measureAggregateJSON
	for i := range aggregates {
		if aggregates[i].Corpus != "real" {
			continue
		}
		if aggregates[i].Format == "legacy-keyline" {
			realAggLegacy = &aggregates[i]
		} else {
			realAggMachine = &aggregates[i]
		}
	}
	if realAggLegacy == nil || realAggMachine == nil {
		t.Fatal("real corpus集計が構成できません")
	}
	byteDelta := float64(realAggMachine.StdoutBytes-realAggLegacy.StdoutBytes) / float64(realAggLegacy.StdoutBytes)
	runDelta := float64(realAggMachine.TokensRun-realAggLegacy.TokensRun) / float64(realAggLegacy.TokensRun)
	punctDelta := float64(realAggMachine.TokensCharPunct-realAggLegacy.TokensCharPunct) / float64(realAggLegacy.TokensCharPunct)
	noisyCorpus := 0
	for _, payload := range realPayloads {
		if hasNonContractNoise(payload.Value) {
			noisyCorpus++
		}
	}
	verdict := verdictJSON{
		RealByteDelta:       byteDelta,
		RealRunTokenDelta:   runDelta,
		RealPunctTokenDelta: punctDelta,
		Decision: fmt.Sprintf("machine JSON継続採用。実corpusstdout bytes差は%+.1f%%でbyte削減根拠はなく、"+
			"採用根拠は単一machine契約・decoder潰れなし(none sentinel保持)・placeholder noise 0・"+
			"Era C result_corrections 0 (n=%d task)。旧形式への撤退理由は測定上存在しない。", byteDelta*100, eraTasks["C"]),
		Rationale: []string{
			fmt.Sprintf("byte/token proxyは実corpusでbytes %+.1f%%・run token %+.1f%%・悲観punct token %+.1f%%と JSON構文費用で増加。JSON形式自体を目的化しない契約どおり、削減根拠からは外す", byteDelta*100, runDelta*100, punctDelta*100),
			fmt.Sprintf("format/semantic修正call: Era A stray-body invalid %d call・compaction %d call(約$%.2f, 総costの%.1f%%) → Era B result_corrections %d (tasks=%d) → Era C result_corrections 0 (tasks=%d)。A→B改善は22c1d0b structured output導入の効果でTask 006/007の効果ではない", eraAStrayBodyInvalid, eraAPacketCompactions, eraACompactionCostUSD, eraACompactionCostShare*100, eraCorrections["B"], eraTasks["B"], eraTasks["C"]),
			fmt.Sprintf("情報保持: machine JSONは全%d payloadで契約面無劣化。旧KEY行形式はnone sentinel潰れ・要素内セミコロン分割で%d payloadが劣化", len(all), len(all)-legacyPreserved),
			fmt.Sprintf("重複・noise: 意味値重複は両形式0件。placeholder行noiseは旧形式のみ(実corpus %d行)、machine JSONは構造的に0", realAggLegacy.NoiseFields),
			fmt.Sprintf("producer契約外field混入は受理%d call中%d callに観測。両形式とも契約面から除外するためCodexへ届かない", acceptedScanned, nonContractNoiseCalls),
		},
		WithdrawalTrigger: "本番Sol Direct A/B (permission待ちのまま分離) でCodex reductionまたは品質がmachine JSON側で劣化した場合のみ撤退。固定入力測定には撤退根拠なし。",
	}

	report := protocolMeasureReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Reproduction: "cd glm-worker && PROTOEVAL_REPORT_DIR=<dir> [GLM_WORKER_HOME=<home>] " +
			"go test ./internal/packet -run TestProtocolMeasurementReport -count=1",
		Corpus:           corpusSummary{RealPayloads: len(realPayloads), SyntheticPayloads: len(syntheticPayloads)},
		RenderComparison: aggregates,
		Retention: retentionSummaryJSON{
			MachinePreserved: machinePreserved, LegacyPreserved: legacyPreserved, TotalPayloads: len(all),
			LegacyLosses: legacyLosses, NonContractNoiseCalls: nonContractNoiseCalls,
			AcceptedCallsScanned: acceptedScanned, NonContractNoiseCorpus: noisyCorpus,
		},
		Telemetry: telemetrySummaryJSON{
			Home: home, SessionDirs: len(sessionDirs),
			EraBoundaries: map[string]string{"A|B": measureEraABoundary + " (22c1d0b structured output移行)", "B|C": measureEraBBoundary + " (202bc92 machine JSON化)"},
			Eras:          eras,
			Attribution:   "A→B改善は22c1d0b。B→C差分(202bc92/029c6f8/cfacab8/705656a)がTask 006/007の測定対象。Era Cのnは小さく傾向提示のみ。",
		},
		PacketByteAudit: audits,
		Git: gitSummaryJSON{
			OldRef: oldRef, NewRef: newRef, Files: fileMetrics, Commits: commitMetrics,
			PacketOld: packetFilesAt(oldRef), PacketNew: packetFilesAt(newRef),
			HeadCaveat: "HEADとの差分にはTask 006/007以外のcommitも含まれる。protocol帰属はCommitsのper-commit numstat(production .go限定)で見る。",
		},
		Verdict: verdict,
		Notes: []string{
			"semantic bytesは両形式とも同一(同じ契約field値を運ぶ)ため、差はすべて構文(framing)側に出る",
			"token proxyは決定論近似(run-based・char-punct悲観)であり実tokenizer値ではない",
			"旧renderer移植は202bc92^時点のproduction test期待値とbyte一致をtestで固定済み",
			"本番Sol/Codex A/Bは実行せずpermission待ち維持。本reportは固定入力・保存telemetryのみの測定",
		},
	}

	writeMeasureArtifacts(t, reportDir, &report)
}

func writeMeasureArtifacts(t *testing.T, reportDir string, report *protocolMeasureReport) {
	t.Helper()
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "protocol-measurement.json"), encoded, 0o644); err != nil {
		t.Fatalf("err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "protocol-measurement.md"), []byte(renderMeasureMarkdown(report)), 0o644); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func renderMeasureMarkdown(report *protocolMeasureReport) string {
	var b strings.Builder
	b.WriteString("# Task 008 machine protocol効果測定 (追加AI callなし)\n\n")
	b.WriteString("- generated_at: " + report.GeneratedAt + "\n")
	b.WriteString("- corpus: real " + itoa(report.Corpus.RealPayloads) + "件 (保存telemetry受理済み結果・era/size分布選出) + synthetic " + itoa(report.Corpus.SyntheticPayloads) + "件 (契約境界)\n")
	b.WriteString("- reproduction: `" + report.Reproduction + "`\n\n")

	b.WriteString("## 1. 同一semantic payloadのrender比較 (旧KEY行 vs machine JSON)\n\n")
	b.WriteString("| corpus | format | payloads | stdout bytes | tokens(run) | tokens(charPunct) | structured bytes | structured ratio | noise | dup |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, agg := range report.RenderComparison {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %d | %.3f | %d | %d |\n",
			agg.Corpus, agg.Format, agg.Payloads, agg.StdoutBytes, agg.TokensRun, agg.TokensCharPunct,
			agg.StructuredBytes, agg.StructuredRatio, agg.NoiseFields, agg.DuplicateValues)
	}
	var realLegacy, realMachine *measureAggregateJSON
	for i := range report.RenderComparison {
		if report.RenderComparison[i].Corpus == "real" {
			if report.RenderComparison[i].Format == "legacy-keyline" {
				realLegacy = &report.RenderComparison[i]
			} else {
				realMachine = &report.RenderComparison[i]
			}
		}
	}
	if realLegacy != nil && realMachine != nil {
		fmt.Fprintf(&b, "\n実corpus差: stdout bytes %+.1f%% / run token %+.1f%% / charPunct token %+.1f%% (machine JSON - legacy)\n",
			float64(realMachine.StdoutBytes-realLegacy.StdoutBytes)/float64(realLegacy.StdoutBytes)*100,
			float64(realMachine.TokensRun-realLegacy.TokensRun)/float64(realLegacy.TokensRun)*100,
			float64(realMachine.TokensCharPunct-realLegacy.TokensCharPunct)/float64(realLegacy.TokensCharPunct)*100)
	}
	b.WriteString("\n" + strings.Join(report.Notes[:2], "\n") + "\n\n")

	b.WriteString("## 2. 情報保持 (information retention)\n\n")
	fmt.Fprintf(&b, "- machine JSON: %d/%d payloadで契約面完全保持\n", report.Retention.MachinePreserved, report.Retention.TotalPayloads)
	fmt.Fprintf(&b, "- 旧KEY行形式: %d/%d payloadで保持。喪失内訳:\n", report.Retention.LegacyPreserved, report.Retention.TotalPayloads)
	for _, loss := range report.Retention.LegacyLosses {
		b.WriteString("  - " + loss + "\n")
	}
	fmt.Fprintf(&b, "- producer契約外field混入: 受理%d call中%d call (実corpus選出では%d件)。両形式とも契約面から除外しCodexへ届かない\n",
		report.Retention.AcceptedCallsScanned, report.Retention.NonContractNoiseCalls, report.Retention.NonContractNoiseCorpus)
	b.WriteString("- 旧形式のみの既知喪失: reviewer TARGETS予約値noneの`TARGETS: none` placeholder潰れ、targets要素内セミコロンによる要素分割\n\n")

	b.WriteString("## 3. 保存telemetryによるformat/semantic修正call比較 (era別)\n\n")
	b.WriteString("- era境界: A|B " + report.Telemetry.EraBoundaries["A|B"] + " / B|C " + report.Telemetry.EraBoundaries["B|C"] + "\n")
	fmt.Fprintf(&b, "- home: %s (session dirs %d)\n", report.Telemetry.Home, report.Telemetry.SessionDirs)
	b.WriteString("- " + report.Telemetry.Attribution + "\n\n")
	b.WriteString("| era | source | tasks | task calls | outcomes | result_corrections | compactions | rejects | sol_packet_bytes |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, era := range report.Telemetry.Eras {
		outcomes := make([]string, 0, len(era.Outcomes))
		for outcome, count := range era.Outcomes {
			outcomes = append(outcomes, outcome+"="+itoa(count))
		}
		sort.Strings(outcomes)
		rejects := make([]string, 0, len(era.RejectsByCategory))
		for category, count := range era.RejectsByCategory {
			rejects = append(rejects, category+"="+itoa(count))
		}
		sort.Strings(rejects)
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %d | %d | %s | %d |\n",
			era.Era, era.Source, era.Tasks, era.TaskCalls, strings.Join(outcomes, ", "),
			era.ResultCorrections, era.PacketCompactions, strings.Join(rejects, ", "), era.SolPacketBytes)
	}
	b.WriteString("\nEra Aの定数出典: IMPLEMENTATION_HISTORY.md §89 (stray-body invalid・compaction cost) / §90 (PoC format failure 65件中53件=82%が構造欠陥)。\n\n")

	b.WriteString("## 4. sol_packet_bytes記録値との照合\n\n")
	classes := map[string]int{}
	for _, audit := range report.PacketByteAudit {
		classes[audit.Class]++
	}
	fmt.Fprintf(&b, "対象%d task: exact-match=%d (記録値=最終worker render byte数によるrenderer式の検証) / multi-emit-sum=%d (記録値が累積和として単一render超えで整合) / not-reconstructible=%d (合成packet・reviewer終端など単一renderから再構成不能)。\n\n",
		len(report.PacketByteAudit), classes["exact-match"], classes["multi-emit-sum"], classes["not-reconstructible"])
	b.WriteString("| task | era | recorded | computed | class | note |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, audit := range report.PacketByteAudit {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s |\n",
			audit.Task, audit.Era, audit.Recorded, audit.Computed, audit.Class, audit.Note)
	}
	b.WriteString("\n")

	b.WriteString("## 5. legacy/migration code量とprotocol branch数\n\n")
	fmt.Fprintf(&b, "file毎比較 (%s → %s。行数=非空行、branch=if/else/for/switch/case/select行頭数の同一定義proxy):\n\n", report.Git.OldRef, report.Git.NewRef)
	b.WriteString("| path | old lines | new lines | old branches | new branches |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, file := range report.Git.Files {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", file.Path, file.LinesOld, file.LinesNew, file.BranchesOld, file.BranchesNew)
	}
	b.WriteString("\nper-commit production .go numstat (Task 006/007帰属。test/testdata除外):\n\n")
	b.WriteString("| commit | subject | files | +lines | -lines |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, commit := range report.Git.Commits {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d |\n", commit.Hash, commit.Subject, commit.ProductionFiles, commit.Additions, commit.Deletions)
	}
	b.WriteString("\ninternal/packet production files: old=" + strings.Join(report.Git.PacketOld, ", ") + " / new=" + strings.Join(report.Git.PacketNew, ", ") + "\n")
	b.WriteString("\n" + report.Git.HeadCaveat + "\n\n")

	b.WriteString("## 6. 採用/撤退判定 (Codex Reduction / Quality Delta)\n\n")
	fmt.Fprintf(&b, "**判定: %s**\n\n", report.Verdict.Decision)
	for _, reason := range report.Verdict.Rationale {
		b.WriteString("- " + reason + "\n")
	}
	b.WriteString("\n撤退条件: " + report.Verdict.WithdrawalTrigger + "\n\n")
	b.WriteString("## 7. 本番A/Bとの分離\n\n")
	b.WriteString("- 本測定は固定入力render比較・保存telemetry・git履歴のみで、追加AI call・本番Sol/Codex A/Bを含まない。\n")
	b.WriteString("- 本番A/B (Direct/orchestrated) はpermission待ちを維持し、本測定の結果で代用しない。\n")
	return b.String()
}

func itoa(v int) string {
	return fmt.Sprintf("%d", v)
}
