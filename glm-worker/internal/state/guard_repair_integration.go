package state

import "fmt"

type GuardRepairIntegrationFile struct {
	Path        string `json:"path"`
	Content     []byte `json:"content,omitempty"`
	Mode        uint32 `json:"mode,omitempty"`
	Exists      bool   `json:"exists"`
	PostContent []byte `json:"post_content,omitempty"`
	PostMode    uint32 `json:"post_mode,omitempty"`
	PostExists  bool   `json:"post_exists"`
}

func (integration GuardRepairIntegration) validate() error {
	boundary := integration.RepositoryBoundary
	if boundary.Head == "" || boundary.IndexDigest == "" || boundary.WorktreeDigest == "" || boundary.ParentFiles == nil {
		return fmt.Errorf("guard repair integration repository boundary is incomplete")
	}
	if len(integration.Files) == 0 {
		return fmt.Errorf("guard repair integration has no file preimages")
	}
	seen := make(map[string]struct{}, len(integration.Files))
	for _, file := range integration.Files {
		if _, ok := seen[file.Path]; ok {
			return fmt.Errorf("guard repair integration has duplicate file path %s", file.Path)
		}
		if err := file.validate(); err != nil {
			return err
		}
		seen[file.Path] = struct{}{}
	}
	return nil
}

func (file GuardRepairIntegrationFile) validate() error {
	if file.Path == "" {
		return fmt.Errorf("guard repair integration has an empty file path")
	}
	if err := validateGuardRepairIntegrationImage(file.Path, file.Content, file.Mode, file.Exists, "preimage"); err != nil {
		return err
	}
	return validateGuardRepairIntegrationImage(file.Path, file.PostContent, file.PostMode, file.PostExists, "postimage")
}

func validateGuardRepairIntegrationImage(path string, content []byte, mode uint32, exists bool, kind string) error {
	if mode&^uint32(0o777) != 0 {
		return fmt.Errorf("guard repair integration has invalid %s mode for %s", kind, path)
	}
	if !exists && (len(content) != 0 || mode != 0) {
		return fmt.Errorf("guard repair integration has %s content for absent file %s", kind, path)
	}
	return nil
}
