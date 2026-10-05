package repositoryproject

import "testing"

func TestEvaluateObservationCapability(t *testing.T) {
	for _, tc := range []struct {
		name     string
		content  string
		admitted bool
		status   string
	}{
		{name: "poc", content: "# task\n\n## External feasibility\n\nstatus: poc\nassumption: producer behavior\n", admitted: true, status: "poc"},
		{name: "observation", content: "# task\n\n## External feasibility\n\nstatus: observation\nassumption: producer behavior\n", admitted: true, status: "observation"},
		{name: "not-applicable", content: "# task\n\n## External feasibility\n\nstatus: not-applicable\n", status: "not-applicable"},
		{name: "implementation", content: "# task\n\n## External feasibility\n\nstatus: implementation\nassumption: producer behavior\nevidence-source: producer\nevidence: observed\ngo: approved\n", status: "implementation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := EvaluateObservationCapability([]byte(tc.content))
			if err != nil {
				t.Fatal(err)
			}
			if policy.Admitted != tc.admitted || policy.Status != tc.status {
				t.Fatalf("policy = %+v", policy)
			}
		})
	}
}

func TestEvaluateObservationCapabilityRejectsMalformedDeclaration(t *testing.T) {
	if _, err := EvaluateObservationCapability([]byte("# task\n")); err == nil {
		t.Fatal("missing External feasibility declaration was admitted")
	}
}
