package main

import (
	"bytes"
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
)

// The pinned document configures concrete native probes and measured budgets.
// Semantic/model/consent authority remains an explicit process dependency.
func prepareAPIInterchangeExport(ctx context.Context, path, pin string, dependencies apiProcessDependencies) (interchangeExportFactory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	authority := dependencies.InterchangeExportAuthority
	origins := dependencies.InterchangePersonOrigins
	process := dependencies.InterchangePersonProcess
	if dependencies.InterchangePersonOriginKey != nil || dependencies.InterchangePersonIssuerID != uuid.Nil ||
		(process != nil && origins != nil) {
		return nil, errors.New("interchange person-origin signer is not installed from pinned operator policy")
	}
	trusted := make(map[interchange.PortablePersonOriginKey]ed25519.PublicKey,
		len(dependencies.InterchangePersonOriginTrust))
	for key, public := range dependencies.InterchangePersonOriginTrust {
		trusted[key] = append(ed25519.PublicKey(nil), public...)
	}
	if (origins == nil && process == nil) != (len(trusted) == 0) {
		return nil, errors.New("interchange person-origin issuer and trust must be installed together")
	}
	if process != nil {
		derived := process.Trust()
		if len(derived) != len(trusted) {
			return nil, errors.New("interchange person-origin process trust mismatch")
		}
		for key, public := range derived {
			if !bytes.Equal(trusted[key], public) {
				return nil, errors.New("interchange person-origin process trust mismatch")
			}
		}
	}
	for key, public := range trusted {
		digest := sha256.Sum256(public)
		if len(public) != ed25519.PublicKeySize || key.IssuerID == uuid.Nil ||
			hex.EncodeToString(digest[:]) != key.KeyID {
			return nil, errors.New("interchange person-origin trust invalid")
		}
	}
	if path == "" && pin == "" {
		if authority != nil || origins != nil || process != nil {
			return nil, errors.New("interchange export authority requires pinned deployment")
		}
		return dependencies.InterchangeExport, nil
	}
	hash, err := hex.DecodeString(pin)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || err != nil || len(hash) != 32 || hex.EncodeToString(hash) != pin {
		return nil, errors.New("interchange export requires absolute deployment file and lowercase SHA256")
	}
	if authority == nil || (origins == nil && process == nil) || len(trusted) == 0 || dependencies.InterchangeExport != nil {
		return nil, errors.New("interchange export requires native authority and person-origin issuer/trust")
	}
	deployment, err := interchange.LoadNativeExportDeployment(ctx, path, pin)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("interchange export deployment unavailable")
	}
	return func(ctx context.Context, api, scheduler *pgxpool.Pool, objects *objectstore.Store, bucket string) (interchange.ExportCreatePreparer, func(), error) {
		candidates := origins
		if process != nil {
			installed, err := process.CandidateFactory(ctx, objects)
			if err != nil {
				return nil, nil, err
			}
			candidates = installed
		}
		prepared, err := deployment.BuildWithAuthority(ctx, interchange.SQLExportExecutionInstaller{
			API: api, Scheduler: scheduler, Objects: objects, Bucket: bucket,
			PersonOriginCandidates: candidates, PersonOriginTrust: trusted,
		}, authority)
		if err != nil {
			return nil, nil, err
		}
		// Scratch operations are scoped and cleaned per request; these probes own
		// no long-lived process or open file descriptor.
		return prepared, func() {}, nil
	}, nil
}
