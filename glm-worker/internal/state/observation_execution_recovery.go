package state

import (
	"fmt"
	"strings"
	"time"
)

func (s *StateStore) ResolveObservationExecutionIndeterminate(executionID, detail string, completedAt time.Time) error {
	doc,err:=s.readObservationDocument()
	if err!=nil{return err}
	for index := range doc.Records {
		record := &doc.Records[index]
		if record.ExecutionID != executionID {
			continue
		}
		if record.Status != ObservationExecutionStatusInFlight {
			return nil
		}
		record.Status = ObservationExecutionStatusIndeterminate
		record.Detail = strings.TrimSpace(detail)
		record.ExitSource = "recovery"
		record.CompletedAtRFC3339 = completedAt.UTC().Format(time.RFC3339Nano)
		return s.writeObservationDocument(doc)
	}
	return fmt.Errorf("observation execution %s has no durable in-flight identity", executionID)
}
