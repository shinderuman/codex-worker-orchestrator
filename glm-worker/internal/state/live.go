package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WarnTaskLiveSkipはlive snapshot書込みをbest-effortで諦めた旨を観測用warningとして出す。
// 欠けるのはwatch表示の詳細行だけで、task本体・event logへ影響しない。
func WarnTaskLiveSkip(err error) {
	fmt.Fprintf(statsWarnOut, "WARNING: live status snapshotの書込みに失敗したため以後のlive表示更新をskipします（task本体へ影響しません）: %v\n", err)
}

// TaskLiveToolは現在実行中のtool 1件のlive観測詳細。event logのmachine-only縮約方針に
// よりtask event recordへ保存できないtool入力の表示要素(command・purpose・background待ち)
// だけを持ち、tool_idでevent log側のtool_use blockと対応付ける。
type TaskLiveTool struct {
	ToolID     string `json:"tool_id"`
	Command    string `json:"command,omitempty"`
	Purpose    string `json:"purpose,omitempty"`
	Background bool   `json:"background,omitempty"`
	WaitTaskID string `json:"wait_task_id,omitempty"`
}

// TaskLiveStatusは`--watch --verbose`表示のための瞬間snapshot。実行中のrunner processだけが
// 上書きし、追記・履歴化は行わない。本文は書込み時の上限boundsで切詰め、retention整理では
// 同一taskのevent logと一緒に削除される。watchは欠損・破損を表示要素の欠落として扱う。
type TaskLiveStatus struct {
	UpdatedAt   time.Time `json:"updated_at"`
	LastEventAt time.Time `json:"last_event_at"`
	// LastModelActivityAtはIsModelActivityEventが受理するeventだけの最終観測時刻。
	// LastEventAtがtool_progress等の非model eventでも進む汎用時刻であるのに対し、こちらは
	// MODEL_IDLE基準専用で、event logへ保存されないsystem/thinking_tokensの観測もここへ
	// 残る。新field導入前の旧snapshotでは欠損し、zeroとして読める。
	LastModelActivityAt time.Time      `json:"last_model_activity_at"`
	Tools               []TaskLiveTool `json:"tools,omitempty"`
}

// IsModelActivityEventはrecordがmodel activity観測かを判定する。live snapshotの
// LastModelActivityAtを書くrunner(producer)とMODEL_IDLEを計算するwatch(consumer)が
// 同一の受理集合を共有するための単一契約で、assistant側のthinking・text・tool_use blockと
// event logへ保存しないsystem/thinking_tokensだけを受理する。system/tool_progress・
// task notification・user tool_result・background通知・resultでは
// MODEL_IDLEが誤ってリセットされない。
func IsModelActivityEvent(record TaskEventRecord) bool {
	if record.Kind == "system" {
		return record.Subtype == "thinking_tokens"
	}
	if record.Kind != "assistant" {
		return false
	}
	for _, block := range record.Blocks {
		switch block.Type {
		case "thinking", "text", "tool_use":
			return true
		}
	}
	return false
}

func (s *StateStore) TaskLiveStatusPath(taskID string) string {
	return s.Path(filepath.Join("events", taskID+".live.json"))
}

// WriteTaskLiveStatusはlive snapshotを原子的に上書きする。UpdatedAt未設定時は現在時刻を
// 使う(AppendTaskEventのTimestamp充填と同じ規則)。
func (s *StateStore) WriteTaskLiveStatus(taskID string, status TaskLiveStatus) error {
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("task live statusをJSON化できません: %w", err)
	}
	return writeFileAtomic(s.TaskLiveStatusPath(taskID), append(data, '\n'), 0o600)
}

// ReadTaskLiveStatusはlive snapshotを読む。不在・破損はerrorを返し、呼出元のwatch表示は
// 詳細行を省いた表示へ落とす(snapshot欠損でwatch自体は失敗させない)。
func (s *StateStore) ReadTaskLiveStatus(taskID string) (TaskLiveStatus, error) {
	data, err := os.ReadFile(s.TaskLiveStatusPath(taskID))
	if err != nil {
		return TaskLiveStatus{}, err
	}
	var status TaskLiveStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return TaskLiveStatus{}, fmt.Errorf("task live statusを読めません: %w", err)
	}
	return status, nil
}
