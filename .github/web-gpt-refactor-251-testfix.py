from pathlib import Path

path = Path("glm-worker/internal/packet/result_test.go")
text = path.read_text()
old = '''\t\t\tcontract := result.contractFields()\n\t\t\tcontractKeys := make(map[string]bool, len(contract))\n\t\t\tfor _, field := range contract {\n\t\t\t\tcontractKeys[field.machine] = true\n\t\t\t\tsetter, ok := textFieldSetters[field.machine]\n\t\t\t\tif !ok {\n\t\t\t\t\tt.Fatalf("contractFieldsにtextFieldSetters未対応のfieldがあります: %s", field.machine)\n\t\t\t\t}\n\t\t\t\tblanked := fullyPopulatedResult(status)\n\t\t\t\tsetter(&blanked, " ")\n\t\t\t\terr := validate(blanked)\n\t\t\t\tif err == nil || !IsConstraintError(err) || !strings.Contains(err.Error(), "必須field "+field.machine) {\n\t\t\t\t\tt.Fatalf("%sを空にした場合の必須field errorが出ていません: %v", field.machine, err)\n\t\t\t\t}\n\t\t\t}\n'''
new = '''\t\t\tcontract := resultFieldsForStatus(status)\n\t\t\tcontractKeys := make(map[string]bool, len(contract))\n\t\t\tfor _, field := range contract {\n\t\t\t\tmachine := string(field)\n\t\t\t\tcontractKeys[machine] = true\n\t\t\t\tsetter, ok := textFieldSetters[machine]\n\t\t\t\tif !ok {\n\t\t\t\t\tt.Fatalf("resultFieldsForStatusにtextFieldSetters未対応のfieldがあります: %s", machine)\n\t\t\t\t}\n\t\t\t\tblanked := fullyPopulatedResult(status)\n\t\t\t\tsetter(&blanked, " ")\n\t\t\t\terr := validate(blanked)\n\t\t\t\tif err == nil || !IsConstraintError(err) || !strings.Contains(err.Error(), "必須field "+machine) {\n\t\t\t\t\tt.Fatalf("%sを空にした場合の必須field errorが出ていません: %v", machine, err)\n\t\t\t\t}\n\t\t\t}\n'''
if old not in text:
    raise SystemExit("result_test contract target missing")
path.write_text(text.replace(old, new, 1))
