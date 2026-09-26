from pathlib import Path

path = Path("glm-worker/internal/shadoweval/input.go")
text = path.read_text()
old = '''func eventEvidencePriority(record state.TaskEventRecord) int {
\tif record.IsError || validationHasCorrectnessSignal(record.Validation) {
\t\treturn 3
\t}
\thasValidation := record.Validation != nil
\thasOperation := len(record.SearchPaths) > 0
\tfor _, block := range record.Blocks {
\t\tif block.IsError {
\t\t\treturn 3
\t\t}
\t\tif block.OperationCategory != "" {
\t\t\thasOperation = true
\t\t}
\t\tif len(block.Validation) > 0 {
\t\t\thasValidation = true
\t\t}
\t\tfor _, observation := range block.Validation {
\t\t\tif validationResultHasCorrectnessSignal(observation.Result) {
\t\t\t\treturn 3
\t\t\t}
\t\t}
\t}
\tif hasValidation {
\t\treturn 2
\t}
\tif hasOperation {
\t\treturn 1
\t}
\treturn 0
}
'''
new = '''func eventEvidencePriority(record state.TaskEventRecord) int {
\tif record.IsError || validationHasCorrectnessSignal(record.Validation) {
\t\treturn 3
\t}
\tpriority := 0
\tif record.Validation != nil {
\t\tpriority = 2
\t} else if len(record.SearchPaths) > 0 {
\t\tpriority = 1
\t}
\tfor _, block := range record.Blocks {
\t\tblockPriority := eventBlockPriority(block)
\t\tif blockPriority == 3 {
\t\t\treturn 3
\t\t}
\t\tif blockPriority > priority {
\t\t\tpriority = blockPriority
\t\t}
\t}
\treturn priority
}

func eventBlockPriority(block state.TaskBlockSummary) int {
\tif block.IsError {
\t\treturn 3
\t}
\tfor _, observation := range block.Validation {
\t\tif validationResultHasCorrectnessSignal(observation.Result) {
\t\t\treturn 3
\t\t}
\t}
\tif len(block.Validation) > 0 {
\t\treturn 2
\t}
\tif block.OperationCategory != "" {
\t\treturn 1
\t}
\treturn 0
}
'''
if text.count(old) != 1:
    raise SystemExit(f"expected one eventEvidencePriority, found {text.count(old)}")
path.write_text(text.replace(old, new, 1))
