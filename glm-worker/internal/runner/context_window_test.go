package runner

import "testing"

func TestContextWindowForManagedGLMModels(t *testing.T) {
	env := map[string]string{
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "glm-5.3",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.3",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "glm-4.7",
	}
	tests := []struct {
		alias    string
		model    string
		known    int
		declared int
	}{
		{alias: "opus", model: "glm-5.3", known: 1_000_000, declared: 1_000_000},
		{alias: "sonnet", model: "glm-5.3", known: 1_000_000, declared: 1_000_000},
		{alias: "haiku", model: "glm-4.7", known: 200_000, declared: 0},
	}
	for _, test := range tests {
		got := contextWindowForModel(test.alias, env)
		if got.resolvedModelID != test.model || got.knownModelContextWindowTokens != test.known ||
			got.declaredMaxContextTokens != test.declared || got.source != zaiModelContextWindowSource {
			t.Fatalf("%s context = %#v", test.alias, got)
		}
	}
}

func TestContextWindowForUnknownModelDoesNotInventCapacity(t *testing.T) {
	got := contextWindowForModel("custom-model", nil)
	if got.resolvedModelID != "custom-model" || got.knownModelContextWindowTokens != 0 ||
		got.declaredMaxContextTokens != 0 || got.source != "" {
		t.Fatalf("unknown context = %#v", got)
	}
}
