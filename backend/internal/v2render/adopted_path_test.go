package v2render

import (
	"strings"
	"testing"
)

func TestAdoptionChildPathMatchesSchemaWalker(t *testing.T) {
	for _, tc := range []struct{ parent, base, child, want string }{
		{"$", "TaskInputs[3]", "TaskInputs[3].changed_mask", "$/changed_mask"},
		{"$/changed_mask", "TaskInputs[3].changed_mask", "TaskInputs[3].changed_mask.transform_chain.steps[0].inverse_field", "$/changed_mask/transform_chain/steps/*/inverse_field"},
		{"$/report", "root.report", "root.report.sources[12].source", "$/report/sources/*/source"},
		{"$/a", "root", "root.items[2][7].value", "$/a/items/*/*/value"},
	} {
		got, err := adoptionChildPath(tc.parent, tc.base, tc.child)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
}

func TestAdoptionChildPathRejectsAmbiguousEdges(t *testing.T) {
	for _, child := range []string{"root", "rooted.report", "root..report", "root.report/other", "root.items[-1]", "root.items[01]", "root.items[]", "root.items[*]", "root.items[1", "root.items[1]suffix", "root.имя", "root." + strings.Repeat("a", 512)} {
		if _, err := adoptionChildPath("$", "root", child); err == nil {
			t.Fatalf("invalid edge accepted: %q", child)
		}
	}
	for _, parent := range []string{"", "$/", "$//report", "$/report/", "$/../report", "$/step*"} {
		if _, err := adoptionChildPath(parent, "root", "root.report"); err == nil {
			t.Fatalf("invalid inventory parent accepted: %q", parent)
		}
	}
}
