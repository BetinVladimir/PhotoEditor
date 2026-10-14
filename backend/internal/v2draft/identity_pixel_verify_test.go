package v2draft

import (
	"context"
	"testing"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestIdentityPixelInputRoleAllowlist(t *testing.T) {
	for _, role := range []string{"params", "before", "after", "effective", "protected"} {
		if !identityInputRole(role) || identityArrayRole(role) {
			t.Fatalf("document role %q", role)
		}
	}
	for _, role := range []string{"before_pixels", "after_pixels", "before_alpha", "after_alpha", "effective_pixels", "protected_pixels"} {
		if !identityInputRole(role) || !identityArrayRole(role) {
			t.Fatalf("array role %q", role)
		}
	}
	for _, role := range []string{"", "pixels", "changed", "report", "calibration", "consents", "../before"} {
		if identityInputRole(role) || identityArrayRole(role) {
			t.Fatalf("unauthorized role %q", role)
		}
	}
}

func TestIdentityPixelVerifierRejectsUnboundInputs(t *testing.T) {
	inputCatalog := &identityInputCatalogFixture{items: map[string]objectstore.Object{}, reads: map[string]int{}}
	outputCatalog := &identityResultCatalogFixture{items: map[string]objectstore.Object{}, reads: map[string]int{}}
	_, err := VerifyIdentityPublishedPixels(context.Background(), IdentityResultScope{},
		IdentityResultInputs{}, IdentityPixelDocuments{}, VerifiedIdentityResult{},
		inputCatalog, outputCatalog, restorationArrayReaderFixture{})
	if err != v2input.ErrInvalid {
		t.Fatalf("unbound result accepted: %v", err)
	}
	if len(inputCatalog.reads) != 0 || len(outputCatalog.reads) != 0 {
		t.Fatal("catalog read before binding")
	}
}
