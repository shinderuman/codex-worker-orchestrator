package controller

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"

func CanonicalAuthorityActive(cfg config.AppConfig) (bool, error) {
	present, err := RepositoryPresent(cfg.RepoRoot)
	if err != nil || !present {
		return false, err
	}
	exists, err := Exists(cfg)
	if err != nil || !exists {
		return false, err
	}
	store, err := Open(cfg)
	if err != nil {
		return false, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return false, err
	}
	return !IsPristine(head), nil
}
