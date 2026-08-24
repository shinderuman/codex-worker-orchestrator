package app

import (
	"bytes"
	"io"
	"os"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// defaultWatchFollowIntervalは--watchのtail polling間隔。保存済みlogのlocal監視だけで
// provider/workerへの問い合わせは行わないため、この間隔での外部影響はない。
const defaultWatchFollowInterval = 500 * time.Millisecond

// watchOptionsは--watch表示の実行要素。verbose以外は従来動作と同じ既定値を使う。
type watchOptions struct {
	verbose        bool
	followInterval time.Duration
	statusInterval time.Duration
	changeInterval time.Duration
	now            func() time.Time
	stop           <-chan struct{}
}

func defaultWatchOptions(verbose bool) watchOptions {
	return watchOptions{
		verbose:        verbose,
		followInterval: defaultWatchFollowInterval,
		statusInterval: defaultWatchStatusInterval,
		changeInterval: defaultWatchChangeInterval,
		now:            time.Now,
	}
}

// watchStartEventはfollow開始時のcontrol event。event_log_statusはfollowing(既存行を
// 表示した後追記をfollowする)・empty(logがまだ無い)・none(現在taskがない)。
type watchStartEvent struct {
	Type           string  `json:"type"`
	TaskID         *string `json:"task_id"`
	EventLog       *string `json:"event_log"`
	EventLogStatus string  `json:"event_log_status"`
}

// watchLogStatusEventはfollow中のevent log状態変化。statusはremoved。
type watchLogStatusEvent struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

// watchExitEventはfollow終端のcontrol event。statusはfollow対象taskのtask.status値、
// 別taskへの切替時はtask-switchedでnew_task_idに切替先が載る。
type watchExitEvent struct {
	Type      string  `json:"type"`
	TaskID    string  `json:"task_id"`
	Status    string  `json:"status"`
	NewTaskID *string `json:"new_task_id,omitempty"`
}

// printWatchは現在taskの受動event logを読み取り専用でJSONL streamへ流す。state書換・
// repo lock・AI call・provider/workerへの問い合わせを行わない。event recordは保存済み
// JSONL行をそのままpassthroughし、watch固有の状態遷移だけ型付きcontrol eventへ出す。
// event logがまだ無いtaskはその旨をstatusへ出して即座に終了し、存在すれば既存行を
// 流した後追記をfollowする。followはfollow対象taskのauthoritative task.statusがactiveを
// 離れた時点・別taskへの切替時に残eventを流してwatch_exit eventを出し終了する。
// event log消失時はremoved status eventのみで終了する。verbose指定時はlive tool状態を
// 型付きlive eventへ出す。stopはtest用の打ち切り信号で、nilでも上記終端で必ず終了する。
func printWatch(st *state.StateStore, stdout io.Writer, opts watchOptions) error {
	taskID := st.ReadOr("task.id", "")
	if taskID == "" {
		return writeWatchEvent(stdout, watchStartEvent{
			Type: "watch_start", TaskID: nil, EventLog: nil, EventLogStatus: "none",
		})
	}
	path := st.TaskEventLogPath(taskID)
	file, err := os.Open(path)
	if err != nil {
		return writeWatchEvent(stdout, watchStartEvent{
			Type: "watch_start", TaskID: &taskID, EventLog: &path, EventLogStatus: "empty",
		})
	}
	defer file.Close()
	if err := writeWatchEvent(stdout, watchStartEvent{
		Type: "watch_start", TaskID: &taskID, EventLog: &path, EventLogStatus: "following",
	}); err != nil {
		return err
	}
	return watchTaskEvents(st, taskID, file, path, stdout, opts)
}

// watchTerminalはfollow対象taskの終端をauthoritative stateから判定する。task.idが別taskへ
// 切替わった場合は切替を、task.statusがactiveを離れた場合はそのstatusを終了理由へ出す。
// task.id読取り失敗・空は切替と判断しない(StartNewTaskの書換途中windowで誤終了させない)。
func watchTerminal(st *state.StateStore, taskID string) (watchExitEvent, bool) {
	current := st.ReadOr("task.id", "")
	if current != "" && current != taskID {
		return watchExitEvent{
			Type: "watch_exit", TaskID: taskID, Status: "task-switched", NewTaskID: &current,
		}, true
	}
	if status := st.TaskStatus(); status != state.TaskStatusActive {
		return watchExitEvent{Type: "watch_exit", TaskID: taskID, Status: string(status)}, true
	}
	return watchExitEvent{}, false
}

// watchTaskEventsはevent logの追記を流し、follow対象taskの終端で復帰する。event logへの
// 書込みは必ずtask.statusがactiveの間に行われ、non-activeへの遷移は当該呼出の最終event
// 追記より後かつ以後の追記より前に行われるため、stateを読んだ後にdrainすれば終端eventを
// 取りこぼさない。この順序をsleep・retry・出力停止観測で代替しない。verbose時はdrainした
// recordでtool状態を組み立て、live eventを間欠的に出す。
func watchTaskEvents(st *state.StateStore, taskID string, file *os.File, path string, stdout io.Writer, opts watchOptions) error {
	tracker := newWatchToolTracker()
	pending, err := drainTaskEvents(file, stdout, nil, tracker.observe)
	if err != nil {
		return err
	}
	status := watchLiveStatus{st: st, taskID: taskID, stdout: stdout, tracker: tracker, opts: opts}
	if err := status.refresh(true); err != nil {
		return err
	}
	if exitEvent, terminal := watchTerminal(st, taskID); terminal {
		return writeWatchEvent(stdout, exitEvent)
	}
	for {
		select {
		case <-opts.stop:
			return nil
		case <-time.After(opts.followInterval):
		}
		if _, err := os.Stat(path); err != nil {
			return writeWatchEvent(stdout, watchLogStatusEvent{Type: "event_log_status", Status: "removed"})
		}
		exitEvent, terminal := watchTerminal(st, taskID)
		pending, err = drainTaskEvents(file, stdout, pending, tracker.observe)
		if err != nil {
			return err
		}
		if err := status.refresh(false); err != nil {
			return err
		}
		if terminal {
			return writeWatchEvent(stdout, exitEvent)
		}
	}
}

func writeWatchEvent(w io.Writer, event any) error {
	line, err := marshalEventLine(event)
	if err != nil {
		return err
	}
	_, err = w.Write(line)
	return err
}

// drainTaskEventsは読み取り位置以降の新規bytesを行ごとに流す。改行で終端しない
// 末尾部分は次回へ持ち越し、書き込み途中の行を破損表示にしない。onRecordは流せた
// recordをverbose tool状態の組立へ渡す。
func drainTaskEvents(file *os.File, stdout io.Writer, pending []byte, onRecord func(state.TaskEventRecord)) ([]byte, error) {
	buffer := make([]byte, 32*1024)
	for {
		read, err := file.Read(buffer)
		if read > 0 {
			pending = append(pending, buffer[:read]...)
			for {
				index := bytes.IndexByte(pending, '\n')
				if index < 0 {
					break
				}
				if err := emitTaskEventLine(pending[:index], stdout, onRecord); err != nil {
					return pending, err
				}
				pending = pending[index+1:]
			}
		}
		if err == io.EOF {
			return pending, nil
		}
		if err != nil {
			return pending, err
		}
	}
}

// emitTaskEventLineはevent logの1行をそのままJSONLへpassthroughする。保存済み行が
// parseできないときだけ、その旨の型付きeventへ置き換える。
func emitTaskEventLine(line []byte, stdout io.Writer, onRecord func(state.TaskEventRecord)) error {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}
	record, err := state.ParseTaskEventLine(trimmed)
	if err != nil {
		return writeWatchEvent(stdout, eventLogSkippedLine{Type: "event_skipped", Error: err.Error()})
	}
	if onRecord != nil {
		onRecord(record)
	}
	_, writeErr := stdout.Write(append(bytes.Clone(trimmed), '\n'))
	return writeErr
}
