package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/workflow"
)

type schedulerImportFactoryStub struct{}

func (schedulerImportFactoryStub) BuildSchedulerInterchangeImport(context.Context, *pgxpool.Pool, *objectstore.Store, string) (workflow.InterchangeImportContinuation, error) {
	return nil, nil
}

func TestSchedulerImportFactorySelection(t *testing.T) {
	if got := schedulerImportFactories(apiProcessDependencies{}); len(got) != 0 {
		t.Fatal("absent installation became a present nil factory")
	}
	installed := schedulerImportFactories(apiProcessDependencies{InterchangeImport: schedulerImportFactoryStub{}})
	if len(installed) != 1 || installed[0] == nil {
		t.Fatal("trusted import factory was not passed to scheduler")
	}
}
