//go:build linux

package runner

import (
	"bytes"
	"encoding/binary"
	"os"
	"syscall"
	"unsafe"
)

func readStagingDirentChunk(file *os.File, buf []byte) (int, error) {
	var count int
	var chunkErr error
	conn, connErr := file.SyscallConn()
	if connErr != nil {
		return 0, connErr
	}
	controlErr := conn.Control(func(fd uintptr) {
		result, _, errno := syscall.Syscall(syscall.SYS_GETDENTS64, fd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if errno != 0 {
			chunkErr = errno
			return
		}
		count = int(result)
	})
	if controlErr != nil {
		return 0, controlErr
	}
	return count, chunkErr
}

func stagingDirentRecordLength(rec []byte) (int, bool) {
	offset := unsafe.Offsetof(syscall.Dirent{}.Reclen)
	if offset+2 > uintptr(len(rec)) {
		return 0, false
	}
	return int(binary.NativeEndian.Uint16(rec[offset:])), true
}

func stagingDirentRecord(rec []byte) (stagingDirent, bool) {
	inoAt := unsafe.Offsetof(syscall.Dirent{}.Ino)
	typeAt := unsafe.Offsetof(syscall.Dirent{}.Type)
	nameAt := unsafe.Offsetof(syscall.Dirent{}.Name)
	if inoAt+8 > uintptr(len(rec)) || typeAt >= uintptr(len(rec)) || nameAt >= uintptr(len(rec)) {
		return stagingDirent{}, false
	}
	reclen, ok := stagingDirentRecordLength(rec)
	if !ok || reclen > len(rec) {
		return stagingDirent{}, false
	}
	nameLen := reclen - int(nameAt)
	if nameLen <= 0 {
		return stagingDirent{}, false
	}
	name := rec[nameAt : nameAt+uintptr(nameLen)]
	if index := bytes.IndexByte(name, 0); index >= 0 {
		name = name[:index]
	}
	return stagingDirent{
		inode:   binary.NativeEndian.Uint64(rec[inoAt:]),
		regular: rec[typeAt] == syscall.DT_REG,
		name:    string(name),
	}, true
}
