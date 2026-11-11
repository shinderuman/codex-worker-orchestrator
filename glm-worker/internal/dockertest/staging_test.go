package dockertest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseStagingRequestRejectsProtocolViolations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{name: "unknown-field", body: `{"schema_version":1,"token":"100","args":["./..."],"root":"/"}`},
		{name: "schema-mismatch", body: `{"schema_version":2,"token":"100","args":["./..."]}`},
		{name: "missing-token", body: `{"schema_version":1,"args":["./..."]}`},
		{name: "invalid-token", body: `{"schema_version":1,"token":"a/b","args":["./..."]}`},
		{name: "empty-args", body: `{"schema_version":1,"token":"100","args":[]}`},
		{name: "empty-arg-element", body: `{"schema_version":1,"token":"100","args":[""]}`},
		{name: "terminator-arg", body: `{"schema_version":1,"token":"100","args":["--"]}`},
		{name: "args-not-array", body: `{"schema_version":1,"token":"100","args":"./..."}`},
		{name: "trailing-json", body: `{"schema_version":1,"token":"100","args":["./..."]}{}{"schema_version":1}`},
		{name: "non-object", body: `[]`},
	}
	for _, tc := range cases {
		if _, err := ParseStagingRequest([]byte(tc.body)); err == nil {
			t.Fatalf("%s: requestが受理されました", tc.name)
		}
	}
	request, err := ParseStagingRequest([]byte(`{"schema_version":1,"token":"100","args":["./...","-run","^TestX$","-count=1"]}`))
	if err != nil {
		t.Fatalf("正当requestが拒否されました: %v", err)
	}
	if request.Token != "100" || len(request.Args) != 4 {
		t.Fatalf("正当requestのparse結果 = %#v", request)
	}
}

func TestStagingRequestFileNameClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		requestOK bool
		token     string
		tokenOK   bool
	}{
		{name: "valid", requestOK: true, token: "1720000000000000000", tokenOK: true},
		{name: "response-name", requestOK: false},
		{name: "dotfile", requestOK: false},
		{name: "missing-suffix", requestOK: false},
		{name: "empty-token", requestOK: true, tokenOK: false},
		{name: "traversal-token", requestOK: true, token: "../escape", tokenOK: false},
	}
	for _, tc := range cases {
		name := "request-" + tc.token + ".json"
		switch tc.name {
		case "response-name":
			name = "response-1720000000000000000.json"
		case "dotfile":
			name = ".request-1720000000000000000.json.tmp"
		case "missing-suffix":
			name = "request-1720000000000000000"
		case "empty-token":
			name = "request-.json"
		}
		if got := IsStagingRequestFileName(name); got != tc.requestOK {
			t.Fatalf("%s: IsStagingRequestFileName = %v want %v", tc.name, got, tc.requestOK)
		}
		token, ok := StagingTokenFromRequestFileName(name)
		if ok != tc.tokenOK || (ok && token != tc.token) {
			t.Fatalf("%s: token = %q ok = %v want %q %v", tc.name, token, ok, tc.token, tc.tokenOK)
		}
	}
}

func TestParseStagingResponseRejectsProtocolViolations(t *testing.T) {
	t.Parallel()
	if _, err := ParseStagingResponse([]byte(`{"schema_version":1,"token":"100","exit_code":0,"stdout":"","stderr":""}`)); err != nil {
		t.Fatalf("正当responseが拒否されました: %v", err)
	}
	for name, body := range map[string]string{
		"unknown-field":   `{"schema_version":1,"token":"100","exit_code":0,"promote":true}`,
		"schema-mismatch": `{"schema_version":3,"token":"100","exit_code":0}`,
		"trailing-value":  `{"schema_version":1,"token":"100","exit_code":0}1`,
	} {
		if _, err := ParseStagingResponse([]byte(body)); err == nil {
			t.Fatalf("%s: responseが受理されました", name)
		}
	}
}

func newStagingClientFixture(t *testing.T) (*StagingClient, string, string) {
	t.Helper()
	temp := t.TempDir()
	staging := filepath.Join(temp, "worker", "docker-test")
	responses := filepath.Join(temp, "docker-test-responses")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(responses, 0o700); err != nil {
		t.Fatal(err)
	}
	client := &StagingClient{
		StagingDir:   staging,
		ResponseDir:  responses,
		PollInterval: 10 * time.Millisecond,
		Timeout:      120 * time.Millisecond,
		Now:          func() time.Time { return time.Unix(0, 1720000000000000000) },
	}
	return client, staging, responses
}

func stagingClientToken() string {
	return "1720000000000000000"
}

