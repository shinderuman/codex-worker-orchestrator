package state

import "encoding/json"

func (identity *taskStatsArchiveIdentity) UnmarshalJSON(data []byte) error {
	var stats TaskStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return err
	}
	*identity = taskStatsArchiveIdentity{
		Version:        stats.Version,
		SchemaRevision: taskStatsSchemaRevision,
		TaskID:         stats.TaskID,
		Status:         stats.Status,
	}
	return nil
}
