from pathlib import Path

path = Path("glm-worker/internal/packet/contract.go")
text = path.read_text()
text = text.replace("type statusContract struct {", "type packetStatusContract struct {")
text = text.replace("var statusContracts = map[Status]statusContract{", "var packetStatusContracts = map[Status]packetStatusContract{")
text = text.replace("statusContracts[status]", "packetStatusContracts[status]")
text = text.replace("statusContracts[contract.strictRequiredStatus]", "packetStatusContracts[contract.strictRequiredStatus]")
text = text.replace("contract statusContract", "contract packetStatusContract")
text = text.replace("statusContracts[result.Status]", "packetStatusContracts[result.Status]")
path.write_text(text)
