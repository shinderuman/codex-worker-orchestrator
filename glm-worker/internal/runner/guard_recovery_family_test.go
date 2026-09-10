package runner

import "testing"

func TestSameGuardFailureFamilyText(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
		want   bool
	}{
		{
			name:   "git",
			first:  "git authority guard failed: after-call-mutation: refs changed",
			second: "git authority guard failed: capture-before-call: cannot read refs",
			want:   true,
		},
		{
			name:   "instruction",
			first:  "repository instruction surface guard failed: after-call-mutation: restored",
			second: "repository instruction surface guard failed: before-call-mismatch: changed",
			want:   true,
		},
		{
			name:   "different",
			first:  "git authority guard failed: after-call-mutation: refs changed",
			second: "repository instruction surface guard failed: before-call-mismatch: changed",
			want:   false,
		},
		{
			name:   "unknown",
			first:  "guard failed",
			second: "guard failed",
			want:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SameGuardFailureFamilyText(test.first, test.second); got != test.want {
				t.Fatalf("SameGuardFailureFamilyText() = %v want %v", got, test.want)
			}
		})
	}
}
