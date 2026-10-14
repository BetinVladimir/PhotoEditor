package v2draft

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type RestorationBootstrap struct {
	Source, Target, Protected []byte
}

// RestorationManualOrigin is a locator for replay of the original manual
// rasterizer. It is not an authorization token; graph seal must recheck SQL.
type RestorationManualOrigin struct {
	Root           v2input.Ref `json:"root"`
	Params         v2input.Ref `json:"params"`
	TargetPixel    v2input.Ref `json:"target_pixel"`
	ProtectedPixel v2input.Ref `json:"protected_pixel"`
}

type RestorationResolvedMaterial struct {
	Documents v2portrait.RestorationDocuments
	Origin    RestorationManualOrigin
}

type RestorationResolvedDraft struct {
	Draft  v2portrait.RestorationDraft
	Origin RestorationManualOrigin
}

// BootstrapRestorationManualMasks reads only version-scoped mask documents
// under editor authorization. The bytes are input to the independent SQL
// original-producer check; they are never admission evidence by themselves.
func BootstrapRestorationManualMasks(ctx context.Context, who auth.Identity,
	tenant uuid.UUID, base v2input.SessionVersionBinding,
	targetRef, guardRef v2input.Ref, catalog ArtifactCatalog,
	objects v2input.SessionAnalysisObjectReader) (RestorationBootstrap, error) {
	var zero RestorationBootstrap
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || tenant == uuid.Nil ||
		!isDocumentRef(targetRef) || !isDocumentRef(guardRef) ||
		targetRef == guardRef {
		return zero, v2input.ErrInvalid
	}
	resolver := ClosureResolver{Catalog: catalog, Objects: objects}
	sourceRaw, err := resolver.ReadLocalPortraitSource(ctx, who, tenant, base)
	if err != nil {
		return zero, err
	}
	source, err := v2portrait.VerifyDonorWorkingImage(base, sourceRaw)
	if err != nil {
		return zero, err
	}
	refs := []v2input.Ref{targetRef, guardRef}
	raw := make([][]byte, 2)
	locators := make([]objectstore.Object, 2)
	for i, ref := range refs {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		binding := base
		node := LayerRef{Path: "Restoration.bootstrap", Contract: "MaskRef",
			Ref: ref, CanvasHash: base.CanvasHash, ExpectedBinding: &binding}
		object, e := catalog.Lookup(ctx, who, tenant, node)
		if e != nil {
			return zero, e
		}
		if object.Bytes < 2 || object.Bytes > 1048576 || object.ContentType != "application/json" ||
			object.SHA256 != ref.SHA256 || object.VersionID == "" || object.VersionID == "null" {
			return zero, v2input.ErrInvalid
		}
		body, e := readJSONArtifact(ctx, objects, object, ref)
		if e != nil {
			return zero, e
		}
		mask, e := review.ParseMaskRef(body)
		if e != nil || mask.Canvas != source.Canvas ||
			!bytes.Equal(mask.TransformChain, source.TransformChain) || mask.Protected != (i == 1) {
			return zero, v2input.ErrInvalid
		}
		raw[i], locators[i] = body, object
	}
	for i, ref := range refs {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		binding := base
		current, e := catalog.Lookup(ctx, who, tenant, LayerRef{Path: "Restoration.bootstrap",
			Contract: "MaskRef", Ref: ref, CanvasHash: base.CanvasHash, ExpectedBinding: &binding})
		if e != nil {
			return zero, e
		}
		if current != locators[i] {
			return zero, v2input.ErrExpired
		}
	}
	return RestorationBootstrap{Source: sourceRaw, Target: raw[0], Protected: raw[1]}, nil
}

