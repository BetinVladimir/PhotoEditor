package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

type RestorationPreviewProducer struct{ Tenant, Task uuid.UUID }

type RestorationPreviewProducerSource interface {
	Next(context.Context, uuid.UUID, string) (*RestorationPreviewProducer, error)
}

type RestorationPreviewReceipt struct {
	Run, SourceResult                            uuid.UUID
	Before1600, Before2560, After1600, After2560 uuid.UUID
	State                                        string
}

type RestorationPreviewPreparer interface {
	Prepare(context.Context, uuid.UUID, uuid.UUID) (RestorationPreviewReceipt, error)
}

type SQLRestorationPreviewProducers struct{ DB *pgxpool.Pool }

func (s SQLRestorationPreviewProducers) Next(ctx context.Context, after uuid.UUID,
	srgb string) (*RestorationPreviewProducer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || domainDigestInvalid(srgb) {
		return nil, objectstore.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return nil, err
	}
	if role != "photo_scheduler" {
		return nil, objectstore.ErrInvalid
	}
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id','',true)"); err != nil {
		return nil, err
	}
	var cursor any
	if after != uuid.Nil {
		cursor = after
	}
	var value RestorationPreviewProducer
	err = tx.QueryRow(bounded,
		"SELECT * FROM photo.next_v28_restoration_preview_producer($1,$2)",
		cursor, srgb).Scan(&value.Tenant, &value.Task)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(bounded); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if value.Tenant == uuid.Nil || value.Task == uuid.Nil ||
		(after != uuid.Nil && value.Task.String() <= after.String()) {
		return nil, objectstore.ErrIntegrity
	}
	if err = tx.Commit(bounded); err != nil {
		return nil, err
	}
	return &value, nil
}

// The cursor advances before object IO; failed candidates are retried after
// wrap without starving later review candidates.
type RestorationPreviewProcessor struct {
	Source   RestorationPreviewProducerSource
	Preparer RestorationPreviewPreparer
	SRGBSHA  string
	after    uuid.UUID
}

func (p *RestorationPreviewProcessor) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p == nil || p.Source == nil || p.Preparer == nil || domainDigestInvalid(p.SRGBSHA) {
		return false, objectstore.ErrInvalid
	}
	value, err := p.Source.Next(ctx, p.after, p.SRGBSHA)
	if err != nil {
		return false, err
	}
	if value == nil {
		p.after = uuid.Nil
		return false, nil
	}
	if value.Tenant == uuid.Nil || value.Task == uuid.Nil ||
		(p.after != uuid.Nil && value.Task.String() <= p.after.String()) {
		return false, objectstore.ErrIntegrity
	}
	p.after = value.Task
	work, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	receipt, err := p.Preparer.Prepare(work, value.Tenant, value.Task)
	if err != nil {
		return false, err
	}
	if receipt.Run == uuid.Nil || receipt.SourceResult == uuid.Nil ||
		receipt.Before1600 != restorationPreviewTaskID(receipt.Run, "before", "preview1600") ||
		receipt.Before2560 != restorationPreviewTaskID(receipt.Run, "before", "preview2560") ||
		receipt.After1600 != restorationPreviewTaskID(receipt.Run, "after", "preview1600") ||
		receipt.After2560 != restorationPreviewTaskID(receipt.Run, "after", "preview2560") {
		return false, objectstore.ErrIntegrity
	}
	switch receipt.State {
	case "running", "completed", "completed_with_errors", "failed", "cancelled":
		return true, nil
	default:
		return false, objectstore.ErrIntegrity
	}
}
