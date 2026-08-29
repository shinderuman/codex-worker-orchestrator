package app

import (
	"errors"
	"os/exec"
	"runtime/debug"
	"strings"
)

type statusRuntimeBuild struct {
	VCSRevision    *string `json:"vcs_revision"`
	VCSModified    *bool   `json:"vcs_modified"`
	RepositoryHead *string `json:"repository_head"`
	Relationship   string  `json:"relationship"`
}

type runtimeBuildSettings struct {
	revision string
	modified *bool
}

const (
	runtimeBuildSame        = "same"
	runtimeBuildAncestor    = "ancestor"
	runtimeBuildNotAncestor = "not-ancestor"
	runtimeBuildUnknown     = "unknown"
)

func currentRuntimeBuild(repoRoot string) statusRuntimeBuild {
	settings := runtimeBuildSettings{}
	if info, ok := debug.ReadBuildInfo(); ok {
		settings = runtimeBuildSettingsFromGo(info.Settings)
	}
	return runtimeBuildStatus(repoRoot, settings)
}

func runtimeBuildSettingsFromGo(settings []debug.BuildSetting) runtimeBuildSettings {
	result := runtimeBuildSettings{}
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			result.revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			value := strings.TrimSpace(setting.Value)
			if value == "true" || value == "false" {
				modified := value == "true"
				result.modified = &modified
			}
		}
	}
	return result
}

func runtimeBuildStatus(repoRoot string, settings runtimeBuildSettings) statusRuntimeBuild {
	output := statusRuntimeBuild{
		VCSRevision:  stringPtr(settings.revision),
		VCSModified:  settings.modified,
		Relationship: runtimeBuildUnknown,
	}
	head := repositoryHead(repoRoot)
	output.RepositoryHead = stringPtr(head)
	if settings.revision == "" || head == "" || settings.modified == nil || *settings.modified {
		return output
	}
	if settings.revision == head {
		output.Relationship = runtimeBuildSame
		return output
	}
	if revisionIsAncestor(repoRoot, settings.revision, head) {
		output.Relationship = runtimeBuildAncestor
		return output
	}
	if revisionIsKnownNonAncestor(repoRoot, settings.revision, head) {
		output.Relationship = runtimeBuildNotAncestor
	}
	return output
}

func repositoryHead(repoRoot string) string {
	if repoRoot == "" {
		return ""
	}
	output, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func revisionIsAncestor(repoRoot, revision, head string) bool {
	command := exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", revision, head)
	return command.Run() == nil
}

func revisionIsKnownNonAncestor(repoRoot, revision, head string) bool {
	command := exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", revision, head)
	err := command.Run()
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}
