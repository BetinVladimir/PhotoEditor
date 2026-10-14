package v2draft

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// RestorationMaterialLoader accepts only separately scoped version catalogs.
// Mask scopes must originate from current producer/mask-origin authority, not
// from caller Params. The catalog checks the exact binding and object version.
type RestorationMaterialLoader struct {
	BaseCatalog    ArtifactCatalog
	DonorCatalog   ArtifactCatalog
	Objects        v2input.SessionAnalysisObjectReader
	targetScope    LayerRef
	protectedScope LayerRef
	originVerified bool
}

func NewRestorationManualMaterialLoader(base, donor ArtifactCatalog,
	objects v2input.SessionAnalysisObjectReader,
	scopes RestorationManualScopes) (RestorationMaterialLoader, error) {
	if !scopes.verified || !isDocumentRef(scopes.root) ||
		!isDocumentRef(scopes.original) || scopes.root == scopes.original ||
		scopes.target.ParentRef != (v2input.Ref{}) ||
		scopes.protected.ParentRef != (v2input.Ref{}) ||
		scopes.target.ExpectedBinding == nil ||
		scopes.protected.ExpectedBinding == nil ||
		base == nil || objects == nil {
		return RestorationMaterialLoader{}, v2input.ErrInvalid
	}
	target, protected := scopes.target, scopes.protected
	targetBinding, protectedBinding := *target.ExpectedBinding, *protected.ExpectedBinding
	target.ExpectedBinding = &targetBinding
	protected.ExpectedBinding = &protectedBinding
	return RestorationMaterialLoader{BaseCatalog: base, DonorCatalog: donor,
		Objects: objects, targetScope: target,
		protectedScope: protected, originVerified: true}, nil
}

type restorationPinnedDocument struct {
	catalog ArtifactCatalog
	node    LayerRef
	object  objectstore.Object
}

// Load reads four canonical JSON documents from immutable object versions and
// repeats every catalog scope after IO. It never accepts supplied JSON bodies.
func (s RestorationMaterialLoader) Load(ctx context.Context, who auth.Identity,
	tenant uuid.UUID, operation string, paramsRaw []byte) (v2portrait.RestorationDocuments, error) {
	var zero v2portrait.RestorationDocuments
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !s.originVerified || tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		s.BaseCatalog == nil || s.Objects == nil || len(paramsRaw) < 2 || len(paramsRaw) > 65536 ||
		(operation != "image.reflection" && operation != "portrait.glare") {
		return zero, v2input.ErrInvalid
	}
	var params struct {
		Base        v2input.SessionVersionBinding  `json:"base"`
		Donor       *v2input.SessionVersionBinding `json:"donor"`
		Mask        v2input.Ref                    `json:"mask"`
		GlassesMask v2input.Ref                    `json:"glasses_mask"`
		Protected   v2input.Ref                    `json:"protected"`
		Mode        string                         `json:"mode"`
	}
	if json.Unmarshal(paramsRaw, &params) != nil || params.Base.AssetVersionID == uuid.Nil ||
		!isDocumentRef(params.Base.Source) {
		return zero, v2input.ErrInvalid
	}
	if operation == "portrait.glare" {
		if params.Mode == "donor" && (params.Donor == nil || s.DonorCatalog == nil ||
			params.Donor.AssetVersionID == uuid.Nil || !isDocumentRef(params.Donor.Source)) {
			return zero, v2input.ErrInvalid
		}
		if params.Mode != "donor" && (params.Mode != "generative" || params.Donor != nil) {
			return zero, v2input.ErrInvalid
		}
	} else if params.Donor != nil || params.Mode != "" {
		return zero, v2input.ErrInvalid
	}
	targetRef := params.Mask
	if operation == "portrait.glare" {
		targetRef = params.GlassesMask
	}
	if !isDocumentRef(targetRef) || !isDocumentRef(params.Protected) ||
		s.targetScope.Contract != "MaskRef" || s.targetScope.Ref != targetRef ||
		s.protectedScope.Contract != "MaskRef" || s.protectedScope.Ref != params.Protected ||
		s.targetScope.CanvasHash != params.Base.CanvasHash ||
		s.protectedScope.CanvasHash != params.Base.CanvasHash ||
		s.targetScope.ExpectedBinding == nil ||
		*s.targetScope.ExpectedBinding != params.Base ||
		s.protectedScope.ExpectedBinding == nil ||
		*s.protectedScope.ExpectedBinding != params.Base ||
		s.targetScope.ParentRef != (v2input.Ref{}) ||
		s.protectedScope.ParentRef != (v2input.Ref{}) {
		return zero, v2input.ErrInvalid
	}
	base := params.Base
	documents := []restorationPinnedDocument{{catalog: s.BaseCatalog, node: LayerRef{
		Path: "TaskInputs.source", Contract: "WorkingImage", Ref: base.Source,
		CanvasHash: base.CanvasHash, ExpectedBinding: &base}}}
	if params.Donor != nil {
		donor := *params.Donor
		documents = append(documents, restorationPinnedDocument{catalog: s.DonorCatalog,
			node: LayerRef{Path: "TaskInputs.donor", Contract: "WorkingImage", Ref: donor.Source,
				CanvasHash: donor.CanvasHash, ExpectedBinding: &donor}})
	}
	documents = append(documents,
		restorationPinnedDocument{catalog: s.BaseCatalog, node: s.targetScope},
		restorationPinnedDocument{catalog: s.BaseCatalog, node: s.protectedScope})
	bodies := make([][]byte, len(documents))
	for i := range documents {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		owned := &documents[i]
		object, err := owned.catalog.Lookup(ctx, who, tenant, owned.node)
		if err != nil {
			return zero, err
		}
		if object.Bytes < 2 || object.Bytes > 1048576 || object.SHA256 != owned.node.Ref.SHA256 ||
			object.ContentType != "application/json" || object.VersionID == "" ||
			object.VersionID == "null" {
			return zero, v2input.ErrInvalid
		}
		body, err := readJSONArtifact(ctx, s.Objects, object, owned.node.Ref)
		if err != nil {
			return zero, err
		}
		owned.object = object
		bodies[i] = body
	}
	for _, owned := range documents {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		current, err := owned.catalog.Lookup(ctx, who, tenant, owned.node)
		if err != nil {
			return zero, err
		}
		if current != owned.object {
			return zero, v2input.ErrExpired
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if params.Donor != nil {
		return v2portrait.RestorationDocuments{Source: bodies[0], Donor: bodies[1],
			Target: bodies[2], Protected: bodies[3]}, nil
	}
	return v2portrait.RestorationDocuments{Source: bodies[0], Target: bodies[1],
		Protected: bodies[2]}, nil
}

// CompileLoadedRestorationDraftTask owns the versioned reads and then requires
// the document-to-array closure before returning a draft for publication.
func (s RestorationMaterialLoader) CompileLoadedRestorationDraftTask(ctx context.Context,
	who auth.Identity, tenant, workflow uuid.UUID, ordinal int, operation string,
	paramsRaw, baseProfile []byte, pin v2portrait.RestorationQualificationPin,
	paramsRef v2input.Ref, inventory []v2input.Entry) (v2portrait.RestorationDraft, error) {
	documents, err := s.Load(ctx, who, tenant, operation, paramsRaw)
	if err != nil {
		return v2portrait.RestorationDraft{}, err
	}
	return v2portrait.CompileVerifiedRestorationDraftTask(ctx, workflow, ordinal,
		operation, paramsRaw, baseProfile, pin, paramsRef, inventory, documents)
}
