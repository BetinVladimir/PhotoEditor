package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"time"
)

type InterchangeImportCandidate struct {
	Tenant, Workflow, Run, Project, Artifact uuid.UUID
	Snapshot, SHA                            string
	Bytes                                    int64
	Bucket, Key, Version                     string
}

// A current editor decision has not yet been granted. The durable claim is
// retried after its SQL cooldown; this is not a failed import attempt.
var ErrInterchangeImportAwaitingDecision = errors.New("interchange import awaiting current review decision")

type InterchangeImportStore interface {
	NextImport(context.Context) (*InterchangeImportCandidate, error)
}

// Implemented by the interchange installation, keeping workflow independent
// from the package that already depends on workflow for canonical ingest.
type InterchangeImportContinuation interface {
	ContinueInterchangeImport(context.Context, InterchangeImportCandidate, []byte) error
}
type InterchangeImportProcessor struct {
	Store        InterchangeImportStore
	Objects      interchangeCollectionObjects
	Continuation InterchangeImportContinuation
	Bucket       string
	Timeout      time.Duration
}

func (p InterchangeImportProcessor) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.Store == nil || p.Objects == nil || p.Continuation == nil || len(p.Bucket) < 3 || len(p.Bucket) > 63 || p.Timeout < time.Second || p.Timeout > 30*time.Minute {
		return false, errors.New("invalid interchange import processor")
	}
	work, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	candidate, err := p.Store.NextImport(work)
	if err != nil || candidate == nil {
		return false, err
	}
	c := *candidate
	if err := work.Err(); err != nil {
		return false, err
	}
	validSHA := func(value string) bool {
		if len(value) != 64 {
			return false
		}
		for _, c := range value {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
	if c.Tenant == uuid.Nil || c.Workflow == uuid.Nil || c.Run == uuid.Nil || c.Project == uuid.Nil || c.Artifact == uuid.Nil || !validSHA(c.SHA) || !validSHA(c.Snapshot) || c.Bucket != p.Bucket || c.Bytes < 2 || c.Bytes > 8<<20 || c.Key == "" || c.Version == "" || c.Version == "null" {
		return false, errors.New("invalid interchange import candidate")
	}
	reader, err := p.Objects.Open(work, objectstore.Object{Key: c.Key, VersionID: c.Version, SHA256: c.SHA, Bytes: c.Bytes, ContentType: "application/json"})
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return false, err
	}
	if reader == nil {
		return false, errors.New("missing interchange import reader")
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, c.Bytes+1))
	closeErr := reader.Close()
	if err := work.Err(); err != nil {
		return false, err
	}
	if readErr != nil {
		return false, readErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != c.Bytes || hex.EncodeToString(sum[:]) != c.SHA {
		return false, errors.New("interchange import hash mismatch")
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, normalized) {
		return false, errors.New("noncanonical interchange import")
	}
	if err := p.Continuation.ContinueInterchangeImport(work, c, append([]byte(nil), raw...)); err != nil {
		if errors.Is(err, ErrInterchangeImportAwaitingDecision) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

// NextImport commits a durable scheduling claim before any object IO. A
// refused or crashed attempt becomes discoverable after the SQL cooldown.
// The claim does not replace any continuation authority checks.
type SQLInterchangeImportStore struct{ DB *pgxpool.Pool }

func (s SQLInterchangeImportStore) NextImport(ctx context.Context) (*InterchangeImportCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, errors.New("missing import scheduler database")
	}
	var c InterchangeImportCandidate
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,workflow_id,run_id,project_id,snapshot_digest,artifact_id,sha256,bytes,bucket,object_key,object_version FROM photo.v30_claim_next_interchange_import()`).Scan(&c.Tenant, &c.Workflow, &c.Run, &c.Project, &c.Snapshot, &c.Artifact, &c.SHA, &c.Bytes, &c.Bucket, &c.Key, &c.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
