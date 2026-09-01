from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    if old not in text:
        raise SystemExit(f"replacement target missing in {path}:\n{old}")
    file.write_text(text.replace(old, new, 1))


# result.go: consume the canonical machine contract instead of owning parallel lists.
result_path = Path("glm-worker/internal/packet/result.go")
text = result_path.read_text()
text = text.replace('''type contractField struct {\n\tmachine string\n\tvalue   func(Result) string\n}\n\n''', '', 1)
text = text.replace('''const (\n\tStatusImplemented      Status = "IMPLEMENTED"\n\tStatusNeedsSolDecision Status = "NEEDS_SOL_DECISION"\n\tStatusPass             Status = "PASS"\n\tStatusFixRequired      Status = "FIX_REQUIRED"\n\tStatusNeedsSolReview   Status = "NEEDS_SOL_REVIEW"\n)\n\nconst (\n\tRiskLow  Risk = "LOW"\n\tRiskHigh Risk = "HIGH"\n)\n\nconst (\n\tParentValidationGoTest     = "go-test"\n\tParentValidationGoTestRace = "go-test-race"\n)\n\n''', '', 1)
start = text.index("var implementedContractFields")
end = text.index("func (e *mismatchError) Error()", start)
text = text[:start] + text[end:]
start = text.index("func (r Result) contractFields()")
end = text.index("func (r Result) ParentValidationRequest()", start)
text = text[:start] + text[end:]
old_machine = '''func (r Result) MachineJSON() ([]byte, error) {\n\tobject := map[string]any{\n\t\t"status": string(r.Status),\n\t\t"risk":   string(r.Risk),\n\t}\n\tfor _, field := range r.contractFields() {\n\t\tif value := field.value(r); value != "" {\n\t\t\tobject[field.machine] = value\n\t\t}\n\t}\n\tif r.Status == StatusImplemented && r.ParentValidation != "" {\n\t\tobject["parent_validation"] = r.ParentValidation\n\t\tobject["parent_validation_working_dir"] = r.ParentValidationWorkingDir\n\t}\n\tif r.Status == StatusImplemented && r.ParentValidationEvidence != nil {\n\t\tobject["parent_validation_evidence"] = r.ParentValidationEvidence\n\t}\n\tif len(r.Targets) > 0 {\n\t\tobject["targets"] = r.Targets\n\t}\n\tif len(r.Artifacts) > 0 {\n\t\tobject["artifacts"] = r.Artifacts\n\t}\n'''
new_machine = '''func (r Result) MachineJSON() ([]byte, error) {\n\tobject := map[string]any{\n\t\tstring(fieldStatus): string(r.Status),\n\t\tstring(fieldRisk):   string(r.Risk),\n\t}\n\tfor _, field := range resultFieldsForStatus(r.Status) {\n\t\tif value := machineFieldValue(r, field); value != "" {\n\t\t\tobject[string(field)] = value\n\t\t}\n\t}\n\tif r.Status == StatusImplemented && r.ParentValidation != "" {\n\t\tobject[string(fieldParentValidation)] = r.ParentValidation\n\t\tobject[string(fieldParentValidationWorkingDir)] = r.ParentValidationWorkingDir\n\t}\n\tif r.Status == StatusImplemented && r.ParentValidationEvidence != nil {\n\t\tobject[string(fieldParentValidationEvidence)] = r.ParentValidationEvidence\n\t}\n\tif len(r.Targets) > 0 {\n\t\tobject[string(fieldTargets)] = r.Targets\n\t}\n\tif len(r.Artifacts) > 0 {\n\t\tobject[string(fieldArtifacts)] = r.Artifacts\n\t}\n'''
if old_machine not in text:
    raise SystemExit("MachineJSON target missing")
text = text.replace(old_machine, new_machine, 1)
result_path.write_text(text)

# schema.go: build all schema variants from shared field/status/risk metadata.
schema_path = Path("glm-worker/internal/packet/schema.go")
text = schema_path.read_text()
start = text.index("func riskProperty()")
end = text.index("func WorkerSchemaJSON()", start)
new_schema_builders = '''func schemaPropertyForField(field machineField, contract machineContract) *propertySchema {\n\tspec := machineFieldSpec(field)\n\tswitch spec.kind {\n\tcase machineFieldString:\n\t\treturn stringProperty()\n\tcase machineFieldStrings:\n\t\treturn stringsProperty()\n\tcase machineFieldStatus:\n\t\tvalues := make([]string, 0, len(contract.statuses))\n\t\tfor _, status := range contract.statuses {\n\t\t\tvalues = append(values, string(status))\n\t\t}\n\t\treturn stringProperty(values...)\n\tcase machineFieldRisk:\n\t\trisks := machineContractRisks(contract)\n\t\tvalues := make([]string, 0, len(risks))\n\t\tfor _, risk := range risks {\n\t\t\tvalues = append(values, string(risk))\n\t\t}\n\t\treturn stringProperty(values...)\n\tcase machineFieldParentValidationForm:\n\t\treturn stringProperty(ParentValidationGoTest, ParentValidationGoTestRace)\n\tdefault:\n\t\tpanic(fmt.Sprintf("machine field %qのschema kindが未対応です", field))\n\t}\n}\n\nfunc schemaForMachineContract(contract machineContract) *objectSchema {\n\tproperties := make(map[string]*propertySchema, len(contract.modelFields))\n\tfor _, field := range contract.modelFields {\n\t\tname := string(field)\n\t\tif _, duplicate := properties[name]; duplicate {\n\t\t\tpanic(fmt.Sprintf("machine contract %sでfield %qが重複しています", contract.name, field))\n\t\t}\n\t\tproperties[name] = schemaPropertyForField(field, contract)\n\t}\n\trequiredFields := schemaRequiredFields(contract)\n\trequired := make([]string, 0, len(requiredFields))\n\tfor _, field := range requiredFields {\n\t\trequired = append(required, string(field))\n\t}\n\treturn &objectSchema{\n\t\tType:       schemaTypeObject,\n\t\tProperties: properties,\n\t\tRequired:   required,\n\t}\n}\n\nfunc workerSchema() *objectSchema {\n\treturn schemaForMachineContract(workerMachineContract)\n}\n\nfunc reviewerSchema() *objectSchema {\n\treturn schemaForMachineContract(reviewerMachineContract)\n}\n\nfunc highFloorReviewerSchema() *objectSchema {\n\treturn schemaForMachineContract(highFloorReviewerMachineContract)\n}\n\nfunc riskFloorReviewerSchema() *objectSchema {\n\treturn schemaForMachineContract(riskFloorReviewerMachineContract)\n}\n\n'''
text = text[:start] + new_schema_builders + text[end:]
schema_path.write_text(text)

