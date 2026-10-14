package main

import (
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/interchange"
	"photoeditor/backend/internal/objectstore"
)

// The pinned document configures concrete native probes and measured budgets.
// Semantic/model/consent authority remains an explicit process dependency.
func prepareAPIInterchangeExport(ctx context.Context, path, pin string, dependencies apiProcessDependencies) (interchangeExportFactory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	authority := dependencies.InterchangeExportAuthority
	if path == "" && pin == "" {
		if authority != nil {
			return nil, errors.New("interchange export authority requires pinned deployment")
		}
		return dependencies.InterchangeExport, nil
	}
	hash, err := hex.DecodeString(pin)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || err != nil || len(hash) != 32 || hex.EncodeToString(hash) != pin {
		return nil, errors.New("interchange export requires absolute deployment file and lowercase SHA256")
	}
	if authority == nil || dependencies.InterchangeExport != nil {
		return nil, errors.New("interchange export requires one installed native authority")
	}
	deployment, err := interchange.LoadNativeExportDeployment(ctx, path, pin)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("interchange export deployment unavailable")
	}
	return func(ctx context.Context, api, scheduler *pgxpool.Pool, objects *objectstore.Store, bucket string) (interchange.ExportCreatePreparer, func(), error) {
		prepared, err := deployment.BuildWithAuthority(ctx, interchange.SQLExportExecutionInstaller{API: api, Scheduler: scheduler, Objects: objects, Bucket: bucket}, authority)
		if err != nil {
			return nil, nil, err
		}
		// Scratch operations are scoped and cleaned per request; these probes own
		// no long-lived process or open file descriptor.
		return prepared, func() {}, nil
	}, nil
}
