package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type publicationRunInventory struct {
	module *state.StateStore
	latest map[publicationRunKey]publicationRunSelection
	byID   map[string]qualitygate.RunRecord
}

type publicationRunKey struct {
	form       string
	repository string
	probeID    string
}

type publicationRunSelection struct {
	record    qualitygate.RunRecord
	ambiguous bool
}

func readPublicationRunInventory(module *state.StateStore) (*publicationRunInventory, error) {
	return readPublicationRunInventoryWith(module, os.ReadDir, qualitygate.Read)
}

func readPublicationRunInventoryWith(
	module *state.StateStore,
	readDir func(string) ([]os.DirEntry, error),
	readRecord func(*state.StateStore, string) (qualitygate.RunRecord, error),
) (*publicationRunInventory, error) {
	entries, err := readDir(module.Path(qualitygate.RunDirectory))
	if err != nil {
		return nil, err
	}
	inventory := &publicationRunInventory{
		module: module,
		latest: make(map[publicationRunKey]publicationRunSelection),
		byID:   make(map[string]qualitygate.RunRecord),
	}
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := readRecord(module, entry.Name())
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("publication run %s is missing its persisted record", entry.Name())
		}
		if err != nil {
			return nil, fmt.Errorf("publication run %s is unreadable: %w", entry.Name(), err)
		}
		key := publicationRunKey{form: record.Form, repository: filepath.Clean(record.Repository)}
		if record.Form == smokeProbeForm {
			key.probeID = record.ProbeID
		}
		previous, found := inventory.latest[key]
		switch {
		case !found, record.StartedAt.After(previous.record.StartedAt):
			inventory.latest[key] = publicationRunSelection{record: record}
		case record.StartedAt.Equal(previous.record.StartedAt):
			previous.ambiguous = true
			inventory.latest[key] = previous
		}
		inventory.byID[record.ValidationRunID] = record
	}
	return inventory, nil
}

func (s *Store) publicationRunInventoryFor(c AcceptedCandidate) (*publicationRunInventory, error) {
	if len(c.RequiredValidations) == 0 && len(c.RequiredSmokeProbes) == 0 {
		return nil, nil
	}
	module, err := s.finalValidationRunStore(c.TaskRef)
	if err != nil {
		return nil, err
	}
	return readPublicationRunInventory(module)
}

func (inventory *publicationRunInventory) latestValidation(repoRoot, form string) (qualitygate.RunRecord, bool, error) {
	return inventory.selectLatest(
		publicationRunKey{form: form, repository: filepath.Clean(repoRoot)},
		"final validation run ordering is ambiguous",
	)
}

func (inventory *publicationRunInventory) latestSmokeProbe(repoRoot, probeID string) (qualitygate.RunRecord, bool, error) {
	return inventory.selectLatest(
		publicationRunKey{form: smokeProbeForm, repository: filepath.Clean(repoRoot), probeID: probeID},
		"post-install smoke run ordering is ambiguous",
	)
}

func (inventory *publicationRunInventory) selectLatest(key publicationRunKey, ambiguity string) (qualitygate.RunRecord, bool, error) {
	if inventory == nil {
		return qualitygate.RunRecord{}, false, nil
	}
	selection, found := inventory.latest[key]
	if !found {
		return qualitygate.RunRecord{}, false, nil
	}
	if selection.ambiguous {
		return qualitygate.RunRecord{}, false, errors.New(ambiguity)
	}
	return selection.record, true, nil
}

func (inventory *publicationRunInventory) record(runID string) (qualitygate.RunRecord, error) {
	if !qualitygate.ValidRunID(runID) {
		return qualitygate.RunRecord{}, fmt.Errorf("invalid validation run id")
	}
	if inventory != nil {
		if record, ok := inventory.byID[runID]; ok {
			return record, nil
		}
	}
	return qualitygate.RunRecord{}, os.ErrNotExist
}
