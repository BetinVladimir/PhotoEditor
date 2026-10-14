package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type RelightAnalysisArrayCatalog interface {
	ResolveAnalysisArray(context.Context, RelightAnalysisPlanRequest, RelightAnalysisArraySpec) (objectstore.Object, error)
}
type SQLRelightAnalysisArrayCatalog struct {
	DB     DonorConfirmationTransactions
	Bucket string
}

func (c SQLRelightAnalysisArrayCatalog) ResolveAnalysisArray(ctx context.Context, r RelightAnalysisPlanRequest, spec RelightAnalysisArraySpec) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Bucket == "" || len(c.Bucket) > 255 || r.Tenant == uuid.Nil || r.Project == uuid.Nil || r.Snapshot == uuid.Nil || r.Actor == uuid.Nil || r.Session == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	specs, err := AnalysisSourceArrays(r)
	if err != nil {
		return zero, err
	}
	found := false
	for _, known := range specs {
		if known == spec {
			found = true
		}
	}
	if !found {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Marshal(r.Base)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = (relightAnalysisParamsRepository{}).authorize(ctx, tx, artifactjson.Scope{Tenant: r.Tenant}); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.v28_relight_analysis_array_source($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)", r.Tenant, r.Project, r.Snapshot, r.Actor, r.Session, string(base), string(r.SourceRaw), spec.Ref.ArtifactID, spec.Ref.SHA256, spec.Role).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 16384 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var wire struct {
		ID      uuid.UUID `json:"artifact_id"`
		SHA     string    `json:"sha256"`
		Bucket  string    `json:"bucket"`
		Key     string    `json:"object_key"`
		Version string    `json:"object_version"`
		Bytes   int64     `json:"bytes"`
		Media   string    `json:"media_type"`
	}
	if json.Unmarshal(normal, &wire) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(normal, exact) || wire.ID != spec.Ref.ArtifactID || wire.SHA != spec.Ref.SHA256 || wire.Bucket != c.Bucket || wire.Key == "" || len(wire.Key) > 1024 || wire.Version == "" || wire.Version == "null" || len(wire.Version) > 1024 || wire.Bytes <= 0 {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return objectstore.Object{Key: wire.Key, VersionID: wire.Version, SHA256: wire.SHA, Bytes: wire.Bytes, ContentType: wire.Media}, nil
}

// VerifyAnalysisSourceObjects completes each locator transaction before object
// IO and rechecks all occurrences after streaming. It grants no admission token.
func VerifyAnalysisSourceObjects(ctx context.Context, catalog RelightAnalysisArrayCatalog, objects v2input.SessionAnalysisObjectReader, r RelightAnalysisPlanRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if catalog == nil || objects == nil {
		return v2input.ErrInvalid
	}
	r.SourceRaw = append([]byte(nil), r.SourceRaw...)
	specs, err := AnalysisSourceArrays(r)
	if err != nil {
		return err
	}
	pinned := make([]objectstore.Object, len(specs))
	for i, spec := range specs {
		object, err := catalog.ResolveAnalysisArray(ctx, r, spec)
		if err != nil {
			return err
		}
		reader, err := objects.Open(ctx, object)
		if err != nil {
			return err
		}
		if reader == nil {
			return v2input.ErrInvalid
		}
		verifyErr := VerifyRelightAnalysisArray(ctx, spec, object, reader)
		closeErr := reader.Close()
		if verifyErr != nil {
			return verifyErr
		}
		if closeErr != nil {
			return closeErr
		}
		current, err := catalog.ResolveAnalysisArray(ctx, r, spec)
		if err != nil {
			return err
		}
		if current != object {
			return v2input.ErrExpired
		}
		pinned[i] = object
	}
	for i, spec := range specs {
		current, err := catalog.ResolveAnalysisArray(ctx, r, spec)
		if err != nil {
			return err
		}
		if current != pinned[i] {
			return v2input.ErrExpired
		}
	}
	return ctx.Err()
}
