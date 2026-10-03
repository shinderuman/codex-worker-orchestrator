package controller

import (
	"fmt"
	"os"
	"strings"
)

type ReboundSuspension struct {
	BaseOID  string         `json:"base_oid"`
	Baseline ExecutionTrees `json:"baseline"`
	Current  ExecutionTrees `json:"current"`
}

func RebindSuspensionTrees(repo string, snapshot SuspensionSnapshot, newBase string) (ReboundSuspension, error) {
	oldTree, err := gitTrimmed(repo, "rev-parse", snapshot.ExecutionBaseOID+"^{tree}")
	if err != nil {
		return ReboundSuspension{}, err
	}
	newTree, err := gitTrimmed(repo, "rev-parse", newBase+"^{tree}")
	if err != nil {
		return ReboundSuspension{}, err
	}
	result := ReboundSuspension{BaseOID: newBase}
	result.Baseline.IndexTree, err = mergeSuspensionTrees(repo, oldTree, snapshot.Baseline.IndexTree, newTree)
	if err != nil {
		return ReboundSuspension{}, err
	}
	result.Baseline.WorktreeTree, err = mergeSuspensionTrees(repo, oldTree, snapshot.Baseline.WorktreeTree, newTree)
	if err != nil {
		return ReboundSuspension{}, err
	}
	result.Current.IndexTree, err = mergeSuspensionTrees(repo, snapshot.Baseline.IndexTree, snapshot.Current.IndexTree, result.Baseline.IndexTree)
	if err != nil {
		return ReboundSuspension{}, err
	}
	result.Current.WorktreeTree, err = mergeSuspensionTrees(repo, snapshot.Baseline.WorktreeTree, snapshot.Current.WorktreeTree, result.Baseline.WorktreeTree)
	if err != nil {
		return ReboundSuspension{}, err
	}
	if err := normalizeReboundTrees(repo, newBase, &result); err != nil {
		return ReboundSuspension{}, err
	}
	return result, nil
}

func mergeSuspensionTrees(repo, baseTree, oursTree, theirsTree string) (string, error) {
	base, err := suspensionTreeCommit(repo, baseTree, "")
	if err != nil {
		return "", err
	}
	ours, err := suspensionTreeCommit(repo, oursTree, base)
	if err != nil {
		return "", err
	}
	theirs, err := suspensionTreeCommit(repo, theirsTree, base)
	if err != nil {
		return "", err
	}
	data, err := runGitBinary(repo, nil, "-c", "merge.renames=false", "merge-tree", "--write-tree", "--merge-base="+base, ours, theirs)
	if err != nil {
		return "", fmt.Errorf("suspension three-way rebind conflict: %w", err)
	}
	tree := strings.TrimSpace(string(data))
	if strings.Contains(tree, "\n") || tree == "" {
		return "", fmt.Errorf("ambiguous suspension merge result")
	}
	return tree, nil
}

func suspensionTreeCommit(repo, tree, parent string) (string, error) {
	args := []string{"-c", "user.name=Repository Controller", "-c", "user.email=controller@invalid", "commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	data, err := runGitBinary(repo, []byte("controller suspension merge\n"), args...)
	return strings.TrimSpace(string(data)), err
}

func normalizeReboundTrees(repo, base string, result *ReboundSuspension) error {
	targets := []*ExecutionTrees{&result.Baseline, &result.Current}
	for _, target := range targets {
		for _, tree := range []*string{&target.IndexTree, &target.WorktreeTree} {
			index, err := newSuspensionIndex(repo, *tree)
			if err != nil {
				return err
			}
			digest, err := normalizeParentPaths(repo, index, base)
			if err == nil {
				*tree, err = suspensionGitText(repo, index, nil, "write-tree")
			}
			removeErr := os.Remove(index)
			if err != nil {
				return err
			}
			if removeErr != nil {
				return removeErr
			}
			target.ParentAuthorityDigest = digest
		}
	}
	return nil
}
