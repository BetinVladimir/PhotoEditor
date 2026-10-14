package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

type schedulerExportFactoryStub struct {
	job ExpansionJob
	err error
}

type schedulerExportJobStub struct{}

func (schedulerExportJobStub) ProcessOne(context.Context) (bool, error) { return false, nil }

func (s schedulerExportFactoryStub) BuildSchedulerInterchangeExport(context.Context,
	*pgxpool.Pool, *objectstore.Store, string) (ExpansionJob, error) {
	return s.job, s.err
}

func TestSchedulerInterchangeExportRequiresCompleteInstallation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := installSchedulerInterchangeExport(ctx, nil, nil, nil, "photos"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled installation proceeded", err)
	}
	if _, err := installSchedulerInterchangeExport(context.Background(), nil, nil, nil, "photos"); err == nil {
		t.Fatal("missing factory accepted")
	}
	if _, err := installSchedulerInterchangeExport(context.Background(), schedulerExportFactoryStub{},
		&pgxpool.Pool{}, &objectstore.Store{}, "photos"); err == nil {
		t.Fatal("factory without job accepted")
	}
	sentinel := errors.New("native authority missing")
	if _, err := installSchedulerInterchangeExport(context.Background(), schedulerExportFactoryStub{err: sentinel},
		&pgxpool.Pool{}, &objectstore.Store{}, "photos"); !errors.Is(err, sentinel) {
		t.Fatal("factory refusal lost", err)
	}
	job := schedulerExportJobStub{}
	installed, err := installSchedulerInterchangeExport(context.Background(), schedulerExportFactoryStub{job: job},
		&pgxpool.Pool{}, &objectstore.Store{}, "photos")
	if err != nil || installed == nil {
		t.Fatal("complete factory refused", err)
	}
}
