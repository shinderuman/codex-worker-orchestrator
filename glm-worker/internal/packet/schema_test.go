package packet

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func decodeSchema(t *testing.T, encoded string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("schema json: %v", err)
	}
	return decoded
}

func TestSchemaJSONRestrictedVocabulary(t *testing.T) {
	allowedNodeKeys := map[string]bool{
		"type": true, "properties": true, "required": true, "enum": true, "const": true,
		"pattern": true, "items": true, "minItems": true, "additionalProperties": true, "anyOf": true,
	}
	allowedTypes := map[string]bool{"object": true, "array": true, "string": true, "number": true, "boolean": true}

	for name, encoded := range map[string]func() (string, error){
		"worker":     WorkerSchemaJSON,
		"reviewer":   ReviewerSchemaJSON,
		"risk-floor": RiskFloorReviewerSchemaJSON,
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := encoded()
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			decoded := decodeSchema(t, encoded)
			if decoded["type"] != "object" {
				t.Fatalf("root type = %v", decoded["type"])
			}
			var walk func(node map[string]any, path string)
			walk = func(node map[string]any, path string) {
				for key := range node {
					if !allowedNodeKeys[key] {
						t.Fatalf("%s: vocabulary外のkey %q", path, key)
					}
				}
				if nodeType, ok := node["type"].(string); ok {
					if !allowedTypes[nodeType] {
						t.Fatalf("%s: 許可外type %q", path, nodeType)
					}
					switch nodeType {
					case "object":
						properties, _ := node["properties"].(map[string]any)
						if len(properties) == 0 {
							t.Fatalf("%s: objectにpropertiesがありません", path)
						}
						if additional, ok := node["additionalProperties"].(bool); !ok || additional {
							t.Fatalf("%s: object additionalProperties = %v, want false", path, node["additionalProperties"])
						}
						for name, raw := range properties {
							child, _ := raw.(map[string]any)
							walk(child, path+"."+name)
						}
						if required, ok := node["required"].([]any); ok {
							for _, raw := range required {
								name, _ := raw.(string)
								if _, ok := properties[name]; !ok {
									t.Fatalf("%s: required %qがpropertiesにありません", path, name)
								}
							}
						}
					case "array":
						items, _ := node["items"].(map[string]any)
						if items == nil {
							t.Fatalf("%s: arrayにitemsがありません", path)
						}
						walk(items, path+".items")
					}
				}
				if properties, ok := node["properties"].(map[string]any); ok && node["type"] == nil {
					for name, raw := range properties {
						child, _ := raw.(map[string]any)
						walk(child, path+"."+name)
					}
				}
				if anyOf, ok := node["anyOf"].([]any); ok {
					for index, raw := range anyOf {
						child, _ := raw.(map[string]any)
						walk(child, path+".anyOf["+string(rune('0'+index))+"]")
					}
				}
			}
			walk(decoded, "$")
		})
	}
}

func TestWorkerSchemaContents(t *testing.T) {
	encoded, err := WorkerSchemaJSON()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	decoded := decodeSchema(t, encoded)
	properties := decoded["properties"].(map[string]any)
	status := properties["status"].(map[string]any)
	enum := status["enum"].([]any)
	values := make([]string, 0, len(enum))
	for _, raw := range enum {
		values = append(values, raw.(string))
	}
	if strings.Join(values, ",") != "IMPLEMENTED,NEEDS_SOL_DECISION" {
		t.Fatalf("worker status enum = %v", values)
	}
	required := decoded["required"].([]any)
	if len(required) != 4 {
		t.Fatalf("required = %v", required)
	}
	for _, want := range []string{"status", "risk", "targets", "artifacts"} {
		if !containsAnyString(required, want) {
			t.Fatalf("worker requiredに%sがありません: %v", want, required)
		}
	}
	for _, want := range []string{"decision", "evidence", "options", "recommendation", "test_obligations", "targets", "artifacts"} {
		if _, ok := properties[want]; !ok {
			t.Fatalf("worker schemaに%sがありません", want)
		}
	}
}

