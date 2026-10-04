package parentactioncmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// finalizationStatusBlocked historically shared the publication prepare status.
// Keep the value local to the live finalize-check surface after the retired
// publication command graph is removed.
const publicationPrepareStatusBlocked = "blocked"

type gitUpstream struct {
	RemoteName  string
	RemoteRef   string
	TrackingRef string
	TrackingOID string
}

var (
	errGitUpstreamMissing  = errors.New("branch upstream missing")
	errGitRemoteURLMissing = errors.New("remote url missing")
)

func resolveGitUpstream(repoRoot, branch string) (gitUpstream, error) {
	remoteName, err := gitFinalizationOutput(repoRoot, "config", "--get", "branch."+branch+".remote")
	if err != nil {
		return gitUpstream{}, errGitUpstreamMissing
	}
	remoteRef, err := gitFinalizationOutput(repoRoot, "config", "--get", "branch."+branch+".merge")
	if err != nil {
		return gitUpstream{}, errGitUpstreamMissing
	}
	if _, err := gitFinalizationOutput(repoRoot, "config", "--get", "remote."+strings.TrimSpace(remoteName)+".url"); err != nil {
		return gitUpstream{}, errGitRemoteURLMissing
	}
	upstream := gitUpstream{
		RemoteName: strings.TrimSpace(remoteName),
		RemoteRef:  strings.TrimSpace(remoteRef),
	}
	upstream.TrackingRef = gitTrackingRefForRemoteRef(upstream.RemoteName, upstream.RemoteRef)
	if upstream.TrackingRef != "" {
		if trackingOID, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", "-q", upstream.TrackingRef); err == nil {
			upstream.TrackingOID = strings.TrimSpace(trackingOID)
		}
	}
	return upstream, nil
}

func gitTrackingRefForRemoteRef(remoteName, remoteRef string) string {
	if !strings.HasPrefix(remoteRef, "refs/heads/") {
		return ""
	}
	return "refs/remotes/" + remoteName + "/" + strings.TrimPrefix(remoteRef, "refs/heads/")
}

func gitAheadBehind(repoRoot, trackingRef, headOID string) (int, int, error) {
	output, err := gitFinalizationOutput(repoRoot, "rev-list", "--left-right", "--count", trackingRef+"..."+headOID)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list count output: %q", strings.TrimSpace(output))
	}
	behind, behindErr := strconv.Atoi(fields[0])
	ahead, aheadErr := strconv.Atoi(fields[1])
	if behindErr != nil || aheadErr != nil {
		return 0, 0, fmt.Errorf("unexpected rev-list count output: %q", strings.TrimSpace(output))
	}
	return ahead, behind, nil
}
