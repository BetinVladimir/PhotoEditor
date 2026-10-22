package run

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

// AuthoredParentLink is immutable accepted-request linkage, not completion,
// current phase approval, display-media authority, or a derivable UUID recipe.
type AuthoredParentLink struct {
	SchemaVersion int       `json:"schema_version"`
	Tenant        uuid.UUID `json:"tenant_id"`
	Project       uuid.UUID `json:"project_id"`
	Parent        uuid.UUID `json:"parent_run_id"`
	RequestSHA    string    `json:"request_sha256"`
	Admission     string    `json:"admission"`
}

func decodeAuthoredParentLink(raw []byte, tenantID, projectID, parent uuid.UUID, requestSHA string) (AuthoredParentLink, error) {
	invalid := func() (AuthoredParentLink, error) {
		return AuthoredParentLink{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || projectID == uuid.Nil || parent == uuid.Nil || !isLowerHexSHA(requestSHA) {
		return invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["parent_run_id"] == nil || fields["request_sha"] == nil {
		return invalid()
	}
	var receipt struct {
		Parent uuid.UUID `json:"parent_run_id"`
		SHA    string    `json:"request_sha"`
	}
	if decodeAuthoredJSON(raw, &receipt) != nil || receipt.Parent != parent || !isLowerHexSHA(receipt.SHA) {
		return invalid()
	}
	if receipt.SHA != requestSHA {
		return AuthoredParentLink{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	}
	return AuthoredParentLink{SchemaVersion: 1, Tenant: tenantID, Project: projectID, Parent: parent, RequestSHA: requestSHA, Admission: "accepted"}, nil
}

// ReadAuthoredParent re-enters persisted receipt authority for the real tenant
// and project on every read. No ordinary RenderBatch satisfies this function.
func (s EditorEffectsService) ReadAuthoredParent(ctx context.Context, who auth.Identity, tenantID, projectID, parent uuid.UUID, requestSHA string) (AuthoredParentLink, error) {
	if err := ctx.Err(); err != nil {
		return AuthoredParentLink{}, err
	}
	if tenantID == uuid.Nil || projectID == uuid.Nil || parent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !isLowerHexSHA(requestSHA) {
		return AuthoredParentLink{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if s.Runs.DB == nil {
		return AuthoredParentLink{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var out AuthoredParentLink
	err := tenant.WithTenant(ctx, s.Runs.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, err := project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT photo.authored_render_parent_accepted($1,$2,$3,$4,$5)`, tenantID, parent, projectID, who.UserID, who.SessionID).Scan(&raw); err != nil {
			return err
		}
		if len(raw) == 0 || string(raw) == "null" {
			return domain.NewError("AUTHORED_RENDER_NOT_FOUND", 404)
		}
		var err error
		out, err = decodeAuthoredParentLink(raw, tenantID, projectID, parent, requestSHA)
		if err != nil {
			return err
		}
		// Query waits may outlive concurrent membership/session revocation.
		currentRole, err := tenant.AuthorizeInTransaction(ctx, tx, who, tenantID, domain.Editor)
		if err != nil {
			return err
		}
		_, err = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, currentRole, domain.Editor, false)
		return err
	})
	if err != nil {
		return AuthoredParentLink{}, err
	}
	return out, ctx.Err()
}
