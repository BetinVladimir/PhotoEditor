package interchange

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// ExportResultPreparationInstaller is process authority for native binary and
// typed JSON qualification. It must not derive trust from a request or ZIP.
type ExportResultPreparationInstaller interface {
	InstallExportResultPreparation(context.Context, SQLExportOutputCatalog, v2input.Ref) (ExportResultPreparation, error)
}

type ExportResultAdoptionService struct {
	DB        *pgxpool.Pool
	Bucket    string
	Installer ExportResultPreparationInstaller
}

// AdoptCurrentExportResult reads the frozen server ledger and canonical result
// first. Callers supply the original request and idempotency key for identity
// matching only; they cannot select the result root or qualification ports.
func (s ExportResultAdoptionService) AdoptCurrentExportResult(ctx context.Context,
	who auth.Identity, tenantID uuid.UUID, request CreateRequest, key string,
) (PreparedExportAdoption, error) {
	var zero PreparedExportAdoption
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Installer == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	intent, err := FreezeCreateIntent(tenantID, who, request, key)
	if err != nil {
		return zero, err
	}
	if intent.Request.Direction != "export" {
		return zero, v2input.ErrInvalid
	}
	candidate, err := RecoverExportAdoptionCandidate(ctx, s.DB, who, tenantID, intent.Request, key)
	if err != nil {
		return zero, err
	}
	config := SQLExportOutputCatalog{DB: s.DB, Tenant: tenantID, Who: who,
		Request: intent.Request, Key: key, Bucket: s.Bucket, Frozen: candidate.Frozen}
	preparation, err := s.Installer.InstallExportResultPreparation(ctx, config, candidate.Root)
	if err != nil {
		return zero, err
	}
	// Never let an installer swap the tenant, request, pool or frozen evidence.
	preparation.Catalog = config
	return preparation.PrepareAndAdopt(ctx, candidate.Root)
}
