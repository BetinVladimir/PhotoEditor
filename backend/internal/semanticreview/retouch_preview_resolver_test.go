package semanticreview

import "testing"

func TestRetouchResolverRejectsMissingDatabase(t *testing.T) {
	if _, err := SQLRetouchPreviewResolver(nil, nil, ""); err != ErrInvalid {
		t.Fatal("missing DB accepted", err)
	}
}
