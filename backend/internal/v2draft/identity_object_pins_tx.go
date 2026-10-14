package v2draft

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// CheckIdentityObjectPinsInTx locks the exact registered input and output
// occurrences whose bytes were physically verified before adoption. The
// caller must retain this transaction through the adoption commit.
func CheckIdentityObjectPinsInTx(ctx context.Context, tx pgx.Tx,
	evidence PreparedIdentityAdoption, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	scope := evidence.Scope
	if tx == nil || !evidence.verified || scope.Tenant == uuid.Nil || scope.Task == uuid.Nil ||
		scope.Result == uuid.Nil || !isDocumentRef(scope.Root) ||
		len(bucket) < 3 || len(bucket) > 63 ||
		evidence.Result.Report != scope.Root ||
		evidence.Inputs.ParamsRef.ArtifactID == uuid.Nil ||
		len(evidence.PixelObjects) != 4+boolToInt(evidence.Documents.BeforeAlpha != nil)+boolToInt(evidence.Documents.AfterAlpha != nil) {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", scope.Tenant.String()); err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	inputs := []struct {
		role string
		ref  v2input.Ref
		pin  objectstore.Object
	}{
		{"params", evidence.Inputs.ParamsRef, evidence.Inputs.Objects[0]},
		{"before", evidence.Documents.BeforeRef, evidence.Documents.Objects[0]},
		{"after", evidence.Documents.AfterRef, evidence.Documents.Objects[1]},
		{"effective", evidence.Documents.EffectiveRef, evidence.Documents.Objects[2]},
		{"protected", evidence.Documents.ProtectedRef, evidence.Documents.Objects[3]},
		{"consents", evidence.Consents.Ref, evidence.Consents.Object},
		{"calibration", evidence.Calibration.Ref, evidence.Calibration.Object},
	}
	for _, pixelRole := range []string{"before_pixels", "after_pixels", "effective_pixels", "protected_pixels", "before_alpha", "after_alpha"} {
		ref, present := identityPixelPinRef(evidence.Documents, pixelRole)
		pin, pinned := evidence.PixelObjects[pixelRole]
		if present != pinned {
			return v2input.ErrInvalid
		}
		if present {
			inputs = append(inputs, struct {
				role string
				ref  v2input.Ref
				pin  objectstore.Object
			}{pixelRole, ref, pin})
		}
	}
	for _, item := range inputs {
		if !isDocumentRef(item.ref) || item.pin.SHA256 != item.ref.SHA256 ||
			item.pin.Key == "" || item.pin.VersionID == "" || item.pin.VersionID == "null" {
			return v2input.ErrInvalid
		}
		var raw []byte
		var err error
		switch item.role {
		case "consents":
			err = tx.QueryRow(ctx, `SELECT photo.v23_identity_consent_result_input($1,$2,$3,$4,$5,$6,$7,$8)`,
				scope.Tenant, scope.Task, scope.Result, scope.Root.ArtifactID, scope.Root.SHA256,
				item.ref.ArtifactID, item.ref.SHA256, bucket).Scan(&raw)
		case "calibration":
			err = tx.QueryRow(ctx, `SELECT photo.v23_identity_calibration_result_input($1,$2,$3,$4,$5,$6,$7,$8)`,
				scope.Tenant, scope.Task, scope.Result, scope.Root.ArtifactID, scope.Root.SHA256,
				item.ref.ArtifactID, item.ref.SHA256, bucket).Scan(&raw)
		default:
			err = tx.QueryRow(ctx, `SELECT photo.v23_identity_result_input($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
				scope.Tenant, scope.Task, scope.Result, scope.Root.ArtifactID, scope.Root.SHA256,
				item.role, item.ref.ArtifactID, item.ref.SHA256, bucket).Scan(&raw)
		}
		if err != nil {
			return err
		}
		current, err := parseRestorationLocator(raw)
		if err != nil || current != item.pin {
			return v2input.ErrExpired
		}
	}
	for i, item := range []struct {
		role string
		ref  v2input.Ref
	}{
		{"report", evidence.Result.Report}, {"mask", evidence.Result.Mask},
		{"pixels", evidence.Result.Pixels}} {
		if !isDocumentRef(item.ref) || evidence.Result.Objects[i].SHA256 != item.ref.SHA256 {
			return v2input.ErrInvalid
		}
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT photo.v23_identity_result_object($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			scope.Tenant, scope.Task, scope.Result, scope.Root.ArtifactID, scope.Root.SHA256,
			item.role, item.ref.ArtifactID, item.ref.SHA256, bucket).Scan(&raw)
		if err != nil {
			return err
		}
		current, err := parseRestorationLocator(raw)
		if err != nil || current != evidence.Result.Objects[i] {
			return v2input.ErrExpired
		}
	}
	return ctx.Err()
}
