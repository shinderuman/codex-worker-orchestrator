package controller

import (
	"archive/zip"
	"fmt"
	"io"
	"time"
)

func WriteEvidenceExportZip(target io.Writer, export EvidenceExport) error {
	writer := zip.NewWriter(target)
	for _, file := range export.Files {
		entry, err := writer.CreateHeader(&zip.FileHeader{
			Name:     file.Path,
			Method:   zip.Deflate,
			Modified: time.Unix(0, 0).UTC(),
		})
		if err != nil {
			return fmt.Errorf("stage evidence export entry %s: %w", file.Path, err)
		}
		if _, err := entry.Write(file.Data); err != nil {
			return fmt.Errorf("write evidence export entry %s: %w", file.Path, err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize evidence export archive: %w", err)
	}
	return nil
}
