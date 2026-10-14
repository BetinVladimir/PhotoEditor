package v2input

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type identityConsentPinQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// SQLIdentityCompareConsentPinner must be called after registering the exact
// task input and before seal_v2_graph. SQL serializes with consent revocation
// and creates the workflow links used by cancellation and seal/adoption gates.
type SQLIdentityCompareConsentPinner struct{ DB identityConsentPinQuerier }

func (s SQLIdentityCompareConsentPinner) Pin(ctx context.Context, tenant, workflow uuid.UUID,
	ordinal int, consents Ref, body []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	task, err := TaskID(workflow, "identity.compare", ordinal)
	if err != nil || tenant == uuid.Nil || s.DB == nil ||
		!identityPinnedDocument(consents, body) || ValidateSessionConsentSnapshot(body) != nil {
		return ErrInvalid
	}
	var pinned bool
	err = s.DB.QueryRow(ctx, `SELECT photo.pin_v23_identity_compare_consents($1,$2,$3,$4)`,
		tenant, workflow, task, string(body)).Scan(&pinned)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !pinned {
		return ErrConflict
	}
	return nil
}
