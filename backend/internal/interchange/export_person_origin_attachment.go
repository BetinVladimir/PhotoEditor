package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// PreparedPersonOriginAttachment is owned canonical receipt data. A publisher
// must persist Body as an immutable versioned object at Ref, then prove the
// stored bytes and current issuer/source authority before adding it to ZIP.
type PreparedPersonOriginAttachment struct {
	Ref     v2input.Ref
	Path    string
	Body    []byte
	Mapping importBundlePersonOrigin
}

func (p PreparedPersonOriginAttachment) OwnedBody() []byte {
	return append([]byte(nil), p.Body...)
}

func PreparePortablePersonOriginAttachment(ctx context.Context, issuer *PortablePersonOriginIssuer, scope PortablePersonOriginExpectation) (PreparedPersonOriginAttachment, error) {
	var zero PreparedPersonOriginAttachment
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if issuer == nil || scope.EntryID == uuid.Nil || !validImportRef(scope.Recipe) ||
		!validImportRef(scope.Override) || !validImportRef(scope.MaskRef) || !validImportRef(scope.MaskPixels) {
		return zero, v2input.ErrInvalid
	}
	body, err := issuer.Issue(ctx, scope)
	if err != nil {
		return zero, err
	}
	if len(body) < 2 || len(body) > 16384 {
		return zero, v2input.ErrInvalid
	}
	path := "origins/" + scope.EntryID.String() + "/" + scope.MaskRef.ArtifactID.String() + ".json"
	if !validImportPlanPath(path) {
		return zero, v2input.ErrInvalid
	}
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(scope.EntryID, []byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String())), SHA256: rawSHA(body)}
	mapping := importBundlePersonOrigin{EntryID: scope.EntryID, Recipe: scope.Recipe,
		Override: scope.Override, MaskRef: scope.MaskRef, MaskPixels: scope.MaskPixels,
		Path: path, SHA: ref.SHA256}
	return PreparedPersonOriginAttachment{Ref: ref, Path: path, Body: append([]byte(nil), body...), Mapping: mapping}, ctx.Err()
}
