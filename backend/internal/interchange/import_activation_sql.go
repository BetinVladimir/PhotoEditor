package interchange

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Accepts only in-process physical/resource qualification evidence. SQL repeats
// live scope, transport, quota and dependency gates under its own row locks.
// A refusal is rolled back; an ambiguous commit error is never reported success.
type SQLImportActivator struct {
	DB     *pgxpool.Pool
	Tenant uuid.UUID
	Parsed []byte
}

func (s SQLImportActivator) ActivateImport(ctx context.Context, qualified QualifiedImportActivation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || s.Tenant == uuid.Nil || qualified.tenant != s.Tenant || qualified.documents.evidence == nil || qualified.digest != qualified.task.InputDigest {
		return v2input.ErrInvalid
	}
	if err := validateImportTask(qualified.documents, qualified.task); err != nil {
		return err
	}
	parsed := append([]byte(nil), s.Parsed...)
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return err
	}
	var params importParamsDocument
	if err := decodeImportDocument(qualified.documents.params.Body, &params); err != nil {
		return err
	}
	if params.Parsed.SHA256 != rawSHA(parsed) || params.Plan != qualified.documents.plan.Ref || params.Evidence == nil || *params.Evidence != qualified.documents.evidence.Ref {
		return v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(qualified.task)
	if err != nil {
		return err
	}
	reviews, err := canonical.Marshal(append([]ImportReviewWitness{}, qualified.reviews...))
	if err != nil {
		return err
	}
	err = database.TenantTransaction(ctx, s.DB, s.Tenant, func(tx pgx.Tx) error {
		var run uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT run_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`, s.Tenant, qualified.documents.identity.Workflow).Scan(&run); err != nil {
			return err
		}
		if run != qualified.documents.identity.Run {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.activate_v30_interchange_import_review_fenced($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb)`, s.Tenant, qualified.documents.identity.Workflow, string(parsed), string(qualified.documents.plan.Body), string(qualified.documents.params.Body), string(raw), string(reviews)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
	return classifyImportReviewActivationError(err)
}

// A grant revoked between the last codec read and activation is a normal
// editor-controlled wait. Other SQL errors still require operator attention.
func classifyImportReviewActivationError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "P0001" && pgErr.Message == "V30_IMPORT_REVIEW_ACTIVATION_GRANT" {
		return ErrImportReviewAwaitingDecision
	}
	return err
}

// Reads historical admission only; it does not publish objects, requalify a
// running task or change task state. Current source authority remains required.
func (s SQLImportActivator) RecoverImportActivation(ctx context.Context, tenant uuid.UUID, documents ImportPreparedDocuments, task v2input.DraftTask, parsedRaw []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || tenant == uuid.Nil || tenant != s.Tenant || documents.evidence == nil {
		return false, v2input.ErrInvalid
	}
	documents = cloneImportPreparedDocuments(documents)
	if err := validateImportTask(documents, task); err != nil {
		return false, err
	}
	parsed := append([]byte(nil), parsedRaw...)
	if rawSHA(parsed) != rawSHA(s.Parsed) {
		return false, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return false, err
	}
	var params importParamsDocument
	if err := decodeImportDocument(documents.params.Body, &params); err != nil {
		return false, err
	}
	if params.Parsed.SHA256 != rawSHA(parsed) || params.Plan != documents.plan.Ref || params.Evidence == nil || *params.Evidence != documents.evidence.Ref {
		return false, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(task)
	if err != nil {
		return false, err
	}
	recovered := false
	err = database.TenantTransaction(ctx, s.DB, s.Tenant, func(tx pgx.Tx) error {
		var run uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT run_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`, s.Tenant, documents.identity.Workflow).Scan(&run); err != nil {
			return err
		}
		if run != documents.identity.Run {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT photo.read_v30_interchange_import_activation($1,$2,$3,$4,$5,$6::jsonb)`, s.Tenant, documents.identity.Workflow, string(parsed), string(documents.plan.Body), string(documents.params.Body), string(raw)).Scan(&recovered)
	})
	if err != nil {
		return false, err
	}
	return recovered, nil
}
