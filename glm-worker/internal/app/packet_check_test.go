package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func writePacketCheckCandidate(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "packet.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runPacketCheck(t *testing.T, cmd Command) packetCheckVerdict {
	t.Helper()
	var stdout bytes.Buffer
	if err := printPacketCheck(cmd, &stdout); err != nil {
		t.Fatalf("printPacketCheck: %v", err)
	}
	var verdict packetCheckVerdict
	if err := json.Unmarshal(stdout.Bytes(), &verdict); err != nil {
		t.Fatalf("verdict JSONとしてdecodeできません: %v: %q", err, stdout.String())
	}
	return verdict
}

func workerCandidate(mutate func(map[string]any)) string {
	object := map[string]any{
		"status":               "IMPLEMENTED",
		"risk":                 "LOW",
		"summary":              "done",
		"requirement_coverage": "covered",
		"tests":                "pass",
		"unverified":           "none",
		"targets":              []string{"none"},
	}
	if mutate != nil {
		mutate(object)
	}
	data, err := json.Marshal(object)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestPacketCheckCommandParsesArguments(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		role    string
		payload string
		root    string
		wantErr bool
	}{
		{name: "file only defaults to worker role", args: []string{"--packet-check", "packet.json"}, role: packetCheckRoleWorker, payload: "packet.json"},
		{name: "reviewer role", args: []string{"--packet-check", "packet.json", "--role", "reviewer"}, role: packetCheckRoleReviewer, payload: "packet.json"},
		{name: "artifact root", args: []string{"--packet-check", "packet.json", "--artifact-root", "/tmp/artifacts"}, role: packetCheckRoleWorker, payload: "packet.json", root: "/tmp/artifacts"},
		{name: "missing file", args: []string{"--packet-check"}, wantErr: true},
		{name: "duplicate file", args: []string{"--packet-check", "a.json", "b.json"}, wantErr: true},
		{name: "unknown role", args: []string{"--packet-check", "packet.json", "--role", "boss"}, wantErr: true},
		{name: "unknown flag", args: []string{"--packet-check", "packet.json", "--strict", "1"}, wantErr: true},
		{name: "flag without value", args: []string{"--packet-check", "packet.json", "--artifact-root"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := packetCheckCommand(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("usage errorを期待しました: %#v", cmd)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cmd.Mode != ModePacketCheck || cmd.Role != tc.role || cmd.Payload != tc.payload || cmd.ArtifactRoot != tc.root {
				t.Fatalf("command = %#v", cmd)
			}
		})
	}
}

func TestPacketCheckAcceptsValidPacketAtJapaneseByteBoundary(t *testing.T) {
	path := writePacketCheckCandidate(t, workerCandidate(func(object map[string]any) {
		object["summary"] = strings.Repeat("あ", 512)
	}))
	verdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, Payload: path})
	if !verdict.Ok || verdict.Violation != "" {
		t.Fatalf("1536 bytesちょうどの日本語fieldは受理されるべき: %#v", verdict)
	}
}

func TestPacketCheckRejectsJapaneseFieldOverByteLimit(t *testing.T) {
	path := writePacketCheckCandidate(t, workerCandidate(func(object map[string]any) {
		object["summary"] = strings.Repeat("あ", 513)
	}))
	verdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, Payload: path})
	if verdict.Ok || !strings.Contains(verdict.Violation, "summary") || !strings.Contains(verdict.Violation, "1536 bytes") {
		t.Fatalf("UTF-8 byte超過の違反を報告すべき: %#v", verdict)
	}
}

func TestPacketCheckRejectsPacketOverTotalByteLimit(t *testing.T) {
	path := writePacketCheckCandidate(t, workerCandidate(func(object map[string]any) {
		object["summary"] = strings.Repeat("x", packet.MaxFieldBytes)
		object["requirement_coverage"] = strings.Repeat("x", packet.MaxFieldBytes)
		object["tests"] = strings.Repeat("x", packet.MaxFieldBytes)
		object["unverified"] = strings.Repeat("x", packet.MaxFieldBytes)
	}))
	verdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, Payload: path})
	if verdict.Ok || !strings.Contains(verdict.Violation, "結果全体") {
		t.Fatalf("全体上限超過を報告すべき: %#v", verdict)
	}
}

