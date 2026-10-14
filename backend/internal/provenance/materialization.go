package provenance

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
)

// MaterializeSigningTask saves one blocked task and its output reservation.
// Prepared qualification must be fresh; SQL checks saved identity/documents and
// current authority. No network/native I/O occurs in this transaction.
func MaterializeSigningTask(ctx context.Context, tx SigningAdmissionTransaction, who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return ErrInvalid
	}
	if _, err := compileSigningPending(who, ready, prepared, bucket); err != nil {
		return err
	}
	raw, err := canonical.Marshal(prepared.compiled.Task())
	if err != nil {
		return err
	}
	i := prepared.intent
	var accepted bool
	if err := tx.QueryRow(ctx, `SELECT photo.materialize_v31_signing_task($1,$2,$3,$4,$5,$6::jsonb)`, i.tenant, who.UserID, who.SessionID, i.key, i.requestSHA, string(raw)).Scan(&accepted); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !accepted {
		return artifactjson.ErrStale
	}
	return nil
}

type SQLSigningTaskMaterializer struct {
	DB       *pgxpool.Pool
	Who      auth.Identity
	Ready    ReadySigningExport
	Prepared PreparedSigningActivation
	Bucket   string
}

func (s SQLSigningTaskMaterializer) Materialize(ctx context.Context) error {
	if s.DB == nil {
		return ErrInvalid
	}
	return tenant.WithTenant(ctx, s.DB, s.Who, s.Ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return MaterializeSigningTask(ctx, tx, s.Who, s.Ready, s.Prepared, s.Bucket)
	})
}
