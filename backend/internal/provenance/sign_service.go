package provenance

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Explicit operator-qualified composition; API and scheduler pools have distinct
// roles. Object publication/native qualification never run inside SQL TXs.
type SQLSignExportService struct {
	API, Scheduler *pgxpool.Pool
	Preparation    SigningActivationPreparation
	Catalog        SigningExportCatalog
	Objects        artifactjson.Objects
	Documents      v2input.DocumentPublisher
	Bucket         string
	Clock          func() time.Time
}

func ensureSigningDraft(ctx context.Context, tx pgx.Tx, who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, bucket string) error {
	if _, err := PinSigningPending(ctx, tx, who, ready, prepared, bucket); err != nil {
		return err
	}
	i := prepared.intent
	params, body := prepared.compiled.ParamsDocument()
	var present bool
	if err := tx.QueryRow(ctx, `SELECT photo.v31_signing_draft_present($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, i.tenant, who.UserID, who.SessionID, i.key, i.requestSHA, i.task, params.ArtifactID, params.SHA256, int64(len(body)), bucket).Scan(&present); err != nil {
		return err
	}
	if present {
		return nil
	}
	return PrepareSigningDraft(ctx, tx, who, ready, prepared, bucket)
}

// SignExport returns a creation acknowledgement only after scheduler commit.
// Exact retries restore frozen pending state or recover the committed ticket.
func (s SQLSignExportService) SignExport(ctx context.Context, who auth.Identity, tid, export, policy uuid.UUID, key string) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.API == nil || s.Scheduler == nil || s.API == s.Scheduler || s.Catalog == nil || s.Objects == nil || s.Documents == nil || s.Clock == nil || s.Preparation.Registry == nil || !s.Preparation.Registry.matchesTenant(tid) || s.Preparation.Source == nil || s.Preparation.Resources == nil || s.Preparation.Clock == nil {
		return zero, ErrInvalid
	}
	if _, ok := s.Objects.(signingPolicyVersionReader); !ok {
		return zero, ErrInvalid
	}
	tickets := SQLSigningTicketReader{DB: s.API}
	if ticket, found, err := tickets.Read(ctx, who, tid, export, policy, key); err != nil {
		return zero, err
	} else if found {
		return ticket, nil
	}
	// Invocation-local projection prevents sharing artifact UUIDs across tenants.
	registry, err := s.Preparation.Registry.ForTenant(tid)
	if err != nil {
		return zero, err
	}
	s.Preparation.Registry = registry
	request, err := canonical.Marshal(struct {
		Export uuid.UUID `json:"export_id"`
		Policy uuid.UUID `json:"signing_policy_id"`
	}{export, policy})
	if err != nil {
		return zero, err
	}
	pending := SQLSigningPendingReader{DB: s.API, Registry: s.Preparation.Registry, Clock: s.Clock}
	intent, savedTask, _, err := pending.Read(ctx, who, tid, export, key, bodyDigest(request))
	restored := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		intent, err = PrepareSigningIntent(ctx, who, tid, export, policy, key, s.Preparation.Source, s.Preparation.Registry, s.Clock())
	}
	if err != nil {
		return zero, err
	}
	if !restored {
		evidence, e := s.Preparation.Source.ResolveSigningExport(ctx, who, tid, export)
		if e != nil {
			return zero, e
		}
		if !signingEvidenceMatches(intent, evidence) {
			return zero, ErrInvalid
		}
		evidence.LineageBody = append([]byte(nil), evidence.LineageBody...)
		resources, e := s.Preparation.Resources.QualifySigningResources(ctx, who, cloneSigningIntent(intent), evidence)
		if e != nil {
			return zero, e
		}
		savedTask, e = CompileSigningTask(ctx, intent, s.Preparation.Registry, evidence.MIME, resources, s.Clock())
		if e != nil {
			return zero, e
		}
	}
	prepared, err := s.Preparation.Prepare(ctx, who, intent, savedTask)
	if err != nil {
		return zero, err
	}
	if restored {
		expected, e := canonical.Marshal(savedTask.Task())
		if e != nil {
			return zero, e
		}
		current, e := canonical.Marshal(prepared.compiled.Task())
		if e != nil {
			return zero, e
		}
		if !bytes.Equal(expected, current) {
			return zero, artifactjson.ErrConflict
		}
	}
	ready, err := s.Catalog.LookupSigningExport(ctx, who, tid, export)
	if err != nil {
		return zero, err
	}
	params, err := DecodeSignParams(intent.params)
	if err != nil {
		return zero, err
	}
	policyPublisher := ImmutableSigningPolicyPublisher{Catalog: s.Catalog, Registry: registry, Store: SQLSigningPolicyStore{DB: s.API, Bucket: s.Bucket}, Objects: s.Objects, Clock: s.Clock}
	if _, err = policyPublisher.PublishSigningPolicy(ctx, who, ready, params); err != nil {
		return zero, err
	}
	err = tenant.WithTenant(ctx, s.API, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return ensureSigningDraft(ctx, tx, who, ready, prepared, s.Bucket)
	})
	if err != nil {
		return zero, err
	}
	repo := SQLSigningParamsRepository{DB: s.API, Who: who, Ready: ready, Prepared: prepared, Bucket: s.Bucket}
	if _, err = repo.Publish(ctx, s.Objects); err != nil {
		return zero, err
	}
	if err = (SQLSigningTaskMaterializer{DB: s.API, Who: who, Ready: ready, Prepared: prepared, Bucket: s.Bucket}).Materialize(ctx); err != nil {
		return zero, err
	}
	transport, err := PublishSigningInputs(ctx, who, ready, prepared, s.Bucket, s.Documents)
	if err != nil {
		return zero, err
	}
	// Repeat physical/operator/resource authority after potentially slow IO.
	fresh, err := s.Preparation.Prepare(ctx, who, intent, prepared.compiled)
	if err != nil {
		return zero, err
	}
	previous, err := canonical.Marshal(prepared.compiled.Task())
	if err != nil {
		return zero, err
	}
	current, err := canonical.Marshal(fresh.compiled.Task())
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(previous, current) {
		return zero, artifactjson.ErrConflict
	}
	var ticket v2input.JobTicket
	err = database.InTransaction(ctx, s.Scheduler, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tid.String()); e != nil {
			return e
		}
		var e error
		ticket, e = ActivateSigningInTransaction(ctx, tx, who, ready, fresh, transport, s.Bucket)
		return e
	})
	if err != nil {
		// An ambiguous commit may already have produced the exact acknowledgement.
		if recovered, found, e := tickets.Read(ctx, who, tid, export, policy, key); e == nil && found {
			return recovered, nil
		}
		return zero, err
	}
	return ticket, nil
}
