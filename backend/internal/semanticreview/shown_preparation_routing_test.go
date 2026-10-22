package semanticreview

import "testing"

func TestShownPreparationRequiresScopedResolverAndFallback(t *testing.T) {
	if _, err := NewShownApplyPreparation(nil, nil, "owned", nil, nil); err == nil {
		t.Fatal("missing dependencies admitted")
	}
}
