package packet

import (
	"encoding/json"
	"fmt"
	"sort"
)

type scalarSchema struct {
	Type    string   `json:"type"`
	Enum    []string `json:"enum,omitempty"`
	Const   *string  `json:"const,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
}

type objectSchema struct {
	Type                 string                     `json:"type"`
	Properties           map[string]*propertySchema `json:"properties"`
	Required             []string                   `json:"required"`
	AdditionalProperties bool                       `json:"additionalProperties"`
	AnyOf                []objectCondition          `json:"anyOf,omitempty"`
}

type objectCondition struct {
	Properties map[string]*propertySchema `json:"properties,omitempty"`
	Required   []string                   `json:"required,omitempty"`
}

type arraySchema struct {
	Type     string       `json:"type"`
	Items    scalarSchema `json:"items"`
	MinItems int          `json:"minItems,omitempty"`
}

type propertySchema struct {
	scalar *scalarSchema
	array  *arraySchema
	object *objectSchema
}

const (
	schemaTypeObject  = "object"
	schemaTypeArray   = "array"
	schemaTypeString  = "string"
	schemaTypeNumber  = "number"
	schemaTypeBoolean = "boolean"
)

const singleLineNonBlankPattern = `^[^\r\n]*[^ \t\r\n][^\r\n]*$`

var scalarTypes = map[string]struct{}{
	schemaTypeString:  {},
	schemaTypeNumber:  {},
	schemaTypeBoolean: {},
}

func (p propertySchema) MarshalJSON() ([]byte, error) {
	switch {
	case p.scalar != nil:
		return json.Marshal(p.scalar)
	case p.array != nil:
		return json.Marshal(p.array)
	case p.object != nil:
		return json.Marshal(p.object)
	default:
		return nil, fmt.Errorf("property schemaの中身が空です")
	}
}

func plainStringProperty() *propertySchema {
	return &propertySchema{scalar: &scalarSchema{Type: schemaTypeString}}
}

func stringProperty(values ...string) *propertySchema {
	if len(values) == 0 {
		return &propertySchema{scalar: &scalarSchema{Type: schemaTypeString, Pattern: singleLineNonBlankPattern}}
	}
	return &propertySchema{scalar: &scalarSchema{Type: schemaTypeString, Enum: values}}
}

func forbiddenStringProperty() *propertySchema {
	empty := ""
	return &propertySchema{scalar: &scalarSchema{Type: schemaTypeString, Const: &empty}}
}

func stringsProperty() *propertySchema {
	return stringsPropertyMinItems(0)
}

func stringsPropertyMinItems(minItems int) *propertySchema {
	return &propertySchema{array: &arraySchema{
		Type:     schemaTypeArray,
		Items:    scalarSchema{Type: schemaTypeString, Pattern: singleLineNonBlankPattern},
		MinItems: minItems,
	}}
}

func schemaPropertyForField(field machineField, contract machineContract) *propertySchema {
	spec := machineFieldSpec(field)
	switch spec.kind {
	case machineFieldString:
		if field == fieldParentValidationWorkingDir {
			return plainStringProperty()
		}
		return stringProperty()
	case machineFieldStrings:
		return stringsProperty()
	case machineFieldStatus:
		values := make([]string, 0, len(contract.statuses))
		for _, status := range contract.statuses {
			values = append(values, string(status))
		}
		return stringProperty(values...)
	case machineFieldRisk:
		risks := machineContractRisks(contract)
		values := make([]string, 0, len(risks))
		for _, risk := range risks {
			values = append(values, string(risk))
		}
		return stringProperty(values...)
	case machineFieldParentValidationForm:
		return stringProperty(ParentValidationGoTest, ParentValidationGoTestRace)
	default:
		panic(fmt.Sprintf("machine field %qのschema kindが未対応です", field))
	}
}

func schemaForMachineContract(contract machineContract) *objectSchema {
	properties := make(map[string]*propertySchema, len(contract.modelFields))
	for _, field := range contract.modelFields {
		name := string(field)
		if _, duplicate := properties[name]; duplicate {
			panic(fmt.Sprintf("machine contract %sでfield %qが重複しています", contract.name, field))
		}
		properties[name] = schemaPropertyForField(field, contract)
	}
	requiredFields := schemaRequiredFields(contract)
	required := make([]string, 0, len(requiredFields))
	for _, field := range requiredFields {
		required = append(required, string(field))
	}
	return &objectSchema{
		Type:                 schemaTypeObject,
		Properties:           properties,
		Required:             required,
		AdditionalProperties: false,
		AnyOf:                schemaStatusConditions(contract),
	}
}

func schemaStatusConditions(contract machineContract) []objectCondition {
	conditions := make([]objectCondition, 0, len(contract.statuses))
	for _, status := range contract.statuses {
		statusContract, ok := packetStatusContracts[status]
		if !ok {
			panic(fmt.Sprintf("status %qにmachine contractがありません", status))
		}
		properties := map[string]*propertySchema{
			string(fieldStatus): stringProperty(string(status)),
		}
		risks := make([]string, 0, len(statusContract.risks))
		for _, risk := range statusContract.risks {
			risks = append(risks, string(risk))
		}
		properties[string(fieldRisk)] = stringProperty(risks...)
		if status != StatusImplemented {
			properties[string(fieldTargets)] = stringsPropertyMinItems(1)
		}
		if contract.name == workerMachineContract.name && status != StatusImplemented {
			properties[string(fieldParentValidation)] = forbiddenStringProperty()
			properties[string(fieldParentValidationWorkingDir)] = forbiddenStringProperty()
		}
		required := []string{string(fieldStatus), string(fieldRisk)}
		for _, field := range statusContract.resultFields {
			required = append(required, string(field))
		}
		required = append(required, string(fieldTargets), string(fieldArtifacts))
		conditions = append(conditions, objectCondition{Properties: properties, Required: required})
	}
	return conditions
}

func workerSchema() *objectSchema {
	return schemaForMachineContract(workerMachineContract)
}

func reviewerSchema() *objectSchema {
	return schemaForMachineContract(reviewerMachineContract)
}

func highFloorReviewerSchema() *objectSchema {
	return schemaForMachineContract(highFloorReviewerMachineContract)
}

func riskFloorReviewerSchema() *objectSchema {
	return schemaForMachineContract(riskFloorReviewerMachineContract)
}

func WorkerSchemaJSON() (string, error) {
	return schemaJSON(workerSchema())
}

func ReviewerSchemaJSON() (string, error) {
	return schemaJSON(reviewerSchema())
}

func HighFloorReviewerSchemaJSON() (string, error) {
	return schemaJSON(highFloorReviewerSchema())
}

func RiskFloorReviewerSchemaJSON() (string, error) {
	return schemaJSON(riskFloorReviewerSchema())
}

func schemaJSON(schema *objectSchema) (string, error) {
	validateObjectSchema(schema, "$")
	data, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func validateObjectSchema(schema *objectSchema, path string) {
	if schema == nil || schema.Type != schemaTypeObject {
		panic(fmt.Sprintf("%s: object schemaのtypeがobjectではありません", path))
	}
	if len(schema.Properties) == 0 {
		panic(fmt.Sprintf("%s: object schemaにpropertiesがありません", path))
	}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		validatePropertySchema(schema.Properties[name], path+"."+name)
	}
	validateRequiredNames(schema.Required, schema.Properties, path)
	for index, condition := range schema.AnyOf {
		validateObjectCondition(condition, schema.Properties, fmt.Sprintf("%s.anyOf[%d]", path, index))
	}
}

func validateObjectCondition(condition objectCondition, rootProperties map[string]*propertySchema, path string) {
	if len(condition.Properties) == 0 || len(condition.Required) == 0 {
		panic(fmt.Sprintf("%s: status conditionにproperties/requiredがありません", path))
	}
	for name, property := range condition.Properties {
		if _, ok := rootProperties[name]; !ok {
			panic(fmt.Sprintf("%s: condition property %sがroot propertiesにありません", path, name))
		}
		validatePropertySchema(property, path+"."+name)
	}
	validateRequiredNames(condition.Required, rootProperties, path)
}

func validateRequiredNames(required []string, properties map[string]*propertySchema, path string) {
	for _, name := range required {
		if _, ok := properties[name]; !ok {
			panic(fmt.Sprintf("%s: requiredの%sがpropertiesにありません", path, name))
		}
	}
}

func validatePropertySchema(property *propertySchema, path string) {
	switch {
	case property.scalar != nil:
		validateScalarSchema(property.scalar, path)
	case property.array != nil:
		if property.array.Type != schemaTypeArray {
			panic(fmt.Sprintf("%s: array schemaのtypeがarrayではありません", path))
		}
		if property.array.MinItems < 0 || property.array.MinItems > 1 {
			panic(fmt.Sprintf("%s: minItemsは0または1だけを指定できます", path))
		}
		if len(property.array.Items.Enum) != 0 || property.array.Items.Const != nil {
			panic(fmt.Sprintf("%s: array itemsへenum/constを指定できません", path))
		}
		validateScalarSchema(&property.array.Items, path+".items")
	case property.object != nil:
		validateObjectSchema(property.object, path)
	default:
		panic(fmt.Sprintf("%s: property schemaの中身が空です", path))
	}
}

func validateScalarSchema(schema *scalarSchema, path string) {
	if _, ok := scalarTypes[schema.Type]; !ok {
		panic(fmt.Sprintf("%s: scalar type %qは許可list外です", path, schema.Type))
	}
	if schema.Pattern != "" && schema.Type != schemaTypeString {
		panic(fmt.Sprintf("%s: patternはstring以外へ指定できません", path))
	}
	if schema.Const != nil && schema.Type != schemaTypeString {
		panic(fmt.Sprintf("%s: constはstring以外へ指定できません", path))
	}
	if len(schema.Enum) == 0 {
		return
	}
	if schema.Type != schemaTypeString {
		panic(fmt.Sprintf("%s: enumはstring以外へ指定できません", path))
	}
	seen := make(map[string]struct{}, len(schema.Enum))
	for _, value := range schema.Enum {
		if value == "" {
			panic(fmt.Sprintf("%s: enumに空文字が含まれます", path))
		}
		if _, ok := seen[value]; ok {
			panic(fmt.Sprintf("%s: enum %sが重複しています", path, value))
		}
		seen[value] = struct{}{}
	}
}
