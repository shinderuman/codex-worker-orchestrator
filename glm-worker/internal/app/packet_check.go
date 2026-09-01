package app

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type packetCheckVerdict struct {
	Ok        bool   `json:"ok"`
	Violation string `json:"violation,omitempty"`
}

const packetCheckRoleWorker = "worker"

const packetCheckRoleReviewer = "reviewer"

const packetCheckUsage = "usage: glm-worker --packet-check <packet.json> [--role worker|reviewer] [--artifact-root <dir>]"

func packetCheckCommand(args []string) (Command, error) {
	command := Command{Mode: ModePacketCheck, Role: packetCheckRoleWorker}
	fileSeen := false
	for index := 1; index < len(args); index++ {
		handled, err := applyPacketCheckOption(&command, args, index)
		if err != nil {
			return Command{}, err
		}
		if handled {
			index++
			continue
		}
		if fileSeen || !packetCheckFileArgument(args[index]) {
			return Command{}, usageError("%s", packetCheckUsage)
		}
		command.Payload = args[index]
		fileSeen = true
	}
	if !fileSeen {
		return Command{}, usageError("%s", packetCheckUsage)
	}
	return command, nil
}

func applyPacketCheckOption(command *Command, args []string, index int) (bool, error) {
	if index+1 >= len(args) {
		return false, nil
	}
	value := args[index+1]
	switch args[index] {
	case "--role":
		if value != packetCheckRoleWorker && value != packetCheckRoleReviewer {
			return true, usageError("%s", packetCheckUsage)
		}
		command.Role = value
		return true, nil
	case "--artifact-root":
		if value == "" {
			return true, usageError("%s", packetCheckUsage)
		}
		command.ArtifactRoot = value
		return true, nil
	}
	return false, nil
}

func packetCheckFileArgument(argument string) bool {
	return argument != "" && !strings.HasPrefix(argument, "--")
}

func printPacketCheck(cmd Command, stdout io.Writer) error {
	data, err := os.ReadFile(cmd.Payload)
	if err != nil {
		return fmt.Errorf("packet-checkの対象fileを読めません: %s: %w", cmd.Payload, err)
	}
	result, parseErr := packet.ParseStructured(data)
	if parseErr != nil {
		return writeJSON(stdout, packetCheckVerdict{Ok: false, Violation: parseErr.Error()})
	}
	if err := packetCheckViolation(cmd, result); err != nil {
		return writeJSON(stdout, packetCheckVerdict{Ok: false, Violation: err.Error()})
	}
	return writeJSON(stdout, packetCheckVerdict{Ok: true})
}

func packetCheckViolation(cmd Command, result packet.Result) error {
	var err error
	if cmd.Role == packetCheckRoleReviewer {
		err = packet.ValidateReviewerResult(result)
	} else {
		err = packet.ValidateWorkerResult(result)
	}
	if err != nil {
		return err
	}
	if cmd.ArtifactRoot != "" {
		return packet.ValidateArtifacts(result.Artifacts, cmd.ArtifactRoot)
	}
	return nil
}
