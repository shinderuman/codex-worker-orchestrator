package shadoweval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

type ReferenceLabel struct {
	CallID             string `json:"call_id"`
	CorrectnessFinding bool   `json:"correctness_finding"`
	Owner              string `json:"owner"`
	Disposition        string `json:"disposition"`
	Locator            string `json:"locator"`
}

type Reference struct {
	Schema         string           `json:"schema"`
	TaskID         string           `json:"bundle_task_id"`
	Basis          string           `json:"basis"`
	Labels         []ReferenceLabel `json:"labels"`
	UnknownCallIDs []string         `json:"unknown_call_ids"`
}

const ReferenceSchema = "system-one-shadow-reference/v1"

var referenceOwnerCategories = map[string]bool{
	"worker":            true,
	"reviewer":          true,
	"parent":            true,
	"test-scenario":     true,
	"production-wiring": true,
	"external-provider": true,
	"none":              true,
	"unknown":           true,
}

func LoadReference(path string) (Reference, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Reference{}, fmt.Errorf("reference fileを読めません: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var reference Reference
	if err := decoder.Decode(&reference); err != nil {
		return Reference{}, fmt.Errorf("reference fileを解析できません: %w", err)
	}
	if err := validateReference(reference); err != nil {
		return Reference{}, err
	}
	return reference, nil
}

func validateReference(reference Reference) error {
	if reference.Schema != ReferenceSchema {
		return fmt.Errorf("reference schemaが不正です: %q", reference.Schema)
	}
	if reference.TaskID == "" {
		return fmt.Errorf("reference bundle_task_idが空です")
	}
	seen := make(map[string]bool, len(reference.Labels)+len(reference.UnknownCallIDs))
	for index, label := range reference.Labels {
		if err := validateReferenceLabel(index, label, seen); err != nil {
			return err
		}
	}
	for index, callID := range reference.UnknownCallIDs {
		if callID == "" {
			return fmt.Errorf("reference unknown_call_ids[%d]が空です", index)
		}
		if seen[callID] {
			return fmt.Errorf("referenceのcall_idが重複しています: %s", callID)
		}
		seen[callID] = true
	}
	return nil
}

func validateReferenceLabel(index int, label ReferenceLabel, seen map[string]bool) error {
	if label.CallID == "" {
		return fmt.Errorf("reference labels[%d]のcall_idが空です", index)
	}
	if seen[label.CallID] {
		return fmt.Errorf("referenceのcall_idが重複しています: %s", label.CallID)
	}
	seen[label.CallID] = true
	if !referenceOwnerCategories[label.Owner] {
		return fmt.Errorf("reference labels[%d]のownerが不正です: %q", index, label.Owner)
	}
	if !dispositionCategories[label.Disposition] {
		return fmt.Errorf("reference labels[%d]のdispositionが不正です: %q", index, label.Disposition)
	}
	return nil
}

func ValidateReferenceAgainstInput(reference Reference, input ShadowInput) error {
	if reference.TaskID != input.TaskID {
		return fmt.Errorf("referenceのbundle_task_idが対象taskと一致しません: %q != %q", reference.TaskID, input.TaskID)
	}
	known := make(map[string]bool, len(input.Items))
	for _, item := range input.Items {
		known[item.CallID] = true
	}
	for _, label := range reference.Labels {
		if !known[label.CallID] {
			return fmt.Errorf("referenceのlabel %sが入力itemsに存在しません", label.CallID)
		}
	}
	for _, callID := range reference.UnknownCallIDs {
		if !known[callID] {
			return fmt.Errorf("referenceのunknown_call_id %sが入力itemsに存在しません", callID)
		}
	}
	return nil
}
