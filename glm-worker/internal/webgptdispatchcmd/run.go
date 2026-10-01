package webgptdispatchcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/webgptdispatch"
)

type transportResult struct {
	MailboxIssue   int    `json:"mailbox_issue"`
	RequestID      string `json:"request_id"`
	Operation      string `json:"operation"`
	ControlSHA     string `json:"control_sha"`
	TargetBranch   string `json:"target_branch"`
	TargetSHA      string `json:"target_sha"`
	ValidationMode string `json:"validation_mode,omitempty"`
	PackageScope   string `json:"package_scope,omitempty"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	switch args[0] {
	case "fields":
		return runFields(args[1:], stderr)
	case "mailbox":
		return runMailbox(args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, usage())
		return 2
	}
}

func runFields(args []string, stderr io.Writer) int {
	if len(args) != 5 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	request := webgptdispatch.OperationRequest{
		Operation:       args[0],
		TargetBranch:    args[1],
		ExpectedHeadSHA: args[2],
		ValidationMode:  args[3],
		PackageScope:    args[4],
	}
	if err := webgptdispatch.ValidateOperation(request); err != nil {
		fmt.Fprintln(stderr, webgptdispatch.ValidationCode(err))
		return 1
	}
	return 0
}

func runMailbox(args []string, stdout, stderr io.Writer) int {
	if len(args) != 6 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	bodyPath, issueText, actor := args[0], args[1], args[2]
	controlSHA, githubOutputPath, resultPath := args[3], args[4], args[5]
	result := transportResult{Status: "rejected"}

	issueNumber, err := strconv.Atoi(issueText)
	if err != nil || issueNumber != webgptdispatch.MailboxIssueNumber {
		return reject(resultPath, result, "invalid_mailbox_issue", stderr)
	}
	result.MailboxIssue = issueNumber
	if actor != webgptdispatch.AuthorizedActor {
		return reject(resultPath, result, "unauthorized_actor", stderr)
	}
	normalizedControlSHA, err := webgptdispatch.NormalizeSHA(controlSHA)
	if err != nil {
		return reject(resultPath, result, "invalid_control_sha", stderr)
	}
	result.ControlSHA = normalizedControlSHA

	body, err := os.Open(bodyPath)
	if err != nil {
		return reject(resultPath, result, "mailbox_body_unavailable", stderr)
	}
	request, parseErr := webgptdispatch.ParseMailbox(body)
	closeErr := body.Close()
	if parseErr != nil {
		return reject(resultPath, result, webgptdispatch.ValidationCode(parseErr), stderr)
	}
	if closeErr != nil {
		return reject(resultPath, result, "mailbox_body_close_failed", stderr)
	}
	result.RequestID = request.RequestID
	result.Operation = request.Operation
	result.TargetBranch = request.TargetBranch
	result.TargetSHA = request.ExpectedHeadSHA
	if request.ValidationMode != nil {
		result.ValidationMode = *request.ValidationMode
	}
	if request.PackageScope != nil {
		result.PackageScope = *request.PackageScope
	}
	result.Status = "accepted"

	if err := appendOutputs(githubOutputPath, result); err != nil {
		return reject(resultPath, result, "github_output_failed", stderr)
	}
	if err := writeResult(resultPath, result); err != nil {
		fmt.Fprintln(stderr, "transport_result_failed")
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "stdout_result_failed")
		return 1
	}
	return 0
}

func reject(path string, result transportResult, code string, stderr io.Writer) int {
	result.Status = "rejected"
	result.Error = code
	if err := writeResult(path, result); err != nil {
		fmt.Fprintln(stderr, "transport_result_failed")
		return 1
	}
	fmt.Fprintln(stderr, code)
	return 1
}

func appendOutputs(path string, result transportResult) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	pairs := [][2]string{
		{"request_id", result.RequestID},
		{"operation", result.Operation},
		{"target_branch", result.TargetBranch},
		{"expected_head_sha", result.TargetSHA},
		{"validation_mode", result.ValidationMode},
		{"package_scope", result.PackageScope},
	}
	for _, pair := range pairs {
		if _, err := fmt.Fprintf(file, "%s=%s\n", pair[0], pair[1]); err != nil {
			return err
		}
	}
	return file.Close()
}

func writeResult(path string, result transportResult) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encodeErr := encoder.Encode(result)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func usage() string {
	return "usage: web-gpt-dispatch-request fields <operation> <target-branch> <expected-head-sha> <validation-mode> <package-scope> | mailbox <body-file> <issue-number> <actor> <control-sha> <github-output> <result-json>"
}
