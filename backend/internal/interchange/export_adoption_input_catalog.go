package interchange

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SQLSchedulerExportInputCatalog resolves only typed inputs sealed into the
// exact canonical export task. It has no foreground session or draft fallback.
type SQLSchedulerExportInputCatalog struct {
	Claims SQLExportAdoptionClaimStore
	Claim  ExportAdoptionClaim
	Bucket string
}

func (s SQLSchedulerExportInputCatalog) LookupExportDocument(ctx context.Context,
	entry v2input.Entry) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || !validImportRef(entry.Ref) ||
		len(s.Bucket) < 3 || len(s.Bucket) > 63 || !validExportInputContract(entry.Contract) {
		return zero, v2input.ErrInvalid
	}
	current, err := s.Claims.CurrentExportAdoptionClaim(ctx, s.Claim)
	if err != nil {
		return zero, err
	}
	if !current {
		return zero, v2input.ErrInvalid
	}
	var foundBucket string
	var object objectstore.Object
	err = s.Claims.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type
 FROM photo.v30_export_adoption_result_input($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task, s.Claim.Result,
		s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA, s.Claim.Token,
		entry.Ref.ArtifactID, entry.Ref.SHA256, entry.Contract).Scan(
		&foundBucket, &object.Key, &object.VersionID, &object.SHA256,
		&object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, artifactjson.ErrStale
	}
	if err != nil {
		return zero, err
	}
	if foundBucket != s.Bucket || object.Key == "" || object.VersionID == "" ||
		object.VersionID == "null" || object.SHA256 != entry.Ref.SHA256 ||
		object.Bytes < 1 || object.Bytes > 128<<30 {
		return zero, objectstore.ErrIntegrity
	}
	current, err = s.Claims.CurrentExportAdoptionClaim(ctx, s.Claim)
	if err != nil {
		return zero, err
	}
	if !current {
		return zero, v2input.ErrInvalid
	}
	return object, ctx.Err()
}

func validExportInputContract(contract string) bool {
	switch contract {
	case "Params", "InterchangePlan", "InputSnapshot", "WorkingImage", "FrozenRenderRecipe",
		"EditingRecipeV3", "StyleV2", "CorrectionPlan", "Override", "LayerStack",
		"MaskRef", "CandidateProfile", "DescriptorShard", "FaceObservations",
		"ResolvedStyle", "Stats20", "SeriesCorrections", "PoseSet", "BodyFit",
		"PairAlignment", "FaceBoxes", "FaceLandmarks", "AbsoluteLUTArray",
		"DustCandidates", "DefectCandidates", "DefectReview", "DefectLabelMap",
		"StyleBackgroundAsset",
		"EncodedImage", "WorkingPixels", "WorkingAlpha", "PixelArray", "MaskPixels",
		"MaskArray", "DenseWarpField", "DenseWarpArray":
		return true
	default:
		return false
	}
}