func TestReviewerSchemaContents(t *testing.T) {
	encoded, err := ReviewerSchemaJSON()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	decoded := decodeSchema(t, encoded)
	properties := decoded["properties"].(map[string]any)
	status := properties["status"].(map[string]any)
	enum := status["enum"].([]any)
	values := make([]string, 0, len(enum))
	for _, raw := range enum {
		values = append(values, raw.(string))
	}
	if strings.Join(values, ",") != "PASS,FIX_REQUIRED,NEEDS_SOL_REVIEW,NEEDS_SOL_DECISION" {
		t.Fatalf("reviewer status enum = %v", values)
	}
	for _, want := range []string{
		"invariants", "test_evidence", "issues", "residual_risk", "sol_question",
		"decision", "evidence", "options", "recommendation", "test_obligations", "targets", "artifacts",
	} {
		if _, ok := properties[want]; !ok {
			t.Fatalf("reviewer schemaに%sがありません", want)
		}
	}
	if decoded["additionalProperties"] != false {
		t.Fatalf("reviewer additionalProperties = %v, want false", decoded["additionalProperties"])
	}
}

func TestRiskFloorReviewerSchemaContents(t *testing.T) {
	encoded, err := RiskFloorReviewerSchemaJSON()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	decoded := decodeSchema(t, encoded)
	properties := decoded["properties"].(map[string]any)
	status := properties["status"].(map[string]any)["enum"].([]any)
	if len(status) != 1 || status[0] != string(StatusNeedsSolReview) {
		t.Fatalf("risk-floor status enum = %v", status)
	}
	risk := properties["risk"].(map[string]any)["enum"].([]any)
	if len(risk) != 1 || risk[0] != string(RiskHigh) {
		t.Fatalf("risk-floor risk enum = %v", risk)
	}
	required := decoded["required"].([]any)
	for _, want := range []string{
		"status", "risk", "summary", "requirement_coverage", "invariants", "test_evidence",
		"issues", "residual_risk", "sol_question", "targets", "artifacts",
	} {
		if !containsAnyString(required, want) {
			t.Fatalf("risk-floor requiredに%sがありません: %v", want, required)
		}
	}
}

func TestSchemaStatusConditionsMatchMachineContracts(t *testing.T) {
	cases := []struct {
		name     string
		contract machineContract
		encode   func() (string, error)
	}{
		{name: "worker", contract: workerMachineContract, encode: WorkerSchemaJSON},
		{name: "reviewer", contract: reviewerMachineContract, encode: ReviewerSchemaJSON},
		{name: "high-floor", contract: highFloorReviewerMachineContract, encode: HighFloorReviewerSchemaJSON},
		{name: "risk-floor", contract: riskFloorReviewerMachineContract, encode: RiskFloorReviewerSchemaJSON},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := tc.encode()
			if err != nil {
				t.Fatal(err)
			}
			decoded := decodeSchema(t, encoded)
			for _, status := range tc.contract.statuses {
				branch := statusBranch(t, decoded, status)
				required := stringsFromAny(t, branch["required"])
				wantRequired := []string{string(fieldStatus), string(fieldRisk)}
				for _, field := range packetStatusContracts[status].resultFields {
					wantRequired = append(wantRequired, string(field))
				}
				wantRequired = append(wantRequired, string(fieldTargets), string(fieldArtifacts))
				sort.Strings(required)
				sort.Strings(wantRequired)
				if !reflect.DeepEqual(required, wantRequired) {
					t.Fatalf("status %s required = %v, want %v", status, required, wantRequired)
				}
				branchProperties := branch["properties"].(map[string]any)
				riskEnum := stringsFromAny(t, branchProperties[string(fieldRisk)].(map[string]any)["enum"])
				wantRisks := make([]string, 0, len(packetStatusContracts[status].risks))
				for _, risk := range packetStatusContracts[status].risks {
					wantRisks = append(wantRisks, string(risk))
				}
				if !reflect.DeepEqual(riskEnum, wantRisks) {
					t.Fatalf("status %s risks = %v, want %v", status, riskEnum, wantRisks)
				}
			}
		})
	}
}

