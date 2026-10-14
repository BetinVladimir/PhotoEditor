package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/interchange"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/workflow"
)

type pinnedSchedulerInterchangeExport struct {
	deployment interchange.NativeExportDeployment
	authority  interchange.ExportNativeAuthorityFactory
	trusted    map[interchange.PortablePersonOriginKey]ed25519.PublicKey
}

func (f pinnedSchedulerInterchangeExport) BuildSchedulerInterchangeExport(ctx context.Context,
	db *pgxpool.Pool, objects *objectstore.Store, bucket string) (workflow.ExpansionJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	processor, err := f.deployment.BuildSchedulerExportAdoptionFromDeployment(
		ctx, db, objects, bucket, f.authority, f.trusted)
	if err != nil {
		return nil, err
	}
	return processor, ctx.Err()
}

func prepareSchedulerInterchangeExport(ctx context.Context, path, pin string,
	dependencies apiProcessDependencies) (workflow.SchedulerInterchangeExportFactory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" && pin == "" {
		if dependencies.InterchangeExportAuthority != nil ||
			dependencies.InterchangePersonOrigins != nil ||
			len(dependencies.InterchangePersonOriginTrust) != 0 {
			return nil, errors.New("scheduler export authority requires pinned deployment")
		}
		return nil, nil
	}
	hash, err := hex.DecodeString(pin)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || err != nil ||
		len(hash) != sha256.Size || hex.EncodeToString(hash) != pin ||
		dependencies.InterchangeExportAuthority == nil ||
		dependencies.InterchangePersonOrigins == nil ||
		len(dependencies.InterchangePersonOriginTrust) == 0 {
		return nil, errors.New("scheduler export requires pinned deployment, native authority and person-origin trust")
	}
	trusted := make(map[interchange.PortablePersonOriginKey]ed25519.PublicKey,
		len(dependencies.InterchangePersonOriginTrust))
	for key, public := range dependencies.InterchangePersonOriginTrust {
		digest := sha256.Sum256(public)
		if len(public) != ed25519.PublicKeySize ||
			key.IssuerID == uuid.Nil || hex.EncodeToString(digest[:]) != key.KeyID {
			return nil, errors.New("scheduler export person-origin trust invalid")
		}
		trusted[key] = append(ed25519.PublicKey(nil), public...)
	}
	deployment, err := interchange.LoadNativeExportDeployment(ctx, path, pin)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("scheduler export deployment unavailable")
	}
	return pinnedSchedulerInterchangeExport{
		deployment: deployment,
		authority:  dependencies.InterchangeExportAuthority,
		trusted:    trusted,
	}, nil
}

var _ workflow.SchedulerInterchangeExportFactory = pinnedSchedulerInterchangeExport{}
