package interchange

import (
	"context"
	"crypto/ed25519"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestPreparePortablePersonOriginAttachmentBindsSignedBytesAndEntry(t *testing.T) {
	issuer, scope, _, _, _, _ := personOriginIssuerFixture(t)
	prepared, err := PreparePortablePersonOriginAttachment(context.Background(), issuer, scope)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Ref.SHA256 != rawSHA(prepared.Body) || prepared.Mapping.EntryID != scope.EntryID ||
		prepared.Mapping.Recipe != scope.Recipe || prepared.Mapping.Override != scope.Override ||
		prepared.Mapping.MaskRef != scope.MaskRef || prepared.Mapping.MaskPixels != scope.MaskPixels ||
		prepared.Mapping.Path != prepared.Path || prepared.Mapping.SHA != prepared.Ref.SHA256 ||
		!validImportPlanPath(prepared.Path) {
		t.Fatal("attachment lost signed scope")
	}
	owned := prepared.OwnedBody()
	owned[0] = '!'
	if prepared.Body[0] == '!' {
		t.Fatal("receipt body leaked mutable storage")
	}
	if _, err := VerifyPortablePersonOrigin(prepared.Body, scope,
		map[PortablePersonOriginKey]ed25519.PublicKey{{IssuerID: issuer.issuerID, KeyID: issuer.keyID}: issuer.public}); err != nil {
		t.Fatal("signed attachment failed verification", err)
	}
	if _, err := PreparePortablePersonOriginAttachment(context.Background(), nil, scope); err != v2input.ErrInvalid {
		t.Fatal("missing issuer accepted", err)
	}
}
