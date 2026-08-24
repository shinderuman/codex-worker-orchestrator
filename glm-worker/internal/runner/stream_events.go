package runner

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// plainSignalMaxBytesはJSON eventとして解釈できないplain stdout行を分類のため
// メモリ内へ保持する上限。旧raw fallbackと違い全量は持たず、末尾側だけを残す。
const plainSignalMaxBytes = 64 * 1024

// maxStreamEventRecordsPerCallは1 callあたりのevent log追記上限。thinking_tokens等の
// 進捗event抑止後の通常呼出は数百件程度であり、未知のevent種別stormでlogだけが
// 肥大化する経路を塞ぐ安全弁。上限到達時はwarning 1回だけ出し、result event捕捉は維持する。
const maxStreamEventRecordsPerCall = 50000

// liveStatusWriteIntervalはlive snapshotの書込み間隔下限。tool状態が変わらない間は
// last_event_at更新だけの書込みを抑制し、thinking_tokensのような高頻度eventで書込み数が
// event数に比例して増えないようにする。
const liveStatusWriteInterval = time.Second

// liveCommandMaxBytes/livePurposeMaxBytesはlive snapshotへ保存するtool入力本文の上限。
// watch表示でのtruncateとは別のsnapshot file size上限で、event logはこの本文を一切保存しない。
const (
	liveCommandMaxBytes = 2048
	livePurposeMaxBytes = 512
)

// streamEventIngesterはClaude CLIのstream-json出力を行単位で受け取り、各eventを
// content本文を含まないmetadataだけへ縮約してtask単位event logへbest-effort追記する。
// streamの非result event本文はどこへも書き出さず、既存result解析に必要な最終
// type=result event行だけをboundedな内部表現へ保持する。JSON eventとして解釈できない
// plain stdout行はraw保存の代わりに分類用signal bufferへ末尾boundedで保持する。
// stdout経路の一部としてchild processへ組み込むためWriteは決してerrorを返さず、
// 追記失敗時はこのcallのevent記録を無効化して本体実行へ影響させない。
type streamEventIngester struct {
	state      *state.StateStore
	base       state.TaskEventRecord
	seq        int
	pending    []byte
	closed     bool
	resultLine []byte
	plain      []byte
	tools      map[string]toolUseObservation
	now        func() time.Time
	// live snapshotは--watch --verbose表示のための瞬間状態。event logのmachine-only
	// 縮約方針を変えずに、tool入力の表示要素だけを別fileへ出す。
	liveLastEventAt         time.Time
	liveLastModelActivityAt time.Time
	liveLastWrite           time.Time
	liveBroken              bool
	// limitStopは受信済みstdout行(JSON event・plain行)からZ.ai 5h上限のexact signalを
	// 観測した時点でchildを終了させる早期停止hook。nilのとき早期停止しない。
	limitStop *zaiLimitStopper
}

// toolUseObservationはtool_use blockを出したeventの観測時刻とtool名。
// 対応するtool_resultの出現時刻との差だけが測定済みdurationとして記録される。
// command・purpose等のtool入力詳細はevent logへ保存できないためここだけが保持する。
type toolUseObservation struct {
	toolID     string
	timestamp  time.Time
	name       string
	command    string
	purpose    string
	background bool
	waitTaskID string
}

func newStreamEventIngester(
	st *state.StateStore,
	taskID string,
	callID string,
	role state.SessionRole,
	phase string,
	model string,
	sessionID string,
	resumed bool,
) *streamEventIngester {
	return &streamEventIngester{
		state: st,
		base: state.TaskEventRecord{
			TaskID:     taskID,
			CallID:     callID,
			SessionID:  sessionID,
			Role:       string(role),
			Phase:      phase,
			ModelAlias: model,
			Resumed:    resumed,
		},
		tools: make(map[string]toolUseObservation),
		now:   time.Now,
	}
}

// resultは保持した最終result event行を返す。未観測のときはfalse。
func (g *streamEventIngester) result() ([]byte, bool) {
	if len(g.resultLine) == 0 {
		return nil, false
	}
	return g.resultLine, true
}

func (g *streamEventIngester) Write(p []byte) (int, error) {
	g.pending = append(g.pending, p...)
	for {
		index := bytes.IndexByte(g.pending, '\n')
		if index < 0 {
			break
		}
		line := g.pending[:index]
		g.pending = g.pending[index+1:]
		g.ingestLine(line)
	}
	return len(p), nil
}

