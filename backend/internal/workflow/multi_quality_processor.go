package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"time"
)

type MultiQualityProducer struct {
	Tenant, Task uuid.UUID
	Operation    string
}
type MultiQualityProducerSource interface {
	Next(context.Context, uuid.UUID) (*MultiQualityProducer, error)
}
type MultiQualityPreparer interface {
	Prepare(context.Context, uuid.UUID, uuid.UUID, string) (MultiQualityReceipt, error)
}
type SQLMultiQualityProducers struct{ DB *pgxpool.Pool }

func (s SQLMultiQualityProducers) Next(ctx context.Context, after uuid.UUID) (*MultiQualityProducer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, objectstore.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return nil, err
	}
	if role != "photo_scheduler" {
		return nil, objectstore.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id','',true)"); err != nil {
		return nil, err
	}
	var cursor any
	if after != uuid.Nil {
		cursor = after
	}
	var value MultiQualityProducer
	err = tx.QueryRow(ctx, "SELECT * FROM photo.next_v29_multi_quality_producer($1)", cursor).Scan(&value.Tenant, &value.Task, &value.Operation)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if value.Tenant == uuid.Nil || value.Task == uuid.Nil || (value.Operation != "optical.panorama" && value.Operation != "optical.focus_stack") || (after != uuid.Nil && value.Task.String() <= after.String()) {
		return nil, objectstore.ErrIntegrity
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &value, nil
}

// Discovery commits before Prepare streams any objects. Advancing the cursor
// before IO keeps one revoked source or failed publication from starving others.
type MultiQualityProcessor struct {
	Source   MultiQualityProducerSource
	Preparer MultiQualityPreparer
	after    uuid.UUID
}

func (p *MultiQualityProcessor) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p == nil || p.Source == nil || p.Preparer == nil {
		return false, objectstore.ErrInvalid
	}
	value, err := p.Source.Next(ctx, p.after)
	if err != nil {
		return false, err
	}
	if value == nil {
		p.after = uuid.Nil
		return false, nil
	}
	if value.Tenant == uuid.Nil || value.Task == uuid.Nil || (value.Operation != "optical.panorama" && value.Operation != "optical.focus_stack") || (p.after != uuid.Nil && value.Task.String() <= p.after.String()) {
		return false, objectstore.ErrIntegrity
	}
	p.after = value.Task
	work, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	receipt, err := p.Preparer.Prepare(work, value.Tenant, value.Task, value.Operation)
	if err != nil {
		return false, err
	}
	if receipt.Run == uuid.Nil || receipt.SourceResult == uuid.Nil || receipt.Quality != uuid.NewSHA1(receipt.Run, []byte("quality.check:master")) {
		return false, objectstore.ErrIntegrity
	}
	switch receipt.State {
	case "running", "completed", "completed_with_errors", "failed", "cancelled":
		return true, nil
	default:
		return false, objectstore.ErrIntegrity
	}
}
