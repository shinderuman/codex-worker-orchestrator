package parentactioncmd

import "testing"

func TestDecodeSessionRotationCreationResultStrictJSON(t *testing.T) {
	valid := `{"version":1,"outcome":"failed","parent_thread_id":"01a0463c-d477-7410-9efd-cb34ff2e0b0e","directive_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","claim_id":"bbbbbbbb-cccc-4ddd-8eee-ffffffffffff","target_task_id":"cccccccc-dddd-4eee-8fff-aaaaaaaaaaaa"}`
	result, err := decodeSessionRotationCreationResult(valid)
	if err != nil || result.Outcome != "failed" {
		t.Fatalf("valid creation result = %#v err=%v", result, err)
	}
	if _, err := decodeSessionRotationCreationResult(valid[:len(valid)-1] + `,"extra":true}`); err == nil {
		t.Fatal("unknown fieldを含むcreation resultが受理されました")
	}
	if _, err := decodeSessionRotationCreationResult(valid + ` {}`); err == nil {
		t.Fatal("trailing JSON valueを含むcreation resultが受理されました")
	}
}