// flushはprocess終了後に改行で終端しなかった最終行を取り込む。
func (g *streamEventIngester) flush() {
	if len(bytes.TrimSpace(g.pending)) > 0 {
		g.ingestLine(g.pending)
	}
	g.pending = nil
}

func (g *streamEventIngester) ingestLine(line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	g.observeZaiLimitSignal(line)
	if streamResultEvent(line) {
		g.resultLine = append(g.resultLine[:0], line...)
	}
	g.capturePlainSignal(line)
	if g.closed {
		return
	}
	if g.seq >= maxStreamEventRecordsPerCall {
		state.WarnTaskEventCap(maxStreamEventRecordsPerCall)
		g.closed = true
		return
	}
	record := reduceStreamEvent(line, g.base, g.seq+1, g.now().UTC())
	modelActivity := state.IsModelActivityEvent(record)
	if progressOnlyStreamEvent(record) {
		// 抑止eventもmodel活動の観測なのでidle計算の基準時刻へは反映する。
		g.noteLiveActivity(record.Timestamp, modelActivity, false)
		return
	}
	toolsChanged := g.observeToolBlocks(&record, toolUseInputs(line))
	if err := g.state.AppendTaskEvent(record); err != nil {
		state.WarnTaskEventSkip("追記", err)
		g.closed = true
		return
	}
	g.seq++
	g.noteLiveActivity(record.Timestamp, modelActivity, toolsChanged)
}

// progressOnlyStreamEventは内容を持たず到着間隔だけが情報の進捗eventを記録対象から
// 除外する。実測ではsystem/thinking_tokensが1 callで数千行を占め、log容量の大半を
// 使っていた。thinking token量そのものはassistant/result eventのusageへ残る。
func progressOnlyStreamEvent(record state.TaskEventRecord) bool {
	return record.Kind == "system" && record.Subtype == "thinking_tokens"
}

// observeToolBlocksは同一call内でtool_useのidとtool_resultのtool_use_idを正確に対応付け、
// 両端のevent観測時刻の差だけを結果blockへ記録する。片方しか観測できない組はduration
// を書かない(未測定を推測で補わない)。対応付けできた結果blockには元tool_useのtool名を
// 写し、tool種別ごとの時間を読み取れるようにする。あわせてlive snapshot用のpending観測を
// 更新し、pending集合が変化したかどうかを返す。result eventでcallは終端しているため、
// 対応するtool_resultが来なかったpending toolはもう実行していないとして清除する。
func (g *streamEventIngester) observeToolBlocks(record *state.TaskEventRecord, inputs map[string]json.RawMessage) bool {
	changed := false
	for i := range record.Blocks {
		block := &record.Blocks[i]
		switch block.Type {
		case "tool_use":
			if block.ToolID == "" {
				continue
			}
			observation := toolUseObservation{toolID: block.ToolID, timestamp: record.Timestamp, name: block.Name}
			if input, ok := inputs[block.ToolID]; ok {
				detail := extractLiveToolDetail(input)
				observation.command = detail.command
				observation.purpose = detail.purpose
				observation.background = detail.background
				observation.waitTaskID = detail.waitTaskID
			}
			g.tools[block.ToolID] = observation
			changed = true
		case "tool_result":
			if block.ToolID == "" {
				continue
			}
			observed, ok := g.tools[block.ToolID]
			if !ok {
				continue
			}
			block.DurationMS = record.Timestamp.Sub(observed.timestamp).Milliseconds()
			if block.Name == "" {
				block.Name = observed.name
			}
			delete(g.tools, block.ToolID)
			changed = true
		}
	}
	if record.Kind == "result" && len(g.tools) > 0 {
		g.tools = make(map[string]toolUseObservation)
		changed = true
	}
	return changed
}

