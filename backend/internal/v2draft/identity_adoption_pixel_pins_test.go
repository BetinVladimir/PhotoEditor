package v2draft

import (
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestIdentityPixelPinRefRequiresPresentOptionalAlpha(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	docs := IdentityPixelDocuments{BeforePixels: ref(), AfterPixels: ref(),
		EffectivePixels: ref(), ProtectedPixels: ref()}
	for role, want := range map[string]v2input.Ref{
		"before_pixels": docs.BeforePixels, "after_pixels": docs.AfterPixels,
		"effective_pixels": docs.EffectivePixels, "protected_pixels": docs.ProtectedPixels} {
		got, ok := identityPixelPinRef(docs, role)
		if !ok || got != want {
			t.Fatalf("lost pin for %s", role)
		}
	}
	if _, ok := identityPixelPinRef(docs, "before_alpha"); ok {
		t.Fatal("accepted absent alpha")
	}
	if _, ok := identityPixelPinRef(docs, "foreign"); ok {
		t.Fatal("accepted foreign pixel role")
	}
	alpha := ref()
	docs.BeforeAlpha = &alpha
	if got, ok := identityPixelPinRef(docs, "before_alpha"); !ok || got != alpha {
		t.Fatal("lost present alpha pin")
	}
}
