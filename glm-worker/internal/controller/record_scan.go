package controller

import (
	"os"
	"strings"
)

const jsonRecordFileExt = ".json"

func canonicalJSONRecordEntry(entry os.DirEntry) (string, bool, error) {
	name := entry.Name()
	if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, jsonRecordFileExt) {
		return "", false, nil
	}
	identity := strings.TrimSuffix(name, jsonRecordFileExt)
	if identity == "" {
		return "", false, nil
	}
	info, err := entry.Info()
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, nil
	}
	return identity, true, nil
}
