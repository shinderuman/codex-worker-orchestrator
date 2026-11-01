//go:build darwin

package runner

import (
	"encoding/binary"
	"os"
	"syscall"
	"unsafe"
)

func readStagingDirentChunk(file *os.File, buf []byte) (int, error) {
	var base uintptr
	var count int
	var chunkErr error
	conn, connErr := file.SyscallConn()
	if connErr != nil {
		return 0, connErr
	}
	controlErr := conn.Control(func(fd uintptr) {
		count, chunkErr = syscall.Getdirentries(int(fd), buf, &base)
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
	namlenAt := unsafe.Offsetof(syscall.Dirent{}.Namlen)
	if inoAt+8 > uintptr(len(rec)) || typeAt >= uintptr(len(rec)) || namlenAt+2 > uintptr(len(rec)) {
		return stagingDirent{}, false
	}
	nameLen := int(binary.NativeEndian.Uint16(rec[namlenAt:]))
	if nameAt+uintptr(nameLen) > uintptr(len(rec)) {
		return stagingDirent{}, false
	}
	return stagingDirent{
		inode:   binary.NativeEndian.Uint64(rec[inoAt:]),
		regular: rec[typeAt] == syscall.DT_REG,
		name:    string(rec[nameAt : nameAt+uintptr(nameLen)]),
	}, true
}
