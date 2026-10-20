package controller

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func acceptedValidationCandidate(cfg config.AppConfig) (*Store, *AcceptedCandidate, error) {
	if cfg.StateBase == "" {
		return nil, nil, nil
	}
	identity, err := ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return nil, nil, err
	}
	s := &Store{dir: controllerStoreDir(cfg, identity), identity: identity, config: cfg}
	if _, err := os.Stat(s.headPath()); os.IsNotExist(err) {
		return s, nil, nil
	} else if err != nil {
		return nil, nil, err
	}
	head, err := s.LoadHead()
	if err != nil || head.AcceptedCandidateRef == nil {
		return s, nil, err
	}
	candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	return s, &candidate, err
}

func PrepareFinalValidationWorkspace(cfg config.AppConfig, form string) (string, func() error, error) {
	s, c, err := acceptedValidationCandidate(cfg)
	if err != nil || c == nil {
		return "", nil, err
	}
	for _, request := range c.RequiredValidations {
		if request.Form != form {
			continue
		}
		root, err := s.finalValidationWorkspace(*c, request)
		if err != nil {
			return root, nil, err
		}
		if err := s.ensureFinalValidationCheckout(root, *c); err != nil {
			return "", nil, err
		}
		return root, func() error { return s.releaseFinalValidationCheckout(root, c.TaskRef) }, nil
	}
	return "", nil, fmt.Errorf("accepted candidate declares no %s validation", form)
}

func (s *Store) ensureFinalValidationCheckout(root string, c AcceptedCandidate) error {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		if _, err := runGitBinary(s.identity.PrimaryRoot, nil, "worktree", "add", "--detach", root, c.CommitOID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return s.verifyFinalValidationCheckout(root, c)
}

func (s *Store) verifyFinalValidationCheckout(root string, c AcceptedCandidate) error {
	identity, err := ResolveRepositoryIdentity(root)
	if err != nil {
		return err
	}
	if identity.LineageID != s.identity.LineageID {
		return fmt.Errorf("validation checkout belongs to another repository")
	}
	snapshot, err := state.CaptureGitSnapshot(root)
	if err != nil {
		return err
	}
	source, err := s.finalValidationSource(c)
	if err != nil {
		return err
	}
	if snapshot.Head != source.CommitOID || snapshot.IndexDigest != source.IndexDigest || snapshot.WorktreeDigest != source.CleanWorktreeDig {
		return fmt.Errorf("validation checkout differs from the final candidate")
	}
	absent, err := qualitygate.NoIgnoredInputs(root)
	if err != nil {
		return err
	}
	if !absent {
		return fmt.Errorf("validation checkout contains ignored inputs")
	}
	return nil
}

func (s *Store) releaseFinalValidationCheckout(root string, task SemanticTaskRef) error {
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	}
	cfg := WorkflowTaskConfig(s.config, s.identity, task)
	if err := finalValidationCheckoutQuiescent(state.AttachStateStore(cfg), root); err != nil {
		return err
	}
	_, err := runGitBinary(s.identity.PrimaryRoot, nil, "worktree", "remove", root)
	return err
}

func finalValidationCheckoutQuiescent(module *state.StateStore, root string) error {
	entries, err := os.ReadDir(module.Path(qualitygate.RunDirectory))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := qualitygate.Read(module, entry.Name())
		if err != nil {
			return err
		}
		if record.Repository == root && record.Status == qualitygate.StatusRunning {
			return fmt.Errorf("validation %s is still running; checkout retained", record.ValidationRunID)
		}
	}
	return nil
}

func (s *Store) finalValidationWorkspace(c AcceptedCandidate, request FinalValidationRequest) (string, error) {
	base, err := canonicalPath(s.dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "validation-"+c.CommitOID+"-"+request.Form), nil
}