// noteLiveActivityはlive snapshotの最終event観測時刻とmodel activity専用観測時刻を進め、
// tool状態変化または書込み間隔経過時にsnapshotを書き替える。model activity判定は
// state.IsModelActivityEventの共有契約に従い、tool_progress等の非model eventでは
// MODEL_IDLE基準時刻を進めない。判定もsnapshot時刻もevent観測時刻だけを使い、ここで
// clockを追加呼び出しない(観測時刻のtest決定性を保つ)。書込み失敗はwatch表示の詳細行欠落
// だけで済むためwarning 1回だけ出し、このcallのlive snapshot更新を止める。
func (g *streamEventIngester) noteLiveActivity(at time.Time, modelActivity bool, toolsChanged bool) {
	if at.After(g.liveLastEventAt) {
		g.liveLastEventAt = at
	}
	if modelActivity && at.After(g.liveLastModelActivityAt) {
		g.liveLastModelActivityAt = at
	}
	if g.liveBroken || g.closed {
		return
	}
	if !toolsChanged && at.Sub(g.liveLastWrite) < liveStatusWriteInterval {
		return
	}
	status := state.TaskLiveStatus{
		UpdatedAt:           at.UTC(),
		LastEventAt:         g.liveLastEventAt.UTC(),
		LastModelActivityAt: g.liveLastModelActivityAt.UTC(),
		Tools:               liveToolsSnapshot(g.tools),
	}
	if err := g.state.WriteTaskLiveStatus(g.base.TaskID, status); err != nil {
		state.WarnTaskLiveSkip(err)
		g.liveBroken = true
		return
	}
	g.liveLastWrite = at
}

// liveToolsSnapshotはpending観測を開始時刻順へ並べてlive snapshotのtool詳細へ写す。
func liveToolsSnapshot(tools map[string]toolUseObservation) []state.TaskLiveTool {
	if len(tools) == 0 {
		return nil
	}
	observations := make([]toolUseObservation, 0, len(tools))
	for _, observation := range tools {
		observations = append(observations, observation)
	}
	sort.Slice(observations, func(i, j int) bool { return observations[i].timestamp.Before(observations[j].timestamp) })
	result := make([]state.TaskLiveTool, 0, len(observations))
	for _, observation := range observations {
		result = append(result, state.TaskLiveTool{
			ToolID:     observation.toolID,
			Command:    observation.command,
			Purpose:    observation.purpose,
			Background: observation.background,
			WaitTaskID: observation.waitTaskID,
		})
	}
	return result
}

// liveToolDetailはlive snapshotへ出すtool入力の表示要素。tool種別ごとにfield名が違うため
// 既知fieldだけをbest-effortで取り、未知の入力形状は空のままにする。
type liveToolDetail struct {
	command    string
	purpose    string
	background bool
	waitTaskID string
}

