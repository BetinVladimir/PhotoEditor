package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type LocalPortraitMaterial struct {
	Source []byte
	Proof  LocalPortraitRecoveryProof
}

type SQLLocalPortraitMaterialLoader struct {
	DB       *pgxpool.Pool
	Resolver ClosureResolver
	Bucket   string
}

// ReadSource checks current catalog authority before and after exact immutable
// object reads. The resolver catalog must be configured for this project/version.
func (s ClosureResolver) ReadLocalPortraitSource(ctx context.Context, who auth.Identity, tenant uuid.UUID, base v2input.SessionVersionBinding) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Catalog == nil || s.Objects == nil || tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !isDocumentRef(base.Source) || base.AssetVersionID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	node := LayerRef{Path: "TaskInputs.source", Contract: "WorkingImage", Ref: base.Source, CanvasHash: base.CanvasHash}
	object, err := s.Catalog.Lookup(ctx, who, tenant, node)
	if err != nil {
		return nil, err
	}
	if object.Bytes > 1048576 {
		return nil, v2input.ErrInvalid
	}
	raw, err := readJSONArtifact(ctx, s.Objects, object, base.Source)
	if err != nil {
		return nil, err
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil || image.DType != "float32" {
		return nil, v2input.ErrInvalid
	}
	canvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil || canvasSHA != base.CanvasHash {
		return nil, v2input.ErrInvalid
	}
	current, err := s.Catalog.Lookup(ctx, who, tenant, node)
	if err != nil {
		return nil, err
	}
	if current != object {
		return nil, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

// Load obtains original manual producer documents through V28 SQL lineage,
// independently verifies pinned bytes and compares every returned origin pin.
// It does not inspect binary pixels or grant worker/admission authority.
func (s SQLLocalPortraitMaterialLoader) Load(ctx context.Context, who auth.Identity, tenant uuid.UUID, requestRaw []byte) (LocalPortraitMaterial, error) {
	var zero LocalPortraitMaterial
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Bucket == "" || len(requestRaw) < 2 || len(requestRaw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	var request LocalPortraitOwnerRequest
	if json.Unmarshal(requestRaw, &request) != nil || request.SchemaVersion != 1 || request.Tenant != tenant || request.Actor != who.UserID || request.Session != who.SessionID {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(exact, requestRaw) {
		return zero, v2input.ErrInvalid
	}
	source, err := s.Resolver.ReadLocalPortraitSource(ctx, who, tenant, request.Intent.Binding)
	if err != nil {
		return zero, err
	}
	var masks struct {
		Mask      v2input.Ref `json:"mask"`
		Protected v2input.Ref `json:"protected"`
	}
	if json.Unmarshal([]byte(request.Intent.ParamsJCS), &masks) != nil {
		return zero, v2input.ErrInvalid
	}
	documents, err := v2portrait.ReadManualPairDocuments(ctx, s.DB, s.Resolver.Objects, who, tenant, request.Project, request.Snapshot, request.Intent.Binding, source, masks.Mask, masks.Protected, request.TargetPixels, request.GuardPixels, s.Bucket)
	if err != nil {
		return zero, err
	}
	if documents.Origin.Root != request.ManualRoot || documents.Origin.Params != request.OriginalParams {
		return zero, v2input.ErrInvalid
	}
	if request.Vectors == nil {
		if documents.VectorsContract != "Params" || documents.VectorsRef != request.OriginalParams {
			return zero, v2input.ErrInvalid
		}
	} else if documents.VectorsContract != "ManualMask" || documents.VectorsRef != *request.Vectors {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return LocalPortraitMaterial{Source: source, Proof: LocalPortraitRecoveryProof{Root: documents.Origin.Root, RootRaw: documents.RootRaw, OriginalParams: documents.Origin.Params, OriginalRaw: documents.ParamsRaw, VectorsRaw: documents.VectorsRaw}}, nil
}
