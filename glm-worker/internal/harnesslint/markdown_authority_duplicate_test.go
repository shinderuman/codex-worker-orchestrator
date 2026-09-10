package harnesslint

import "testing"

func TestMarkdownDerivedStateRejectsNoneInDuplicateReviewFindings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{
			name:    "unresolved-then-none",
			content: "# Task\n\n## Review findings\n\n- unresolved defect\n\n## Review findings\n\nnone\n",
		},
		{
			name:    "none-then-unresolved",
			content: "# Task\n\n## Review findings\n\nnone\n\n## Review findings\n\n- unresolved defect\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := "IMPLEMENTATION_TASKS/a.md"
			writeMarkdownAuthorityFixture(t, root, path, tc.content)

			violations, err := markdownDerivedStatePathViolations(root, path)
			if err != nil {
				t.Fatal(err)
			}
			requireMarkdownDerivedStateViolation(t, violations, path)
		})
	}
}
