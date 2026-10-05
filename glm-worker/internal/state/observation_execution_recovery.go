package state

import (
	"fmt"
	"strings"
	"time"
)

func (s *StateStore) ResolveObservationExecutionIndeterminate(executionID, detail string, completedAt time.Time) error {
	records, err := s.ObservationExecutions()
	if err != nil {
		return err
	}
	for index := range records {
		record := &records[index]
		if record.ExecutionID != executionID {
			continue
		}
		if record.Status != ObservationExecutionStatusInFlight {
			return fmt.Errorf("observation execution %s is not in-flight", executionID)
		}
		record.Status = ObservationExecutionStatusIndeterminate
		record.Detail = strings.TrimSpace(detail)
		record.ExitSource = "recovery"
		record.CompletedAtRFC3339 = completedAt.UTC().Format(time.RFC3339Nano)
		return s.writeObservationExecutions(records)
	}
	return fmt.Errorf("observation execution %s has no durable in-flight identity", executionID)
}
