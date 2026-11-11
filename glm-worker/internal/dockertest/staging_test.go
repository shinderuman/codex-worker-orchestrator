package dockertest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		StagingDir:    staging,
		ResponseDir:   responses,
		PollInterval:  10 * time.Millisecond,
		Timeout:       120 * time.Millisecond,
		generateToken: func() (string, error) { return stagingClientToken(), nil },
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

func publishTestStagingResponseAfterRequest(staging, responses, token, body string) <-chan error {
	done := make(chan error, 1)
	go func() {
		final := filepath.Join(staging, StagingRequestFileName(token))
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Lstat(final); err == nil {
				done <- os.WriteFile(filepath.Join(responses, StagingResponseFileName(token)), []byte(body), 0o600)
				return
			} else if !os.IsNotExist(err) {
				done <- err
				return
			}
			time.Sleep(time.Millisecond)
		}
		done <- errors.New("staging request was never published")
	}()
	return done
}

func TestStagingClientReproducesOwnerResponse(t *testing.T) {
	t.Parallel()
	client, staging, responses := newStagingClientFixture(t)
	client.Timeout = 2 * time.Second
	token := stagingClientToken()
	body := `{"schema_version":1,"token":"` + token + `","exit_code":3,"stdout":"container-out\n","stderr":"goquality-diagnostic {\"form\":\"diagnostic-go-test\",\"child_exit\":3,\"exit\":3}\n"}`
	written := publishTestStagingResponseAfterRequest(staging, responses, token, body)
	var stdout, stderr strings.Builder
	code := client.Run(context.Background(), []string{"./internal/app"}, &stdout, &stderr)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if code != 3 {
		t.Fatalf("owner終了codeの再現 = code %d out %q err %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "container-out") || !strings.Contains(stderr.String(), "goquality-diagnostic") {
		t.Fatalf("owner出力の再現 = out %q err %q", stdout.String(), stderr.String())
	}
}

func TestStagingClientIgnoresForeignResponses(t *testing.T) {
	t.Parallel()
	client, staging, responses := newStagingClientFixture(t)
	client.Timeout = 500 * time.Millisecond
	token := stagingClientToken()
	foreign := `{"schema_version":1,"token":"9999999999999999999","exit_code":0,"stdout":"forged\n","stderr":""}`
	written := publishTestStagingResponseAfterRequest(staging, responses, token, foreign)
	var stdout, stderr strings.Builder
	code := client.Run(context.Background(), []string{"./..."}, &stdout, &stderr)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if code != 1 || strings.Contains(stdout.String(), "forged") {
		t.Fatalf("token不一致responseの取り込み = code %d out %q", code, stdout.String())
	}
}

func TestStagingClientRandomTokensAcrossConcurrentCalls(t *testing.T) {
	t.Parallel()
	client, staging, responses := newStagingClientFixture(t)
	client.generateToken = nil
	client.Timeout = 3 * time.Second
	client.PollInterval = 5 * time.Millisecond
	other := &StagingClient{
		StagingDir:   staging,
		ResponseDir:  responses,
		Timeout:      3 * time.Second,
		PollInterval: 5 * time.Millisecond,
	}
	type result struct {
		code int
		out  string
		err  string
	}
	var wg sync.WaitGroup
	results := make([]result, 2)
	args := []string{"./internal/runner", "./internal/dockertest"}
	for i := range args {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var out, errOut strings.Builder
			target := client
			if i == 1 {
				target = other
			}
			results[i].code = target.Run(context.Background(), []string{args[i]}, &out, &errOut)
			results[i].out, results[i].err = out.String(), errOut.String()
		}(i)
	}

	deadline := time.Now().Add(2 * time.Second)
	var published map[string]StagingRequest
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(staging)
		if err != nil {
			t.Fatal(err)
		}
		current := make(map[string]StagingRequest, 2)
		for _, entry := range entries {
			if !IsStagingRequestFileName(entry.Name()) {
				continue
			}
			token, ok := StagingTokenFromRequestFileName(entry.Name())
			if !ok {
				t.Fatalf("invalid published name %q", entry.Name())
			}
			raw, err := os.ReadFile(filepath.Join(staging, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			request, err := ParseStagingRequest(raw)
			if err != nil || request.Token != token {
				t.Fatalf("bad published request: %v %#v", err, request)
			}
			current[token] = request
		}
		if len(current) == 2 {
			published = current
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(published) != 2 {
		t.Fatalf("simultaneous clients did not publish distinct requests: %#v", published)
	}
	for token, request := range published {
		if len(request.Args) != 1 {
			t.Fatalf("unexpected args: %#v", request)
		}
		code := 0
		out := "runner-result"
		if request.Args[0] == "./internal/dockertest" {
			code = 7
			out = "dockertest-result"
		} else if request.Args[0] != "./internal/runner" {
			t.Fatalf("unexpected package: %#v", request.Args)
		}
		raw, err := json.Marshal(StagingResponse{SchemaVersion: StagingSchemaVersion, Token: token, ExitCode: code, Stdout: out})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(responses, StagingResponseFileName(token)), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	if results[0].code != 0 || results[0].out != "runner-result" || results[0].err != "" {
		t.Fatalf("runner response confused: %#v", results[0])
	}
	if results[1].code != 7 || results[1].out != "dockertest-result" || results[1].err != "" {
		t.Fatalf("dockertest response confused: %#v", results[1])
	}
}

func TestStagingClientFixedTokenCollisionNeverReplacesRequest(t *testing.T) {
	t.Parallel()
	client, staging, _ := newStagingClientFixture(t)
	other := *client
	requests := []StagingRequest{
		{SchemaVersion: StagingSchemaVersion, Token: stagingClientToken(), Args: []string{"./internal/app"}},
		{SchemaVersion: StagingSchemaVersion, Token: stagingClientToken(), Args: []string{"./internal/runner"}},
	}
	start := make(chan struct{})
	errs := make([]error, len(requests))
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			target := client
			if i == 1 {
				target = &other
			}
			errs[i] = target.writeRequest(requests[i])
		}(i)
	}
	close(start)
	wg.Wait()
	successes := 0
	var accepted StagingRequest
	for i, err := range errs {
		if err == nil {
			successes++
			accepted = requests[i]
		} else if !strings.Contains(err.Error(), "tokenが重複") {
			t.Fatalf("collision failure %d: %v", i, err)
		}
	}
	if successes != 1 {
		t.Fatalf("fixed-token writers accepted %d requests, errors %#v", successes, errs)
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != StagingRequestFileName(stagingClientToken()) {
		t.Fatalf("collision created or retained multiple files: %#v", entries)
	}
	raw, err := os.ReadFile(filepath.Join(staging, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseStagingRequest(raw)
	if err != nil || got.Token != accepted.Token || len(got.Args) != 1 || got.Args[0] != accepted.Args[0] {
		t.Fatalf("published request was overwritten: %#v, error %v", got, err)
	}
}

func TestStagingClientNeverClobbersExistingFinalOrTemp(t *testing.T) {
	t.Parallel()
	t.Run("existing-final", func(t *testing.T) {
		client, staging, _ := newStagingClientFixture(t)
		final := filepath.Join(staging, StagingRequestFileName(stagingClientToken()))
		original := []byte("preexisting-final")
		if err := os.WriteFile(final, original, 0o600); err != nil {
			t.Fatal(err)
		}
		err := client.writeRequest(StagingRequest{SchemaVersion: StagingSchemaVersion, Token: stagingClientToken(), Args: []string{"./internal/app"}})
		if err == nil || !strings.Contains(err.Error(), "tokenが重複") {
			t.Fatalf("existing final was accepted: %v", err)
		}
		raw, err := os.ReadFile(final)
		if err != nil || string(raw) != string(original) {
			t.Fatalf("existing final changed: %q / %v", raw, err)
		}
		if _, err := os.Lstat(filepath.Join(staging, ".request-"+stagingClientToken()+".tmp")); !os.IsNotExist(err) {
			t.Fatalf("owned temp was not cleaned: %v", err)
		}
	})
	t.Run("existing-temp", func(t *testing.T) {
		client, staging, _ := newStagingClientFixture(t)
		temp := filepath.Join(staging, ".request-"+stagingClientToken()+".tmp")
		original := []byte("preexisting-temp")
		if err := os.WriteFile(temp, original, 0o600); err != nil {
			t.Fatal(err)
		}
		err := client.writeRequest(StagingRequest{SchemaVersion: StagingSchemaVersion, Token: stagingClientToken(), Args: []string{"./internal/runner"}})
		if err == nil || !strings.Contains(err.Error(), "tokenが重複") {
			t.Fatalf("existing temp was accepted: %v", err)
		}
		raw, err := os.ReadFile(temp)
		if err != nil || string(raw) != string(original) {
			t.Fatalf("preexisting temp was modified or removed: %q / %v", raw, err)
		}
		if _, err := os.Lstat(filepath.Join(staging, StagingRequestFileName(stagingClientToken()))); !os.IsNotExist(err) {
			t.Fatalf("request was incorrectly published: %v", err)
		}
	})
}

func TestStagingClientDoesNotCleanUpUnownedTempOnTimeout(t *testing.T) {
	t.Parallel()
	client, staging, _ := newStagingClientFixture(t)
	var stderr strings.Builder
	request := StagingRequest{SchemaVersion: StagingSchemaVersion, Token: stagingClientToken(), Args: []string{"./..."}}
	if err := client.writeRequest(request); err != nil {
		t.Fatal(err)
	}
	temp := filepath.Join(staging, ".request-"+stagingClientToken()+".tmp")
	if err := os.WriteFile(temp, []byte("other-writer"), 0o600); err != nil {
		t.Fatal(err)
	}
	response, ok := client.awaitResponse(context.Background(), request, &stderr)
	if ok || response.Token != "" {
		t.Fatalf("unexpected response: %#v %v", response, ok)
	}
	client.removeRequest(stagingClientToken())
	raw, err := os.ReadFile(temp)
	if err != nil || string(raw) != "other-writer" {
		t.Fatalf("timeout cleanup removed unowned temp: %q %v", raw, err)
	}
}

func TestStagingClientFailsClosedOnTokenGenerationError(t *testing.T) {
	t.Parallel()
	client, staging, _ := newStagingClientFixture(t)
	client.generateToken = func() (string, error) { return "", errors.New("entropy unavailable") }
	var stdout, stderr strings.Builder
	if code := client.Run(context.Background(), []string{"./..."}, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "entropy unavailable") || stdout.Len() != 0 {
		t.Fatalf("token generation error was not rejected: code %d out %q err %q", code, stdout.String(), stderr.String())
	}
	entries, err := os.ReadDir(staging)
	if err != nil || len(entries) != 0 {
		t.Fatalf("token generation error published request: %#v %v", entries, err)
	}
}

func TestStagingClientRejectsReusedTokenWithStaleOwnerResponse(t *testing.T) {
	t.Parallel()
	client, staging, responses := newStagingClientFixture(t)
	token := stagingClientToken()
	stale := StagingResponse{SchemaVersion: StagingSchemaVersion, Token: token, ExitCode: 0, Stdout: "unrelated-success"}
	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(responses, StagingResponseFileName(token)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := client.Run(context.Background(), []string{"./internal/runner"}, &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "tokenが重複") {
		t.Fatalf("stale response was reused: code %d out %q err %q", code, stdout.String(), stderr.String())
	}
	entries, err := os.ReadDir(staging)
	if err != nil || len(entries) != 0 {
		t.Fatalf("stale response token published request: %#v %v", entries, err)
	}
}
