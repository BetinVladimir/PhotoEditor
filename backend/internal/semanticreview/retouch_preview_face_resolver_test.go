package semanticreview

import "testing"

func TestRetouchFaceResolverRequiresDatabaseAndObjectReader(t *testing.T) {
	if _, err := SQLRetouchPreviewFaceResolver(nil, nil); err != ErrInvalid {
		t.Fatal("missing installation accepted", err)
	}
}
