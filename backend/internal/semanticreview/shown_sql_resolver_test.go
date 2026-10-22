package semanticreview

import "testing"

func TestShownSQLResolverRequiresScopedDependencies(t *testing.T) {
	if _, err := NewSQLShownApplyResolver(nil, nil, nil, 1048576, "a", "b", nil); err == nil {
		t.Fatal("missing scoped dependencies admitted")
	}
}
