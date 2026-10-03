package app

import (
	"fmt"

	"sort"

	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexrollout"
)

type codexRollout = codexrollout.Rollout

type codexAssociation struct {
	ParentStatus   string
	ParentPath     string
	ParentSource   string
	ParentThreadID string
	ParentChain    []codexRollout
	GuardianStatus string
	GuardianDetail string
	Guardians      []codexRollout
	Basis          string
	Detail         string
}

const (
	codexStatusIncluded    = "included"
	codexStatusMissing     = "missing"
	codexStatusUnavailable = "unavailable"
	codexStatusAmbiguous   = "ambiguous"

	codexAssociationBasis = "stored-parent-identity"
)

func (association codexAssociation) rolloutChain() []codexRollout {
	if len(association.ParentChain) > 0 {
		return association.ParentChain
	}
	if association.ParentPath == "" {
		return nil
	}
	return []codexRollout{{AbsolutePath: association.ParentPath, HomeRelative: association.ParentSource}}
}

func (association codexAssociation) parentSources() []string {
	chain := association.rolloutChain()
	sources := make([]string, 0, len(chain))
	for _, member := range chain {
		sources = append(sources, member.HomeRelative)
	}
	return sources
}

func (association codexAssociation) parentSourceLabel() string {
	return strings.Join(association.parentSources(), ";")
}

func resolveCodexAssociation(codexHome string, task bundleTask) codexAssociation {
	return resolveCodexAssociationWithScan(codexHome, task, codexrollout.Scan)
}

func resolveCodexAssociationWithScan(codexHome string, task bundleTask, scan func(string) ([]codexRollout, error)) codexAssociation {
	threadID := task.Stats.ParentCodexThreadID
	if threadID == "" {
		return codexAssociation{ParentStatus: codexStatusMissing, Detail: "parent Codex identity is not recorded for this task"}
	}
	if !codexrollout.DirExists(codexHome) {
		return codexAssociation{ParentStatus: codexStatusUnavailable, Basis: codexAssociationBasis, Detail: "codex home is not present"}
	}
	rollouts, err := scan(codexHome)
	if err != nil {
		return codexAssociation{ParentStatus: codexStatusUnavailable, Basis: codexAssociationBasis, Detail: "codex rollout enumeration failed: " + err.Error()}
	}
	return buildCodexAssociation(codexrollout.Matching(rollouts, threadID), rollouts, codexAssociationBasis, task)
}

func buildCodexAssociation(matches, rollouts []codexRollout, basis string, task bundleTask) codexAssociation {
	switch len(matches) {
	case 0:
		return codexAssociation{ParentStatus: codexStatusMissing, Basis: basis, Detail: "no rollout has session_meta.id equal to the stored parent thread ID"}
	case 1:
		return includedCodexAssociation(matches[0], rollouts, basis, task)
	default:
		chain, reason := codexrollout.ResolveChain(matches)
		if reason != "" {
			return ambiguousCodexChainAssociation(matches, basis, reason)
		}
		return includedCodexChainAssociation(chain, rollouts, basis, task)
	}
}

func ambiguousCodexChainAssociation(matches []codexRollout, basis, reason string) codexAssociation {
	detail := fmt.Sprintf("%d rollouts share the stored parent thread ID; %s", len(matches), reason)
	return codexAssociation{ParentStatus: codexStatusAmbiguous, Basis: basis, Detail: detail}
}

func includedCodexChainAssociation(chain, rollouts []codexRollout, basis string, task bundleTask) codexAssociation {
	association := includedCodexAssociation(chain[0], rollouts, basis, task)
	association.ParentChain = chain
	if len(chain) > 1 {
		chainDetail := fmt.Sprintf("stored parent thread ID resolves to an ordered rollout chain of %d files", len(chain))
		if association.Detail == "" {
			association.Detail = chainDetail
		} else {
			association.Detail = association.Detail + "; " + chainDetail
		}
	}
	return association
}

func includedCodexAssociation(parent codexRollout, rollouts []codexRollout, basis string, task bundleTask) codexAssociation {
	start, end := taskWindow(task)
	guardians, qualifying := selectCodexGuardianChildren(rollouts, parent, start, end)
	association := codexAssociation{
		ParentStatus:   codexStatusIncluded,
		ParentPath:     parent.AbsolutePath,
		ParentSource:   parent.HomeRelative,
		ParentThreadID: parent.ID,
		ParentChain:    []codexRollout{parent},
		GuardianStatus: codexStatusIncluded,
		Guardians:      guardians,
		Basis:          basis,
	}
	if qualifying > len(guardians) {
		association.GuardianStatus = codexStatusAmbiguous
		association.Guardians = nil
		association.GuardianDetail = fmt.Sprintf("%d rollouts share a direct guardian thread ID", qualifying)
	}
	return association
}

func taskWindow(task bundleTask) (time.Time, time.Time) {
	start := task.Stats.StartedAt.UTC()
	end := time.Now().UTC()
	if task.Stats.ArchivedAt != nil && task.Stats.ArchivedAt.Before(end) {
		end = task.Stats.ArchivedAt.UTC()
	}
	return start, end
}

func selectCodexGuardianChildren(rollouts []codexRollout, parent codexRollout, start, end time.Time) ([]codexRollout, int) {
	unique := make(map[string]codexRollout)
	qualifying := 0
	for _, rollout := range rollouts {
		if rollout.ParentThreadID != parent.ID || !rollout.GuardianSource {
			continue
		}
		last, ok := codexrollout.LastTimestamp(rollout.AbsolutePath)
		if !ok || rollout.FirstTimestamp.After(end) || last.Before(start) {
			continue
		}
		qualifying++
		unique[rollout.ID] = rollout
	}
	children := make([]codexRollout, 0, len(unique))
	for _, rollout := range unique {
		children = append(children, rollout)
	}
	sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
	return children, qualifying
}