func TestStagingClientFailsClosedWithoutOwnerResponse(t *testing.T) {
	t.Parallel()
	client, staging, _ := newStagingClientFixture(t)
	var stdout, stderr strings.Builder
	code := client.Run(context.Background(), []string{"./..."}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("response欠落時の終了 = code %d out %q err %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "staging responseが締切内に得られませんでした") {
		t.Fatalf("response欠落のfail-closed出力 = %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "goquality") || strings.Contains(stderr.String(), "goquality-diagnostic") {
		t.Fatalf("response欠落時に証跡行が捏造されました out %q err %q", stdout.String(), stderr.String())
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("fail-closed後にrequestが残っています: %#v", entries)
	}
}

func TestStagingClientCancelsWithoutPromotingResult(t *testing.T) {
	t.Parallel()
	client, staging, _ := newStagingClientFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	var stdout, stderr strings.Builder
	code := client.Run(ctx, []string{"./..."}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("取消時の終了 = code %d", code)
	}
	if !strings.Contains(stderr.String(), "取消") {
		t.Fatalf("取消の出力 = %q", stderr.String())
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("取消後にrequestが残っています: %#v", entries)
	}
}

func TestStagingClientRejectsInvalidArgsBeforeStaging(t *testing.T) {
	t.Parallel()
	client, staging, _ := newStagingClientFixture(t)
	var stdout, stderr strings.Builder
	if code := client.Run(context.Background(), []string{"./...", "--"}, &stdout, &stderr); code != 1 {
		t.Fatalf("不正argsの終了 = code %d", code)
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("不正argsでstagingへ書き込みました: %#v", entries)
	}
}

func TestStagingClientRejectsDuplicateToken(t *testing.T) {
	t.Parallel()
	client, staging, _ := newStagingClientFixture(t)
	if err := os.WriteFile(filepath.Join(staging, StagingRequestFileName(stagingClientToken())), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := client.Run(context.Background(), []string{"./..."}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "tokenが重複") {
		t.Fatalf("同一token再利用 = code %d err %q", code, stderr.String())
	}
}

func TestStagingClientReproducesOwnerResponse(t *testing.T) {
	t.Parallel()
	client, _, responses := newStagingClientFixture(t)
	token := stagingClientToken()
	body := `{"schema_version":1,"token":"` + token + `","exit_code":3,"stdout":"container-out\n","stderr":"goquality-diagnostic {\"form\":\"diagnostic-go-test\",\"child_exit\":3,\"exit\":3}\n"}`
	if err := os.WriteFile(filepath.Join(responses, StagingResponseFileName(token)), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := client.Run(context.Background(), []string{"./internal/app"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("owner終了codeの再現 = code %d out %q err %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "container-out") || !strings.Contains(stderr.String(), "goquality-diagnostic") {
		t.Fatalf("owner出力の再現 = out %q err %q", stdout.String(), stderr.String())
	}
}

func TestStagingClientIgnoresForeignResponses(t *testing.T) {
	t.Parallel()
	client, _, responses := newStagingClientFixture(t)
	token := stagingClientToken()
	foreign := `{"schema_version":1,"token":"9999999999999999999","exit_code":0,"stdout":"forged\n","stderr":""}`
	if err := os.WriteFile(filepath.Join(responses, StagingResponseFileName(token)), []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := client.Run(context.Background(), []string{"./..."}, &stdout, &stderr)
	if code != 1 || strings.Contains(stdout.String(), "forged") {
		t.Fatalf("token不一致responseの取り込み = code %d out %q", code, stdout.String())
	}
}

func TestStagingClientFailsImmediatelyOnOversizedResponse(t *testing.T) {
	client, staging, responses := newStagingClientFixture(t)
	client.Timeout = 10 * time.Second
	client.PollInterval = time.Millisecond
	token := stagingClientToken()
	path := filepath.Join(responses, StagingResponseFileName(token))
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// A sparse oversized response exercises the real 64 MiB reader boundary.
	if err := file.Truncate(StagingMaxResponseBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	start := time.Now()
	code := client.Run(context.Background(), []string{"./..."}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "符号化後size上限") {
		t.Fatalf("過大responseの即時拒否 = code %d out %q err %q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "締切内") || stdout.Len() != 0 {
		t.Fatalf("過大responseがpending/成功出力になりました: out %q err %q", stdout.String(), stderr.String())
	}
	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Fatalf("過大response拒否に%vかかりました（timeout待機の疑い）", elapsed)
	}
	if _, err := os.Stat(filepath.Join(staging, StagingRequestFileName(token))); !os.IsNotExist(err) {
		t.Fatalf("fail-closed後にrequestが残っています: %v", err)
	}
}

func TestStagingClientKeepsPartialResponsePendingAndCancellable(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		client, _, responses := newStagingClientFixture(t)
		token := stagingClientToken()
		if err := os.WriteFile(filepath.Join(responses, StagingResponseFileName(token)), []byte(`{"schema_version":1,"token":"`+token+`"`), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		code := client.Run(context.Background(), []string{"./..."}, &stdout, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "締切内に得られませんでした") || strings.Contains(stderr.String(), "size上限") {
			t.Fatalf("部分responseのdeadline待機 = code %d out %q err %q", code, stdout.String(), stderr.String())
		}
	})
	t.Run("cancel", func(t *testing.T) {
		client, _, responses := newStagingClientFixture(t)
		client.Timeout = 5 * time.Second
		token := stagingClientToken()
		if err := os.WriteFile(filepath.Join(responses, StagingResponseFileName(token)), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		var stdout, stderr strings.Builder
		code := client.Run(ctx, []string{"./..."}, &stdout, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "取消しました") || strings.Contains(stderr.String(), "size上限") {
			t.Fatalf("部分responseのcancel = code %d out %q err %q", code, stdout.String(), stderr.String())
		}
	})
}