# validate.go: role/status/risk and required fields use the same machine contract.
validate_path = Path("glm-worker/internal/packet/validate.go")
text = validate_path.read_text()
start = text.index("func ValidateWorkerResult(result Result) error {")
end = text.index("func validateParentValidation", start)
text = text[:start] + '''func ValidateWorkerResult(result Result) error {\n\tif err := validateMachineStatusRisk(result, workerMachineContract); err != nil {\n\t\treturn err\n\t}\n\tif err := validateParentValidation(result); err != nil {\n\t\treturn err\n\t}\n\tif err := validateFields(result, resultFieldsForStatus(result.Status)); err != nil {\n\t\treturn err\n\t}\n\treturn validateTargets(result)\n}\n\n''' + text[end:]
start = text.index("func ValidateReviewerResult(result Result) error {")
end = text.index("func validateTargets", start)
text = text[:start] + '''func ValidateReviewerResult(result Result) error {\n\tif err := validateMachineStatusRisk(result, reviewerMachineContract); err != nil {\n\t\treturn err\n\t}\n\tif result.ParentValidation != "" || result.ParentValidationWorkingDir != "" || result.ParentValidationEvidence != nil {\n\t\treturn &constraintError{reason: "reviewer結果にparent validation fieldは指定できません"}\n\t}\n\tif err := validateFields(result, resultFieldsForStatus(result.Status)); err != nil {\n\t\treturn err\n\t}\n\treturn validateTargets(result)\n}\n\n''' + text[end:]
old_fields = '''func validateFields(result Result, fields []contractField) error {\n\tfor _, field := range fields {\n\t\tvalue := field.value(result)\n\t\tif strings.TrimSpace(value) == "" {\n\t\t\treturn &constraintError{reason: fmt.Sprintf("結果に必須field %sがありません", field.machine)}\n\t\t}\n\t\tif strings.ContainsAny(value, "\\n\\r") {\n\t\t\treturn &constraintError{reason: fmt.Sprintf("field %sに改行を含められません: 複数事項は同じvalue内でセミコロン区切りにしてください", field.machine)}\n\t\t}\n\t\tif len(value) > MaxFieldBytes {\n\t\t\treturn &constraintError{reason: fmt.Sprintf("field %sは%d bytes以内にしてください", field.machine, MaxFieldBytes)}\n\t\t}\n\t}\n'''
new_fields = '''func validateFields(result Result, fields []machineField) error {\n\tfor _, field := range fields {\n\t\tvalue := machineFieldValue(result, field)\n\t\tif strings.TrimSpace(value) == "" {\n\t\t\treturn &constraintError{reason: fmt.Sprintf("結果に必須field %sがありません", field)}\n\t\t}\n\t\tif strings.ContainsAny(value, "\\n\\r") {\n\t\t\treturn &constraintError{reason: fmt.Sprintf("field %sに改行を含められません: 複数事項は同じvalue内でセミコロン区切りにしてください", field)}\n\t\t}\n\t\tif len(value) > MaxFieldBytes {\n\t\t\treturn &constraintError{reason: fmt.Sprintf("field %sは%d bytes以内にしてください", field, MaxFieldBytes)}\n\t\t}\n\t}\n'''
if old_fields not in text:
    raise SystemExit("validateFields target missing")
text = text.replace(old_fields, new_fields, 1)
validate_path.write_text(text)

# Avoid an unnecessary exported helper in the descriptor.
replace_once(
    "glm-worker/internal/packet/contract.go",
    '''\tStatusNeedsSolReview: {\n\t\trisks: RiskSlice(RiskHigh),''',
    '''\tStatusNeedsSolReview: {\n\t\trisks: []Risk{RiskHigh},''',
)
replace_once(
    "glm-worker/internal/packet/contract.go",
    '''\nfunc RiskSlice(values ...Risk) []Risk {\n\treturn values\n}\n''',
    "",
)
