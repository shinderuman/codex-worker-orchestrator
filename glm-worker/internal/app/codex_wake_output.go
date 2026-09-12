package app

import (
	"bytes"
	"encoding/json"
	"io"
)

func writeCodexWakeJSON(w io.Writer, value any) (bool, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return false, err
	}
	data := buf.Bytes()
	n, err := w.Write(data)
	if n == len(data) {
		return true, nil
	}
	if err == nil {
		err = io.ErrShortWrite
	}
	return false, err
}
