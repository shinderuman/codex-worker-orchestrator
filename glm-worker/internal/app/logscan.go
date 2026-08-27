package app

import (
	"bufio"
	"os"
)

func scanLogRecords[T any](path string, parse func([]byte) (T, error)) ([]T, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var records []T
	skipped := 0
	for scanner.Scan() {
		record, err := parse(scanner.Bytes())
		if err != nil {
			skipped++
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return records, skipped, err
	}
	return records, skipped, nil
}
