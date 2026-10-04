package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type Store struct {
	config              config.AppConfig
	dir                 string
	identity            RepositoryIdentity
	archiveVerification sync.Mutex
	archiveVerified     map[string]struct{}
}

const controllerSchemaVersion = 1

var controllerStoreDirs = []string{
	"attempts",
	"leases",
	"transitions",
	"transition-state",
	"mutations",
	"failures",
}

func Exists(cfg config.AppConfig) (bool, error) {
	identity, err := ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return false, err
	}
	base := controllerStoreDir(cfg, identity)
	info, err := os.Stat(base)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect repository controller store: %w", err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("repository controller store is not a directory")
	}
	if _, err := os.Stat(filepath.Join(base, "head.json")); errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("repository controller store exists without head")
	} else if err != nil {
		return false, fmt.Errorf("inspect repository controller head: %w", err)
	}
	return true, nil
}

func Open(cfg config.AppConfig) (*Store, error) {
	identity, err := ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return nil, err
	}
	base := controllerStoreDir(cfg, identity)
	store := &Store{dir: base, identity: identity, config: cfg}
	info, err := os.Stat(base)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := initializeControllerStore(store); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, fmt.Errorf("inspect repository controller store: %w", err)
	case !info.IsDir():
		return nil, fmt.Errorf("repository controller store is not a directory")
	default:
		if err := validateControllerStoreLayout(store); err != nil {
			return nil, err
		}
	}
	if _, err := store.LoadHead(); err != nil {
		return nil, err
	}
	return store, nil
}

func controllerStoreDir(cfg config.AppConfig, identity RepositoryIdentity) string {
	return filepath.Join(filepath.Dir(cfg.StateBase), "controllers", identity.LineageID)
}

