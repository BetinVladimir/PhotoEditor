package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type relightManualPreintentWitness struct {
	origin                  RelightPreintentProducer
	selection, selectedBody string
	objects                 map[string]relightProducerLocator
}

type SQLRelightPreintentManualReader struct {
	DB      DonorConfirmationTransactions
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
	Limits  RelightManualRasterLimits
	Budget  *RelightManualRasterBudget
}

func (s SQLRelightPreintentManualReader) object(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, params RelightParams, documents RelightPreintentDocuments, selection, kind string) (relightProducerLocator, error) {
	var zero relightProducerLocator
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	if err = LockRelightSource(ctx, tx, who, tenant, project, snapshot, params.Base); err != nil {
		return zero, err
	}
	baseRaw, err := canonical.Marshal(params.Base)
	if err != nil {
		return zero, err
	}
	selectedRaw, err := canonical.Marshal(manualLocatorRef(documents.Origin.Selected))
	if err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.v28_relight_preintent_manual_object($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10,$11)", tenant, project, snapshot, who.UserID, who.SessionID, string(baseRaw), string(selectedRaw), selection, string(documents.SelectedBody), string(documents.RootBody), kind).Scan(&raw); err != nil {
		return zero, err
	}
	var result relightProducerLocator
	if len(raw) < 2 || len(raw) > 16384 || json.Unmarshal(raw, &result) != nil {
		return zero, v2input.ErrExpired
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
	}
	encoded, err := canonical.Marshal(result)
	media := "application/octet-stream"
	minBytes, maxBytes := int64(32), int64(800065536)
	if kind == "vectors" {
		media = "application/json"
		minBytes = 2
		maxBytes = 1048576
	}
	if err != nil || !bytes.Equal(normalized, encoded) || result.ArtifactID == uuid.Nil || result.Bucket != s.Bucket || result.Key == "" || result.Version == "" || result.Version == "null" || result.Media != media || result.Bytes < minBytes || result.Bytes > maxBytes {
		return zero, v2input.ErrInvalid
	}
	if _, err = domain.ParseDigest(result.SHA256); err != nil {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

// Load resolves all original manual dependencies and verifies paired raster
// pixels before immutable intent creation. Final admission must retain current
// authority independently; one request budget must span all groups/retries.
func (s SQLRelightPreintentManualReader) Load(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, params RelightParams, sourceRaw []byte, protected bool) (RelightManualProducer, error) {
	var zero RelightManualProducer
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || s.Budget == nil {
		return zero, v2input.ErrInvalid
	}
	raw, _, err := params.Compile()
	if err != nil {
		return zero, err
	}
	var owned RelightParams
	if json.Unmarshal(raw, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	params = owned
	if len(sourceRaw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	sourceRaw = append([]byte(nil), sourceRaw...)
	selection := "manual_target"
	if protected {
		selection = "manual_guard"
	}
	baseReader := SQLRelightPreintentReader{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket}
	documents, err := baseReader.Load(ctx, who, tenant, project, snapshot, params, sourceRaw, selection)
	if err != nil {
		return zero, err
	}
	rootRef, originalRef := manualLocatorRef(documents.Origin.Root), manualLocatorRef(documents.Origin.Params)
	root, err := review.ParseManualRasterDocument(documents.RootBody, review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256})
	if err != nil {
		return zero, err
	}
	locators := map[string]relightProducerLocator{}
	var vectorsBody []byte
	if documents.ExternalVectors != nil {
		locators["vectors"], err = s.object(ctx, who, tenant, project, snapshot, params, documents, selection, "vectors")
		if err != nil {
			return zero, err
		}
		if manualLocatorRef(locators["vectors"]) != *documents.ExternalVectors {
			return zero, v2input.ErrInvalid
		}
		vectorsBody, err = (SQLRelightManualReader{Objects: s.Objects}).document(ctx, locators["vectors"])
		if err != nil {
			return zero, err
		}
	}
	plan, err := ResolveRelightManualPlan(ctx, params, sourceRaw, documents.SelectedBody, documents.ParamsBody, documents.RootBody, vectorsBody, originalRef, rootRef, protected)
	if err != nil {
		return zero, err
	}
	if err = s.Budget.reserve(ctx, plan, s.Limits); err != nil {
		return zero, err
	}
	for _, kind := range []string{"target", "guard"} {
		locators[kind], err = s.object(ctx, who, tenant, project, snapshot, params, documents, selection, kind)
		if err != nil {
			return zero, err
		}
	}
	if manualLocatorRef(locators["target"]) != (v2input.Ref{ArtifactID: root.Target.Artifact.ID, SHA256: root.Target.Artifact.SHA256}) || manualLocatorRef(locators["guard"]) != (v2input.Ref{ArtifactID: root.Protected.Artifact.ID, SHA256: root.Protected.Artifact.SHA256}) {
		return zero, v2input.ErrInvalid
	}
	if err = VerifyRelightManualPixels(ctx, s.Objects, plan, manualLocatorObject(locators["target"]), manualLocatorObject(locators["guard"]), s.Limits); err != nil {
		return zero, err
	}
	inputs := []v2input.Entry{{Ref: rootRef, Contract: "RasterizedMask"}, {Ref: originalRef, Contract: "Params"}}
	for _, kind := range []string{"vectors", "target", "guard"} {
		before, ok := locators[kind]
		if !ok {
			continue
		}
		after, e := s.object(ctx, who, tenant, project, snapshot, params, documents, selection, kind)
		if e != nil {
			return zero, e
		}
		if after != before {
			return zero, v2input.ErrExpired
		}
		contract := "NpyArray"
		if kind == "vectors" {
			contract = "ManualMask"
		}
		inputs = append(inputs, v2input.Entry{Ref: manualLocatorRef(before), Contract: contract})
	}
	after, err := baseReader.lookup(ctx, who, tenant, project, snapshot, params.Base, manualLocatorRef(documents.Origin.Selected), selection)
	if err != nil {
		return zero, err
	}
	if after != documents.Origin {
		return zero, v2input.ErrExpired
	}
	inputs, err = mergeRelightInputs(inputs)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return RelightManualProducer{Root: rootRef, Original: originalRef, RootBody: documents.RootBody, ParamsBody: documents.ParamsBody, VectorsBody: vectorsBody, Inputs: inputs, preintent: &relightManualPreintentWitness{origin: documents.Origin, selection: selection, selectedBody: string(documents.SelectedBody), objects: locators}}, nil
}
