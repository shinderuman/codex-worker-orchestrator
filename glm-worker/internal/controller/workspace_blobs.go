package controller

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type workspaceBlobs map[string][]byte

const workspaceBlobBatchSize = 128

func readWorkspaceBlobs(repo string, groups ...[]workspaceTreeEntry) (workspaceBlobs, error) {
	blobs := workspaceBlobs{}
	requested := workspaceBlobOIDs(groups)
	if len(requested) == 0 {
		return blobs, nil
	}
	data, err := runGitBinary(repo, []byte(strings.Join(requested, "\n")+"\n"), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	for _, oid := range requested {
		var content []byte
		content, data, err = readWorkspaceBlob(oid, data)
		if err != nil {
			return nil, err
		}
		blobs[oid] = content
	}
	if len(data) != 0 {
		return nil, fmt.Errorf("git blob batch contains unexpected trailing data")
	}
	return blobs, nil
}

func workspaceBlobOIDs(groups [][]workspaceTreeEntry) []string {
	seen := map[string]bool{}
	var requested []string
	for _, entries := range groups {
		for _, entry := range entries {
			if entry.path == "" || entry.mode == "160000" || seen[entry.oid] {
				continue
			}
			seen[entry.oid] = true
			requested = append(requested, entry.oid)
		}
	}
	return requested
}

func readWorkspaceBlob(oid string, data []byte) ([]byte, []byte, error) {
	header, rest, found := bytes.Cut(data, []byte{'\n'})
	fields := strings.Fields(string(header))
	if !found || len(fields) != 3 || fields[0] != oid || fields[1] != "blob" {
		return nil, nil, fmt.Errorf("git blob batch returned an invalid object for %s", oid)
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size < 0 || size >= len(rest) || rest[size] != '\n' {
		return nil, nil, fmt.Errorf("git blob batch returned an invalid size for %s", oid)
	}
	return rest[:size], rest[size+1:], nil
}

func (b workspaceBlobs) content(entry workspaceTreeEntry) ([]byte, error) {
	data, exists := b[entry.oid]
	if !exists {
		return nil, fmt.Errorf("git blob batch lacks object for %q", entry.path)
	}
	return data, nil
}
