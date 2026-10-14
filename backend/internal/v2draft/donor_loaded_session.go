package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"reflect"
)

// LockLoadedDonorSession rechecks the exact loaded plan before acquiring current
// identity/session locks in the caller's scheduler transaction. The caller must
// keep that transaction open through graph admission. This is not admission by
// itself: complete closure, geometry, qualification and task fencing are required.
func LockLoadedDonorSession(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenant, project, shootSession uuid.UUID, loaded LoadedDonorTaskPlan, confirmationRaw []byte) error {
	params, err := validateLoadedDonorPlan(ctx, loaded)
	if err != nil {
		return err
	}

	return v2portrait.LockOpenEyesDonorSession(ctx, tx, who, tenant, project, shootSession, params, confirmationRaw)
}

func validateLoadedDonorPlan(ctx context.Context, loaded LoadedDonorTaskPlan) (v2portrait.OpenEyesParams, error) {
	if err := ctx.Err(); err != nil {
		return v2portrait.OpenEyesParams{}, err
	}
	if len(loaded.Plan.Params) < 2 || len(loaded.Plan.Params) > 65536 || loaded.Plan.Operation != "portrait.open_eyes" {
		return v2portrait.OpenEyesParams{}, v2input.ErrInvalid
	}
	var params v2portrait.OpenEyesParams
	if json.Unmarshal(loaded.Plan.Params, &params) != nil {
		return v2portrait.OpenEyesParams{}, v2input.ErrInvalid
	}
	body, digest, err := params.Compile()
	if err != nil || digest != loaded.Plan.ParamsSHA || !bytes.Equal(body, loaded.Plan.Params) {
		return v2portrait.OpenEyesParams{}, v2input.ErrInvalid
	}
	// Recompile every derived field from the loader's original compilation
	// inputs; a mutable profile, reserve or inventory must not reach SQL locks.
	expected, err := v2portrait.CompileDonorTaskPlan(ctx, params.Base, params.Donor, loaded.Images.SourceRaw, loaded.Images.DonorRaw,
		body, loaded.patchWidth, loaded.patchHeight, loaded.runtimeSHA, loaded.capacity)
	if err != nil {
		return v2portrait.OpenEyesParams{}, err
	}
	if !reflect.DeepEqual(expected, loaded.Plan) {
		return v2portrait.OpenEyesParams{}, v2input.ErrInvalid
	}
	// Reparse manifest bytes, not cached typed structs, and require the exact
	// occurrence list used by the loaded plan and catalog-read evidence.
	if _, err := v2portrait.DonorImageInputs(params, loaded.Images.SourceRaw, loaded.Images.DonorRaw); err != nil {
		return v2portrait.OpenEyesParams{}, err
	}
	occurrences, err := v2portrait.DonorImageOccurrences(params, loaded.Images.SourceRaw, loaded.Images.DonorRaw)
	if err != nil {
		return v2portrait.OpenEyesParams{}, err
	}
	if len(occurrences) != len(loaded.Images.Physical) || len(occurrences) != len(loaded.Plan.PhysicalImageOccurrences) {
		return v2portrait.OpenEyesParams{}, v2input.ErrInvalid
	}
	for i, occurrence := range occurrences {
		if occurrence != loaded.Images.Physical[i].Occurrence || occurrence != loaded.Plan.PhysicalImageOccurrences[i] {
			return v2portrait.OpenEyesParams{}, v2input.ErrInvalid
		}
	}
	return params, nil
}

// LockLoadedDonorSessionFromStore loads the exact server-issued confirmation
// through a provenance-aware catalog before taking scheduler transaction locks.
// The catalog must authorize DonorConfirmation under the source parent; the
// SQL draft catalog requires ready issuer lineage and current consent/session
// scope. No fallback to tenant-only lookup is permitted.
func LockLoadedDonorSessionFromStore(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenant, project, shootSession uuid.UUID, loaded LoadedDonorTaskPlan,
	catalog ArtifactCatalog, objects v2input.SessionAnalysisObjectReader) error {
	params, err := validateLoadedDonorPlan(ctx, loaded)
	if err != nil {
		return err
	}
	if catalog == nil || objects == nil || tenant == uuid.Nil || project == uuid.Nil || shootSession == uuid.Nil || tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	binding := params.Base
	object, err := catalog.Lookup(ctx, who, tenant, LayerRef{Contract: "DonorConfirmation", Ref: params.Confirmation,
		ParentRef: params.Base.Source, ExpectedBinding: &binding, CanvasHash: binding.CanvasHash})
	if err != nil {
		return err
	}
	if object.Bytes > 65536 {
		return v2input.ErrInvalid
	}
	raw, err := readJSONArtifact(ctx, objects, object, params.Confirmation)
	if err != nil {
		return err
	}
	// Revalidate the plan after catalog/reader callbacks, before issuing SQL.
	return LockLoadedDonorSession(ctx, tx, who, tenant, project, shootSession, loaded, raw)
}

func validDonorConfirmationCatalogNode(node LayerRef, version uuid.UUID) bool {
	b := node.ExpectedBinding
	return node.Contract == "DonorConfirmation" && b != nil && version != uuid.Nil && b.AssetVersionID == version &&
		isDocumentRef(node.Ref) && isDocumentRef(b.Source) && node.ParentRef == b.Source && node.CanvasHash == b.CanvasHash &&
		isDocumentRef(v2input.Ref{ArtifactID: version, SHA256: b.CanvasHash}) && node.RenderFrame == nil
}

// LockLoadedDonorFaceSession rechecks loaded face bytes and derived geometry
// before the live scheduler session/consent locks. Calibration qualification,
// complete mask closure and graph registration remain separate requirements.
func LockLoadedDonorFaceSession(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenant, project, shootSession uuid.UUID, loaded LoadedDonorTaskPlan, evidence LoadedDonorFaceEvidence) error {
	confirmation := append([]byte(nil), evidence.ConfirmationRaw...)
	sourceFaces := append([]byte(nil), evidence.SourceFacesRaw...)
	donorFaces := append([]byte(nil), evidence.DonorFacesRaw...)
	geometry, err := VerifyLoadedDonorFaces(ctx, loaded, confirmation, sourceFaces, donorFaces, evidence.minEdge)
	if err != nil {
		return err
	}
	if geometry != evidence.Geometry {
		return v2input.ErrInvalid
	}
	return LockLoadedDonorSession(ctx, tx, who, tenant, project, shootSession, loaded, confirmation)
}
