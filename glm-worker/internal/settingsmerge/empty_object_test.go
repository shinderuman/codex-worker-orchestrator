package settingsmerge

import "testing"

func TestManagedSettingsEmptyObjectPreservesUserOwnedChildren(t *testing.T) {
	target := map[string]any{
		"env": map[string]any{
			"LOCAL": "keep",
		},
	}
	previous := managedState{Version: managedStateVersion, Values: []managedValueState{}}
	fragment := map[string]any{"env": map[string]any{}}

	next, err := reconcileManagedValues(target, previous, fragment)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Values) != 0 {
		t.Fatalf("empty object created managed ownership: %#v", next.Values)
	}
	env, ok := target["env"].(map[string]any)
	if !ok || env["LOCAL"] != "keep" {
		t.Fatalf("user-owned env changed: %#v", target)
	}
}