// extractLiveToolDetailはtool_useのinputからBash command・description/purpose・
// run_in_background・待機対象のbackground task識別子を取り出す。
func extractLiveToolDetail(input json.RawMessage) liveToolDetail {
	if len(input) == 0 {
		return liveToolDetail{}
	}
	var parsed struct {
		Command         string `json:"command"`
		Description     string `json:"description"`
		RunInBackground bool   `json:"run_in_background"`
		TaskID          string `json:"task_id"`
		BashID          string `json:"bash_id"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return liveToolDetail{}
	}
	detail := liveToolDetail{
		command:    boundLiveText(parsed.Command, liveCommandMaxBytes),
		purpose:    boundLiveText(parsed.Description, livePurposeMaxBytes),
		background: parsed.RunInBackground,
		waitTaskID: parsed.TaskID,
	}
	if detail.waitTaskID == "" {
		detail.waitTaskID = parsed.BashID
	}
	return detail
}

// boundLiveTextはlive snapshot本文を上限bytesへUTF-8境界で切詰める。watch表示でのtruncate
// とは別に、snapshot file sizeを固定上限へ束ねる境界。
func boundLiveText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// toolUseInputsはevent行からtool_use blockのid→inputを取り出す。live表示詳細の抽出だけに
// 使い、event record側へは何も加えない。
func toolUseInputs(line []byte) map[string]json.RawMessage {
	var event struct {
		Message struct {
			Content []struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return nil
	}
	var inputs map[string]json.RawMessage
	for _, block := range event.Message.Content {
		if block.Type != "tool_use" || block.ID == "" {
			continue
		}
		if inputs == nil {
			inputs = make(map[string]json.RawMessage)
		}
		inputs[block.ID] = block.Input
	}
	return inputs
}

// observeZaiLimitSignalは受信済みstdout行をJSON eventかplain行かを問わず既存5h
// classifierへ通し、exact signalならchildを終了させる。JSON event行はplain分類bufferへ
// 入らないため、kill判断に使った観測行だけをbufferへ残し、終端分類が停止判断と同じ証拠を
// 見てRATE_LIMITEDへ至るようにする。bufferへ載るのは検出済みの1行だけで、それ以外の
// JSON event本文は従来どおりどこにも保存しない。
func (g *streamEventIngester) observeZaiLimitSignal(line []byte) {
	if g.limitStop == nil || !g.limitStop.observeSignal(string(line)) {
		return
	}
	var event streamEvent
	if json.Unmarshal(line, &event) == nil {
		g.appendPlainSignal(line)
	}
}

// capturePlainSignalはJSON eventとして解釈できないplain stdout行だけを分類用bufferへ
// 追記する。assistant/thinking/tool等のJSON content内の数値・文字列をprovider信号へ
// 誤認しないための境界で、有効なJSON object行はここへ入らない。bufferは生本文を
// 保持するが分類後破棄され、外部へ出るのは分類Kind・Detailのような構造値だけ。
func (g *streamEventIngester) capturePlainSignal(line []byte) {
	var event streamEvent
	if json.Unmarshal(line, &event) == nil {
		return
	}
	g.appendPlainSignal(line)
}

// appendPlainSignalは分類用plain bufferへ行を末尾boundedで追記する。
func (g *streamEventIngester) appendPlainSignal(line []byte) {
	g.plain = append(g.plain, line...)
	g.plain = append(g.plain, '\n')
	if excess := len(g.plain) - plainSignalMaxBytes; excess > 0 {
		g.plain = g.plain[excess:]
	}
}

// plainSignalは分類対象のplain stdout末尾を返す。保持していないときは空文字。
func (g *streamEventIngester) plainSignal() string {
	return string(g.plain)
}

// streamResultEventは行がtype=result eventかどうかをcontentを読まずに判定する。
func streamResultEvent(line []byte) bool {
	var head struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(line, &head) == nil && head.Type == "result"
}

// streamEventはstream-json 1行から観測に必要な非content fieldだけを取り出すための
// 受け皿。content本文はjson.RawMessageのままblock単位のbyte数へ縮約され、recordへは
// 入らない。未知のevent種別もkind名だけ残し schema driftを観測可能にする。
type streamEvent struct {
	Type          string                `json:"type"`
	Subtype       string                `json:"subtype"`
	Model         string                `json:"model"`
	Message       json.RawMessage       `json:"message"`
	IsError       bool                  `json:"is_error"`
	DurationMS    int64                 `json:"duration_ms"`
	DurationAPIMS int64                 `json:"duration_api_ms"`
	NumTurns      int                   `json:"num_turns"`
	TotalCostUSD  float64               `json:"total_cost_usd"`
	Usage         *state.TaskEventUsage `json:"usage"`
}

type streamMessage struct {
	Model   string                `json:"model"`
	Usage   *state.TaskEventUsage `json:"usage"`
	Content []json.RawMessage     `json:"content"`
}

type streamBlock struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	ID        string `json:"id"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
}

func reduceStreamEvent(line []byte, base state.TaskEventRecord, seq int, observedAt time.Time) state.TaskEventRecord {
	record := base
	record.Version = 0
	record.Seq = seq
	record.Timestamp = observedAt
	var event streamEvent
	if err := json.Unmarshal(line, &event); err != nil {
		record.Kind = "unknown"
		return record
	}
	record.Kind = event.Type
	record.Subtype = event.Subtype
	switch event.Type {
	case "system":
		record.MessageModel = event.Model
	case "assistant", "user":
		var message streamMessage
		if err := json.Unmarshal(event.Message, &message); err == nil {
			record.MessageModel = message.Model
			record.Usage = message.Usage
			record.Blocks = reduceStreamBlocks(message.Content)
		}
	case "result":
		record.IsError = event.IsError
		record.DurationMS = event.DurationMS
		record.DurationAPIMS = event.DurationAPIMS
		record.NumTurns = event.NumTurns
		record.TotalCostUSD = event.TotalCostUSD
		record.Usage = event.Usage
	}
	return record
}

func reduceStreamBlocks(content []json.RawMessage) []state.TaskBlockSummary {
	blocks := make([]state.TaskBlockSummary, 0, len(content))
	for _, raw := range content {
		var block streamBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			blocks = append(blocks, state.TaskBlockSummary{Type: "unknown", Bytes: len(raw)})
			continue
		}
		blocks = append(blocks, state.TaskBlockSummary{
			Type:    block.Type,
			Name:    block.Name,
			ToolID:  blockToolID(block),
			Bytes:   len(raw),
			IsError: block.IsError,
		})
	}
	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

// blockToolIDはblock種別どちらかのID値を対応付け用の単一fieldへ正規化する。
// tool_useはidを、tool_resultはtool_use_idを持ち、同じtool呼出しを指す。
func blockToolID(block streamBlock) string {
	if block.ToolUseID != "" {
		return block.ToolUseID
	}
	return block.ID
}