func TestPacketCheckReportsMissingRequiredField(t *testing.T) {
	path := writePacketCheckCandidate(t, workerCandidate(func(object map[string]any) {
		delete(object, "tests")
	}))
	verdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, Payload: path})
	if verdict.Ok || !strings.Contains(verdict.Violation, "必須field") {
		t.Fatalf("必須field欠落を報告すべき: %#v", verdict)
	}
}

func TestPacketCheckReportsMultilineField(t *testing.T) {
	path := writePacketCheckCandidate(t, workerCandidate(func(object map[string]any) {
		object["summary"] = "line1\nline2"
	}))
	verdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, Payload: path})
	if verdict.Ok || !strings.Contains(verdict.Violation, "改行") {
		t.Fatalf("改行違反を報告すべき: %#v", verdict)
	}
}

func TestPacketCheckReportsOversizeTargetElement(t *testing.T) {
	path := writePacketCheckCandidate(t, workerCandidate(func(object map[string]any) {
		object["targets"] = []string{strings.Repeat("glm-worker/internal/app/packet_check.go:", 40)}
	}))
	verdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, Payload: path})
	if verdict.Ok || !strings.Contains(verdict.Violation, "TARGETS/ARTIFACTSの各要素") {
		t.Fatalf("TARGETS要素上限違反を報告すべき: %#v", verdict)
	}
}

func TestPacketCheckValidatesArtifactsAgainstRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "report.txt")
	if err := os.WriteFile(inside, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(outside, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}

	insideVerdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, ArtifactRoot: root,
		Payload: writePacketCheckCandidate(t, workerCandidate(func(object map[string]any) {
			object["artifacts"] = []string{inside}
		}))})
	if !insideVerdict.Ok {
		t.Fatalf("artifact dir配下の実在fileは受理されるべき: %#v", insideVerdict)
	}

	outsideVerdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, ArtifactRoot: root,
		Payload: writePacketCheckCandidate(t, workerCandidate(func(object map[string]any) {
			object["artifacts"] = []string{outside}
		}))})
	if outsideVerdict.Ok || !strings.Contains(outsideVerdict.Violation, "ARTIFACTS") {
		t.Fatalf("artifact dir外のpathを拒否すべき: %#v", outsideVerdict)
	}
}

func TestPacketCheckRoleSelectsValidatorContract(t *testing.T) {
	reviewer := `{"status":"PASS","risk":"LOW","summary":"ok","requirement_coverage":"covered","invariants":"kept","test_evidence":"pass","issues":"none","residual_risk":"none","targets":["glm-worker/internal/app/packet_check.go"]}`
	path := writePacketCheckCandidate(t, reviewer)
	reviewerVerdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleReviewer, Payload: path})
	if !reviewerVerdict.Ok {
		t.Fatalf("reviewer契約の妥当なpacketは受理されるべき: %#v", reviewerVerdict)
	}
	workerVerdict := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, Payload: path})
	if workerVerdict.Ok || !strings.Contains(workerVerdict.Violation, "worker") {
		t.Fatalf("worker契約ではreviewer statusを拒否すべき: %#v", workerVerdict)
	}
}

func TestPacketCheckReportsUnparsableCandidate(t *testing.T) {
	malformed := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker,
		Payload: writePacketCheckCandidate(t, "{not json")})
	if malformed.Ok || !strings.Contains(malformed.Violation, "解析できません") {
		t.Fatalf("JSON解析違反を報告すべき: %#v", malformed)
	}
	missingStatus := runPacketCheck(t, Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker,
		Payload: writePacketCheckCandidate(t, `{"risk":"LOW"}`)})
	if missingStatus.Ok || !strings.Contains(missingStatus.Violation, "status") {
		t.Fatalf("status欠落を報告すべき: %#v", missingStatus)
	}
}

func TestPacketCheckMissingFileFailsClosed(t *testing.T) {
	var stdout bytes.Buffer
	err := printPacketCheck(Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker, Payload: filepath.Join(t.TempDir(), "absent.json")}, &stdout)
	if err == nil {
		t.Fatal("対象file欠落はerrorにすべき")
	}
	if stdout.Len() != 0 {
		t.Fatalf("失敗時にverdictを出力すべきではありません: %q", stdout.String())
	}
}
