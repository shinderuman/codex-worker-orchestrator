package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

type claudeLocalAPIFixture struct {
	url     string
	command string
}

type claudeFixtureBlock struct {
	complete map[string]any
	start    map[string]any
	delta    map[string]any
}

func startClaudeLocalAPIFixture(t *testing.T, command string) *claudeLocalAPIFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &claudeLocalAPIFixture{url: "http://" + listener.Addr().String(), command: command}
	server := &http.Server{Handler: http.HandlerFunc(fixture.serveHTTP)}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return fixture
}

func (f *claudeLocalAPIFixture) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	if strings.HasSuffix(request.URL.Path, "/count_tokens") {
		writeClaudeFixtureJSON(writer, map[string]any{"input_tokens": 1})
		return
	}
	text := string(body)
	stream := strings.Contains(text, `"stream":true`) || strings.Contains(text, `"stream": true`)
	blocks := []claudeFixtureBlock{claudeFixtureToolUseBlock("toolu_live_dial", "Bash", map[string]any{"command": f.command})}
	stopReason := "tool_use"
	if strings.Contains(text, `"tool_result"`) {
		blocks = []claudeFixtureBlock{claudeFixtureTextBlock("done")}
		stopReason = "end_turn"
	}
	f.writeAssistantMessage(writer, stream, stopReason, blocks)
}

func (*claudeLocalAPIFixture) writeAssistantMessage(writer http.ResponseWriter, stream bool, stopReason string, blocks []claudeFixtureBlock) {
	if !stream {
		content := make([]map[string]any, 0, len(blocks))
		for _, block := range blocks {
			content = append(content, block.complete)
		}
		writeClaudeFixtureJSON(writer, map[string]any{
			"id": "msg_fixture", "type": "message", "role": "assistant", "model": "claude-sonnet-4-5",
			"content": content, "stop_reason": stopReason, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 2},
		})
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, flushable := writer.(http.Flusher)
	emit := func(event string, payload map[string]any) {
		data, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return
		}
		_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, data)
		if flushable {
			flusher.Flush()
		}
	}
	emit("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_fixture", "type": "message", "role": "assistant", "model": "claude-sonnet-4-5",
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 0},
	}})
	for index, block := range blocks {
		emit("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": block.start})
		emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": block.delta})
		emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
	}
	emit("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 2}})
	emit("message_stop", map[string]any{"type": "message_stop"})
}

func claudeFixtureTextBlock(textValue string) claudeFixtureBlock {
	return claudeFixtureBlock{
		complete: map[string]any{"type": "text", "text": textValue},
		start:    map[string]any{"type": "text", "text": ""},
		delta:    map[string]any{"type": "text_delta", "text": textValue},
	}
}

func claudeFixtureToolUseBlock(id, name string, input map[string]any) claudeFixtureBlock {
	return claudeFixtureBlock{
		complete: map[string]any{"type": "tool_use", "id": id, "name": name, "input": input},
		start:    map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}},
		delta:    map[string]any{"type": "input_json_delta", "partial_json": marshalClaudeFixtureJSON(input)},
	}
}

func marshalClaudeFixtureJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func writeClaudeFixtureJSON(writer http.ResponseWriter, payload map[string]any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(payload)
}
