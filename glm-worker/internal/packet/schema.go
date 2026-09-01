package packet

import (
	"encoding/json"
	"fmt"
	"sort"
)

type scalarSchema struct {
	Type string   `json:"type"`
	Enum []string `json:"enum,omitempty"`
}

type objectSchema struct {
	Type       string                     `json:"type"`
	Properties map[string]*propertySchema `json:"properties"`
	Required   []string                   `json:"required"`
}

type arraySchema struct {
	Type  string       `json:"type"`
	Items scalarSchema `json:"items"`
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

func stringProperty(values ...string) *propertySchema {
	if len(values) == 0 {
		return &propertySchema{scalar: &scalarSchema{Type: schemaTypeString}}
	}
	return &propertySchema{scalar: &scalarSchema{Type: schemaTypeString, Enum: values}}
}

func stringsProperty() *propertySchema {
	return &propertySchema{array: &arraySchema{Type: schemaTypeArray, Items: scalarSchema{Type: schemaTypeString}}}
}

func schemaPropertyForField(field machineField, contract machineContract) *propertySchema {
	spec := machineFieldSpec(field)
	switch spec.kind {
	case machineFieldString:
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
		Type:       schemaTypeObject,
		Properties: properties,
		Required:   required,
	}
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
	for _, required := range schema.Required {
		if _, ok := schema.Properties[required]; !ok {
			panic(fmt.Sprintf("%s: requiredの%sがpropertiesにありません", path, required))
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
		if len(property.array.Items.Enum) != 0 {
			panic(fmt.Sprintf("%s: array itemsへenumを指定できません", path))
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