// ResolveRestorationManualEvidence executes read -> SQL producer lock -> scoped
// reread. No database transaction spans object IO. Final graph admission must
// repeat the same source/mask/consent and qualification checks.
func ResolveRestorationManualEvidence(ctx context.Context,
	db v2portrait.DonorConfirmationTransactions, who auth.Identity,
	tenant, project, snapshot uuid.UUID, operation string, paramsRaw []byte,
	baseCatalog, donorCatalog ArtifactCatalog,
	objects v2input.SessionAnalysisObjectReader) (RestorationResolvedMaterial, error) {
	var zero RestorationResolvedMaterial
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	var p struct {
		Base        v2input.SessionVersionBinding `json:"base"`
		Mask        v2input.Ref                   `json:"mask"`
		GlassesMask v2input.Ref                   `json:"glasses_mask"`
		Protected   v2input.Ref                   `json:"protected"`
	}
	if json.Unmarshal(paramsRaw, &p) != nil {
		return zero, v2input.ErrInvalid
	}
	target := p.Mask
	if operation == "portrait.glare" {
		target = p.GlassesMask
	}
	if operation != "portrait.glare" && operation != "image.reflection" {
		return zero, v2input.ErrInvalid
	}
	bootstrap, err := BootstrapRestorationManualMasks(ctx, who, tenant, p.Base, target,
		p.Protected, baseCatalog, objects)
	if err != nil {
		return zero, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	scopes, err := LockRestorationManualScopes(ctx, tx, who, tenant, project, snapshot,
		p.Base, bootstrap.Source, bootstrap.Target, bootstrap.Protected, target, p.Protected)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	loader, err := NewRestorationManualMaterialLoader(baseCatalog, donorCatalog, objects, scopes)
	if err != nil {
		return zero, err
	}
	documents, err := loader.Load(ctx, who, tenant, operation, paramsRaw)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(documents.Source, bootstrap.Source) ||
		!bytes.Equal(documents.Target, bootstrap.Target) ||
		!bytes.Equal(documents.Protected, bootstrap.Protected) {
		return zero, v2input.ErrExpired
	}
	return RestorationResolvedMaterial{Documents: documents,
		Origin: RestorationManualOrigin{Root: scopes.RootRef(),
			Params:         scopes.OriginalParamsRef(),
			TargetPixel:    scopes.TargetPixelRef(),
			ProtectedPixel: scopes.ProtectedPixelRef()}}, nil
}

// ResolveRestorationManualMaterial preserves the document-only interface for
// callers that do not yet stage a durable restoration descriptor.
func ResolveRestorationManualMaterial(ctx context.Context,
	db v2portrait.DonorConfirmationTransactions, who auth.Identity,
	tenant, project, snapshot uuid.UUID, operation string, paramsRaw []byte,
	baseCatalog, donorCatalog ArtifactCatalog,
	objects v2input.SessionAnalysisObjectReader) (v2portrait.RestorationDocuments, error) {
	resolved, err := ResolveRestorationManualEvidence(ctx, db, who, tenant,
		project, snapshot, operation, paramsRaw, baseCatalog, donorCatalog, objects)
	if err != nil {
		return v2portrait.RestorationDocuments{}, err
	}
	return resolved.Documents, nil
}

// CompileResolvedRestorationDraftTask is the publication-facing manual-mask
// path. It binds the SQL producer result to exact material documents before
// compiling the frozen task. Durable admission still rechecks live authority.
func CompileResolvedRestorationDraftTask(ctx context.Context,
	db v2portrait.DonorConfirmationTransactions, who auth.Identity,
	tenant, project, snapshot, workflow uuid.UUID, ordinal int,
	operation string, paramsRaw, baseProfile []byte,
	pin v2portrait.RestorationQualificationPin, paramsRef v2input.Ref,
	inventory []v2input.Entry, baseCatalog, donorCatalog ArtifactCatalog,
	objects v2input.SessionAnalysisObjectReader) (RestorationResolvedDraft, error) {
	resolved, err := ResolveRestorationManualEvidence(ctx, db, who, tenant,
		project, snapshot, operation, paramsRaw, baseCatalog, donorCatalog, objects)
	if err != nil {
		return RestorationResolvedDraft{}, err
	}
	draft, err := v2portrait.CompileVerifiedRestorationDraftTask(ctx, workflow, ordinal,
		operation, paramsRaw, baseProfile, pin, paramsRef, inventory, resolved.Documents)
	if err != nil {
		return RestorationResolvedDraft{}, err
	}
	return RestorationResolvedDraft{Draft: draft, Origin: resolved.Origin}, nil
}
