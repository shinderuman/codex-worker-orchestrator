//go:build unix

package runner

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

var errStagingDirentMalformed = errors.New("staging directoryのdirentが不正です")

func openStagingRequestFile(dir, name string) (*os.File, error) {
	return os.OpenFile(filepath.Join(dir, name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

func stagingFileIdentity(info os.FileInfo) (stagingDirIdentity, bool) {
	system, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return stagingDirIdentity{}, false
	}
	return stagingDirIdentity{device: uint64(system.Dev), inode: system.Ino}, true
}

func stagingDirFileIdentity(file *os.File) (stagingDirIdentity, bool) {
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		return stagingDirIdentity{}, false
	}
	system, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return stagingDirIdentity{}, false
	}
	return stagingDirIdentity{device: uint64(system.Dev), inode: system.Ino}, true
}

func readStagingDirentsFD(file *os.File) ([]stagingDirent, error) {
	dirents := make([]stagingDirent, 0, 16)
	buf := make([]byte, 8192)
	for {
		count, chunkErr := readStagingDirentChunk(file, buf)
		if chunkErr != nil {
			return nil, chunkErr
		}
		if count == 0 {
			return dirents, nil
		}
		dirents, chunkErr = appendStagingDirentRecords(dirents, buf[:count])
		if chunkErr != nil {
			return nil, chunkErr
		}
	}
}

func appendStagingDirentRecords(dirents []stagingDirent, chunk []byte) ([]stagingDirent, error) {
	for len(chunk) > 0 {
		reclen, lengthOK := stagingDirentRecordLength(chunk)
		if !lengthOK || reclen < 1 || reclen > len(chunk) {
			return nil, errStagingDirentMalformed
		}
		entry, entryOK := stagingDirentRecord(chunk[:reclen])
		if !entryOK {
			return nil, errStagingDirentMalformed
		}
		chunk = chunk[reclen:]
		if entry.name == "" || entry.name == "." || entry.name == ".." {
			continue
		}
		dirents = append(dirents, entry)
	}
	return dirents, nil
}

func createStagingResponseFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
}

func pinStagingDirIdentity(path string) (stagingDirIdentity, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return stagingDirIdentity{}, false
	}
	system, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return stagingDirIdentity{}, false
	}
	return stagingDirIdentity{device: uint64(system.Dev), inode: system.Ino}, true
}
