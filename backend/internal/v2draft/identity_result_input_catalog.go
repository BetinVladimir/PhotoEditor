package v2draft

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type IdentityResultInputCatalog interface {
	ResolveIdentityResultInput(context.Context, IdentityResultScope, string, v2input.Ref) (objectstore.Object, error)
}

func (c SQLIdentityResultCatalog) ResolveIdentityResultInput(ctx context.Context,
	scope IdentityResultScope, role string, ref v2input.Ref) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 ||
		scope.Tenant == uuid.Nil || scope.Task == uuid.Nil || scope.Result == uuid.Nil ||
		!isDocumentRef(scope.Root) || !isDocumentRef(ref) ||
		!identityInputRole(role) {
		return zero, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := c.DB.Begin(bounded)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", scope.Tenant.String()); err != nil {
		return zero, err
	}
	var currentRole string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&currentRole); err != nil {
		return zero, err
	}
	if currentRole != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	if role == "consents" || role == "calibration" {
		function := "photo.v23_identity_consent_result_input"
		if role == "calibration" {
			function = "photo.v23_identity_calibration_result_input"
		}
		err = tx.QueryRow(bounded, "SELECT "+function+"($1,$2,$3,$4,$5,$6,$7,$8)",
			scope.Tenant, scope.Task, scope.Result, scope.Root.ArtifactID, scope.Root.SHA256,
			ref.ArtifactID, ref.SHA256, c.Bucket).Scan(&raw)
	} else {
		err = tx.QueryRow(bounded, `SELECT photo.v23_identity_result_input($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			scope.Tenant, scope.Task, scope.Result, scope.Root.ArtifactID, scope.Root.SHA256,
			role, ref.ArtifactID, ref.SHA256, c.Bucket).Scan(&raw)
	}
	if err != nil {
		return zero, err
	}
	object, err := parseRestorationLocator(raw)
	if err != nil {
		return zero, err
	}
	array := identityArrayRole(role)
	maxBytes := int64(1048576)
	if role == "consents" {
		maxBytes = 8388608
	}
	media := "application/json"
	minBytes := int64(2)
	if array {
		media, minBytes, maxBytes = "application/x-npy", 14, 800004096
		if role == "before_pixels" || role == "after_pixels" {
			maxBytes = 2400065536
		}
	}
	if object.Key == "" || len(object.Key) > 1024 || object.VersionID == "" ||
		object.VersionID == "null" || len(object.VersionID) > 1024 ||
		object.SHA256 != ref.SHA256 || object.ContentType != media ||
		object.Bytes < minBytes || object.Bytes > maxBytes {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return object, nil
}

func identityInputRole(role string) bool {
	switch role {
	case "params", "before", "after", "effective", "protected", "consents", "calibration",
		"before_pixels", "after_pixels", "before_alpha", "after_alpha",
		"effective_pixels", "protected_pixels":
		return true
	}
	return false
}

func identityArrayRole(role string) bool {
	switch role {
	case "before_pixels", "after_pixels", "before_alpha", "after_alpha",
		"effective_pixels", "protected_pixels":
		return true
	}
	return false
}