func TestSchemaMovesMechanicalFieldConstraintsToStructuredBoundary(t *testing.T) {
	encoded, err := WorkerSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeSchema(t, encoded)
	properties := decoded["properties"].(map[string]any)
	if pattern := properties[string(fieldSummary)].(map[string]any)["pattern"]; pattern != singleLineNonBlankPattern {
		t.Fatalf("summary pattern = %v, want %q", pattern, singleLineNonBlankPattern)
	}
	targets := properties[string(fieldTargets)].(map[string]any)
	if pattern := targets["items"].(map[string]any)["pattern"]; pattern != singleLineNonBlankPattern {
		t.Fatalf("targets item pattern = %v, want %q", pattern, singleLineNonBlankPattern)
	}
	decisionBranch := statusBranch(t, decoded, StatusNeedsSolDecision)
	decisionProperties := decisionBranch["properties"].(map[string]any)
	if minItems := decisionProperties[string(fieldTargets)].(map[string]any)["minItems"]; minItems != float64(1) {
		t.Fatalf("NEEDS_SOL_DECISION targets minItems = %v, want 1", minItems)
	}
	for _, field := range []machineField{fieldParentValidation, fieldParentValidationWorkingDir} {
		if forbidden := decisionProperties[string(field)].(map[string]any)["const"]; forbidden != "" {
			t.Fatalf("%s forbidden const = %v, want empty string", field, forbidden)
		}
	}
	if strings.Contains(encoded, "maxLength") || strings.Contains(encoded, "maxItems") {
		t.Fatalf("unsupported byte/array maximum leaked into Claude schema: %s", encoded)
	}
}

func statusBranch(t *testing.T, decoded map[string]any, status Status) map[string]any {
	t.Helper()
	for _, raw := range decoded["anyOf"].([]any) {
		branch := raw.(map[string]any)
		properties := branch["properties"].(map[string]any)
		statusProperty := properties[string(fieldStatus)].(map[string]any)
		values := stringsFromAny(t, statusProperty["enum"])
		if len(values) == 1 && values[0] == string(status) {
			return branch
		}
	}
	t.Fatalf("status branch %s not found", status)
	return nil
}

func stringsFromAny(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("value = %#v, want []any", value)
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("item = %#v, want string", item)
		}
		result = append(result, text)
	}
	return result
}

func containsAnyString(values []any, want string) bool {
	for _, raw := range values {
		if raw == want {
			return true
		}
	}
	return false
}

func TestSchemaValidationPanicsOnVocabularyViolation(t *testing.T) {
	cases := []struct {
		name   string
		schema *objectSchema
	}{
		{"unknown scalar type", &objectSchema{
			Type: "object",
			Properties: map[string]*propertySchema{
				"x": {scalar: &scalarSchema{Type: "integer"}},
			},
		}},
		{"enum on non string", &objectSchema{
			Type: "object",
			Properties: map[string]*propertySchema{
				"x": {scalar: &scalarSchema{Type: schemaTypeNumber, Enum: []string{"1"}}},
			},
		}},
		{"enum duplicate", &objectSchema{
			Type: "object",
			Properties: map[string]*propertySchema{
				"x": {scalar: &scalarSchema{Type: schemaTypeString, Enum: []string{"a", "a"}}},
			},
		}},
		{"enum empty value", &objectSchema{
			Type: "object",
			Properties: map[string]*propertySchema{
				"x": {scalar: &scalarSchema{Type: schemaTypeString, Enum: []string{""}}},
			},
		}},
		{"required not in properties", &objectSchema{
			Type: schemaTypeObject,
			Properties: map[string]*propertySchema{
				"x": stringProperty(),
			},
			Required: []string{"y"},
		}},
		{"array items enum", &objectSchema{
			Type: schemaTypeObject,
			Properties: map[string]*propertySchema{
				"x": {array: &arraySchema{Type: schemaTypeArray, Items: scalarSchema{Type: schemaTypeString, Enum: []string{"a"}}}},
			},
		}},
		{"array minItems unsupported", &objectSchema{
			Type: schemaTypeObject,
			Properties: map[string]*propertySchema{
				"x": {array: &arraySchema{Type: schemaTypeArray, Items: scalarSchema{Type: schemaTypeString}, MinItems: 2}},
			},
		}},
		{"empty property", &objectSchema{
			Type: schemaTypeObject,
			Properties: map[string]*propertySchema{
				"x": {},
			},
		}},
		{"nested object violation", &objectSchema{
			Type: schemaTypeObject,
			Properties: map[string]*propertySchema{
				"x": {object: &objectSchema{
					Type:       schemaTypeObject,
					Properties: map[string]*propertySchema{"y": {scalar: &scalarSchema{Type: "integer"}}},
				}},
			},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			_, err := schemaJSON(c.schema)
			if err != nil {
				t.Fatalf("unexpected error before validation: %v", err)
			}
		})
	}
}

func TestSchemaRoundTripViaUnmarshal(t *testing.T) {
	encoded, err := WorkerSchemaJSON()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("err = %v", err)
	}
	if decoded["type"] != "object" || decoded["properties"] == nil || decoded["required"] == nil || decoded["anyOf"] == nil {
		t.Fatalf("schema root shape: %v", decoded)
	}
}