func initializeControllerStore(store *Store) error {
	parent := filepath.Dir(store.dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create repository controller store parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(store.dir)+".init-*")
	if err != nil {
		return fmt.Errorf("create repository controller store staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	staged := *store
	staged.dir = staging
	for _, name := range controllerStoreDirs {
		if err := os.Mkdir(filepath.Join(staging, name), 0o700); err != nil {
			return fmt.Errorf("create repository controller store staging layout: %w", err)
		}
	}
	head := RepositoryControllerHead{
		SchemaVersion:        controllerSchemaVersion,
		RepositoryIdentity:   store.identity.LineageID,
		ControllerGeneration: 0,
		Status:               ControllerStatusActive,
	}
	if err := writeJSONAtomic(staged.headPath(), head); err != nil {
		return fmt.Errorf("create repository controller staged head: %w", err)
	}
	if err := syncDirectoryPath(staging); err != nil {
		return fmt.Errorf("sync repository controller staged store: %w", err)
	}
	if err := os.Rename(staging, store.dir); err != nil {
		if _, statErr := os.Stat(store.dir); statErr == nil {
			if layoutErr := validateControllerStoreLayout(store); layoutErr == nil {
				return nil
			}
		}
		return fmt.Errorf("publish repository controller store: %w", err)
	}
	if err := syncDirectoryPath(parent); err != nil {
		return fmt.Errorf("sync repository controller store parent: %w", err)
	}
	return nil
}

func validateControllerStoreLayout(store *Store) error {
	if _, err := os.Stat(store.headPath()); err != nil {
		return fmt.Errorf("inspect repository controller head: %w", err)
	}
	for _, name := range controllerStoreDirs {
		info, err := os.Stat(filepath.Join(store.dir, name))
		if err != nil {
			return fmt.Errorf("inspect repository controller store layout: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("repository controller store layout entry %s is not a directory", name)
		}
	}
	return nil
}

func (s *Store) Identity() RepositoryIdentity {
	return s.identity
}

func (s *Store) LoadHead() (RepositoryControllerHead, error) {
	var head RepositoryControllerHead
	if err := readJSON(s.headPath(), &head); err != nil {
		return RepositoryControllerHead{}, fmt.Errorf("read repository controller head: %w", err)
	}
	if head.SchemaVersion != controllerSchemaVersion || head.RepositoryIdentity != s.identity.LineageID {
		return RepositoryControllerHead{}, fmt.Errorf("repository controller head identity is invalid")
	}
	if head.Status != ControllerStatusActive && head.Status != ControllerStatusFailClosed {
		return RepositoryControllerHead{}, fmt.Errorf("repository controller status is invalid: %s", head.Status)
	}
	return head, nil
}

func (s *Store) BootstrapExecution(task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	return s.bootstrapExecution(task, workspace, snapshot)
}

func (s *Store) AdmitMutation(authority MutationAuthority, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	return s.admitMutation(authority, workspace, snapshot)
}

func (s *Store) writeHeadCAS(expected uint64, next RepositoryControllerHead) error {
	current, err := s.LoadHead()
	if err != nil {
		return err
	}
	if current.ControllerGeneration != expected {
		return fmt.Errorf("controller generation CAS failed: got=%d want=%d", current.ControllerGeneration, expected)
	}
	if next.SchemaVersion != controllerSchemaVersion || next.RepositoryIdentity != s.identity.LineageID || next.ControllerGeneration <= current.ControllerGeneration {
		return fmt.Errorf("repository controller head replacement is invalid")
	}
	return writeJSONAtomic(s.headPath(), next)
}

func (s *Store) writeAttempt(record AttemptRecord) error {
	return writeJSONAtomic(filepath.Join(s.dir, "attempts", record.AttemptID+".json"), record)
}

func (s *Store) loadAttempt(id string) (AttemptRecord, error) {
	var record AttemptRecord
	if err := readJSON(filepath.Join(s.dir, "attempts", id+".json"), &record); err != nil {
		return AttemptRecord{}, fmt.Errorf("read attempt %s: %w", id, err)
	}
	if record.SchemaVersion != controllerSchemaVersion || record.AttemptID != id {
		return AttemptRecord{}, fmt.Errorf("attempt %s identity is invalid", id)
	}
	return record, nil
}

func (s *Store) writeLease(record ExecutionLease) error {
	return writeJSONAtomic(filepath.Join(s.dir, "leases", record.LeaseID+".json"), record)
}

func (s *Store) loadLease(id string) (ExecutionLease, error) {
	var record ExecutionLease
	if err := readJSON(filepath.Join(s.dir, "leases", id+".json"), &record); err != nil {
		return ExecutionLease{}, fmt.Errorf("read lease %s: %w", id, err)
	}
	if record.SchemaVersion != controllerSchemaVersion || record.LeaseID != id {
		return ExecutionLease{}, fmt.Errorf("lease %s identity is invalid", id)
	}
	return record, nil
}

func (s *Store) writeTransitionState(record TransitionState) error {
	return writeJSONAtomic(s.transitionStatePath(record.TransitionID), record)
}

func (s *Store) loadTransitionState(id string) (TransitionState, error) {
	var record TransitionState
	if err := readJSON(s.transitionStatePath(id), &record); err != nil {
		return TransitionState{}, err
	}
	if record.SchemaVersion != controllerSchemaVersion || record.TransitionID != id {
		return TransitionState{}, fmt.Errorf("transition state %s identity is invalid", id)
	}
	return record, nil
}

func (s *Store) headPath() string {
	return filepath.Join(s.dir, "head.json")
}

func (s *Store) transitionPath(id string) string {
	return filepath.Join(s.dir, "transitions", id+".json")
}

func (s *Store) transitionStatePath(id string) string {
	return filepath.Join(s.dir, "transition-state", id+".json")
}

func (s *Store) mutationPath(id string) string {
	return filepath.Join(s.dir, "mutations", id+".json")
}

func (s *Store) failurePath(id string) string {
	return filepath.Join(s.dir, "failures", id+".json")
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".controller-*.tmp")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	return nil
}
