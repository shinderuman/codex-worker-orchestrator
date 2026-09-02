package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const taskLiveStatusSchemaVersion = 1

const gitSnapshotSchemaVersion = 1

const taskStatsSchemaRevision = 1

const modelCallLogSchemaRevision = 1

type taskEventRecordAlias TaskEventRecord

type taskLiveStatusAlias TaskLiveStatus

type gitSnapshotAlias GitSnapshot

type taskStatsAlias TaskStats

type modelCallLogAlias ModelCallLog

type taskLiveStatusWire struct {
	Version int `json:"version"`
	taskLiveStatusAlias
}

type gitSnapshotWire struct {
	Version int `json:"version"`
	gitSnapshotAlias
}

type taskStatsWire struct {
	SchemaRevision int `json:"schema_revision"`
	taskStatsAlias
}

type modelCallLogWire struct {
	SchemaRevision int `json:"schema_revision"`
	modelCallLogAlias
}

func decodeCurrentStateJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func (record *TaskEventRecord) UnmarshalJSON(data []byte) error {
	var current taskEventRecordAlias
	if err := decodeCurrentStateJSON(data, &current); err != nil {
		return err
	}
	*record = TaskEventRecord(current)
	return nil
}

func (status TaskLiveStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(taskLiveStatusWire{
		Version:             taskLiveStatusSchemaVersion,
		taskLiveStatusAlias: taskLiveStatusAlias(status),
	})
}

func (status *TaskLiveStatus) UnmarshalJSON(data []byte) error {
	var current taskLiveStatusWire
	if err := decodeCurrentStateJSON(data, &current); err != nil {
		return err
	}
	if current.Version != taskLiveStatusSchemaVersion {
		return fmt.Errorf("unsupported task live status version: %d", current.Version)
	}
	*status = TaskLiveStatus(current.taskLiveStatusAlias)
	return nil
}

func (snapshot GitSnapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(gitSnapshotWire{
		Version:          gitSnapshotSchemaVersion,
		gitSnapshotAlias: gitSnapshotAlias(snapshot),
	})
}

func (snapshot *GitSnapshot) UnmarshalJSON(data []byte) error {
	var current gitSnapshotWire
	if err := decodeCurrentStateJSON(data, &current); err != nil {
		return err
	}
	if current.Version != gitSnapshotSchemaVersion {
		return fmt.Errorf("unsupported git snapshot version: %d", current.Version)
	}
	*snapshot = GitSnapshot(current.gitSnapshotAlias)
	return nil
}

func (stats TaskStats) MarshalJSON() ([]byte, error) {
	return json.Marshal(taskStatsWire{
		SchemaRevision: taskStatsSchemaRevision,
		taskStatsAlias: taskStatsAlias(stats),
	})
}

func (stats *TaskStats) UnmarshalJSON(data []byte) error {
	var header struct {
		Version        int `json:"version"`
		SchemaRevision int `json:"schema_revision"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	if header.Version != taskStatsVersion || header.SchemaRevision != taskStatsSchemaRevision {
		return fmt.Errorf("%w: version=%d schema_revision=%d", errUnsupportedTaskStatsVersion, header.Version, header.SchemaRevision)
	}
	var current taskStatsWire
	if err := decodeCurrentStateJSON(data, &current); err != nil {
		return err
	}
	*stats = TaskStats(current.taskStatsAlias)
	return nil
}

func (call ModelCallLog) MarshalJSON() ([]byte, error) {
	return json.Marshal(modelCallLogWire{
		SchemaRevision:   modelCallLogSchemaRevision,
		modelCallLogAlias: modelCallLogAlias(call),
	})
}

func (call *ModelCallLog) UnmarshalJSON(data []byte) error {
	var header struct {
		Version        int `json:"version"`
		SchemaRevision int `json:"schema_revision"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	if header.Version != ModelCallLogVersion || header.SchemaRevision != modelCallLogSchemaRevision {
		*call = ModelCallLog{Version: header.Version}
		if header.Version == ModelCallLogVersion {
			call.Version = 0
		}
		return nil
	}
	var current modelCallLogWire
	if err := decodeCurrentStateJSON(data, &current); err != nil {
		return err
	}
	*call = ModelCallLog(current.modelCallLogAlias)
	return nil
}
