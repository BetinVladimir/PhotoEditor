package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"sort"
	"time"
)

type InteractiveLease struct {
	TenantID, RunID, TaskID, SourceID, Token         uuid.UUID
	Kind, Operation, ParamsSHA, Backend, RuntimeLock string
	Declared, Params                                 json.RawMessage
}
type InteractiveStore struct {
	DB interface {
		artifactjson.RowQuerier
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}
}

func (s InteractiveStore) Claim(ctx context.Context) (*InteractiveLease, error) {
	v := new(InteractiveLease)
	var lock *string
	e := s.DB.QueryRow(ctx, `SELECT tenant_id,run_id,task_id,kind,operation,source_artifact_id,declared_inputs,params,params_sha256,backend,runtime_lock_id,token FROM photo.claim_interactive_task()`).Scan(&v.TenantID, &v.RunID, &v.TaskID, &v.Kind, &v.Operation, &v.SourceID, &v.Declared, &v.Params, &v.ParamsSHA, &v.Backend, &lock, &v.Token)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if lock != nil {
		v.RuntimeLock = *lock
	}
	return v, e
}
func (s InteractiveStore) Source(ctx context.Context, l InteractiveLease) (styleArtifact, error) {
	var a styleArtifact
	e := s.DB.QueryRow(ctx, `SELECT id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND state='ready' AND object_version IS NOT NULL AND (expires_at IS NULL OR expires_at>clock_timestamp())`, l.TenantID, l.SourceID).Scan(&a.ArtifactID, &a.SHA256, &a.Bytes, &a.Bucket, &a.Key, &a.VersionID, &a.MediaType)
	return a, e
}
func (s InteractiveStore) Materialize(ctx context.Context, l InteractiveLease, p artifactjson.Ref, inputs []review.ArtifactRef, digest string, profile []byte) (bool, error) {
	raw, e := canonical.Marshal(inputs)
	if e != nil {
		return false, e
	}
	var ok bool
	e = s.DB.QueryRow(ctx, `SELECT photo.materialize_interactive_task($1,$2,$3,$4,$5,$6,$7,$8,$9)`, l.TenantID, l.RunID, l.TaskID, l.Token, p.ID, p.SHA256, raw, digest, profile).Scan(&ok)
	return ok, e
}
func (s InteractiveStore) Plan(ctx context.Context, l *InteractiveLease, params []byte) (bool, error) {
	h, e := canonical.Hash(json.RawMessage(params))
	if e != nil {
		return false, e
	}
	var ok bool
	e = s.DB.QueryRow(ctx, `SELECT photo.plan_interactive_params($1,$2,$3,$4)`, l.TaskID, l.Token, params, h).Scan(&ok)
	if ok {
		l.Params = append([]byte(nil), params...)
		l.ParamsSHA = h
	}
	return ok, e
}

type InteractiveDocuments struct {
	DB     artifactjson.RowQuerier
	Bucket string
}

func (r InteractiveDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	e := r.DB.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_interactive_document($1,$2,$3,$4,$5,$6)`, s.Tenant, s.Run, s.Owner, sha, size, r.Bucket).Scan(&outcome, &id, &token, &ready)
	if e != nil {
		return artifactjson.Reservation{}, e
	}
	switch outcome {
	case "quota":
		return artifactjson.Reservation{}, artifactjson.ErrQuota
	case "conflict":
		return artifactjson.Reservation{}, artifactjson.ErrConflict
	case "forbidden", "stale":
		return artifactjson.Reservation{}, artifactjson.ErrStale
	}
	if id == nil || token == nil {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	v := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		v.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	return v, nil
}
func (r InteractiveDocuments) Complete(ctx context.Context, s artifactjson.Scope, t artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	var ok bool
	e := r.DB.QueryRow(ctx, `SELECT photo.complete_interactive_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.Tenant, s.Run, s.Owner, t.ArtifactID, t.Token, o.SHA256, o.Bytes, r.Bucket, o.VersionID).Scan(&ok)
	if e != nil {
		return artifactjson.Ref{}, e
	}
	if !ok {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: t.ArtifactID, SHA256: o.SHA256}, nil
}

type InteractiveCoordinator struct {
	Store     InteractiveStore
	Publisher ContractPublisher
	Objects   ArtifactOpener
	Bucket    string
}

func (c InteractiveCoordinator) ProcessOne(ctx context.Context) (bool, error) {
	l, e := c.Store.Claim(ctx)
	if e != nil || l == nil {
		return false, e
	}
	source, e := c.Store.Source(ctx, *l)
	if e != nil {
		return false, e
	}
	var working *review.WorkingImage
	if l.Kind != "contact_sheet" {
		value, readErr := readWorking(ctx, c.Objects, c.Bucket, source)
		if readErr != nil {
			return false, readErr
		}
		working = &value
	}
	if l.Kind == "mask_manual" {
		planned, err := verifyPlannedInteractiveManual(l.Params, l.ParamsSHA, l.TaskID, source.ref(), *working)
		if err != nil {
			return c.failInvalidManual(ctx, *l, "MANUAL_TEMPLATE_INVALID", err)
		}
		if !planned {
			var t struct {
				ObjectID  *uuid.UUID      `json:"object_id"`
				SourceSHA string          `json:"source_sha256"`
				Shapes    json.RawMessage `json:"shapes"`
			}
			if json.Unmarshal(l.Params, &t) != nil || t.SourceSHA != working.Canvas.SourceSHA256 {
				return c.failInvalidManual(ctx, *l, "MANUAL_TEMPLATE_INVALID", errors.New("invalid manual mask template"))
			}
			vectors := struct {
				Version   string          `json:"version"`
				SourceSHA string          `json:"source_sha256"`
				Canvas    review.Canvas   `json:"canvas"`
				Shapes    json.RawMessage `json:"shapes"`
			}{"manual_mask_v1", t.SourceSHA, working.Canvas, t.Shapes}
			vectorsBody, x := canonical.Marshal(vectors)
			if x != nil {
				return false, x
			}
			vectorsSHA, x := canonical.Hash(vectors)
			if x != nil {
				return false, x
			}
			// Local validation identity only, never published or used as artifact
			// authority. Geometry/source checks use the independently read image.
			vectorsRef := review.ArtifactRef{ID: uuid.NewSHA1(l.TaskID, []byte("interactive.manual.vectors.validation")), SHA256: vectorsSHA}
			if x := review.VerifyManualVectorsDocument(vectorsBody, vectorsRef, working.Source(source.ref())); x != nil {
				return c.failInvalidManual(ctx, *l, "MANUAL_GEOMETRY_INVALID", x)
			}
			params, x := canonical.Marshal(struct {
				Image    review.ArtifactRef `json:"image"`
				Vectors  any                `json:"inline_vectors"`
				ObjectID *uuid.UUID         `json:"object_id,omitempty"`
			}{source.ref(), vectors, t.ObjectID})
			if x != nil {
				return false, x
			}
			ok, x := c.Store.Plan(ctx, l, params)
			if x != nil || !ok {
				if x == nil {
					x = errors.New("manual mask planning rejected")
				}
				return false, x
			}
		}
	}
	var declared []review.ArtifactRef
	if json.Unmarshal(l.Declared, &declared) != nil {
		return false, errors.New("invalid interactive inputs")
	}
	if l.Kind == "contact_sheet" {
		var params struct {
			Entries []struct {
				Preview review.ArtifactRef `json:"preview"`
			} `json:"entries"`
			Font    review.ArtifactRef `json:"font"`
			Columns int                `json:"columns"`
			Rows    int                `json:"rows"`
		}
		if json.Unmarshal(l.Params, &params) != nil || len(params.Entries) < 1 || len(params.Entries) > 10000 || params.Font != source.ref() || params.Columns < 1 || params.Columns > 8 || params.Rows < 1 || params.Rows > 10 || len(declared) != len(params.Entries)+1 {
			return false, errors.New("invalid contact sheet plan")
		}
		for i, entry := range params.Entries {
			if declared[i] != entry.Preview {
				return false, errors.New("contact sheet input order mismatch")
			}
		}
		if declared[len(declared)-1] != params.Font {
			return false, errors.New("contact sheet font binding mismatch")
		}
	} else {
		declared = append(declared, source.ref(), working.Pixels)
		if working.Alpha != nil {
			declared = append(declared, *working.Alpha)
		}
		sort.Slice(declared, func(i, j int) bool { return declared[i].ID.String() < declared[j].ID.String() })
	}
	inputs := declared[:0]
	for _, r := range declared {
		if len(inputs) == 0 || inputs[len(inputs)-1].ID != r.ID {
			inputs = append(inputs, r)
		} else if inputs[len(inputs)-1] != r {
			return false, errors.New("interactive artifact identity conflict")
		}
	}
	p, e := c.Publisher.Publish(ctx, artifactjson.Scope{Tenant: l.TenantID, Run: l.RunID, Owner: l.TaskID, Purpose: "task_params"}, l.Params)
	if e != nil {
		return false, e
	}
	digest, e := repairInputDigest(l.Operation, "1", review.ArtifactRef{ID: p.ID, SHA256: p.SHA256}, inputs)
	if e != nil {
		return false, e
	}
	pixels := int64(0)
	if working != nil {
		pixels = working.Canvas.Width * working.Canvas.Height
	}
	inputBytes := source.Bytes
	out := pixels*16 + 8*1048576
	ram := int64(1 << 30)
	if l.Operation == "image.generate" {
		ram = 4 << 30
		out = pixels*12 + 2*1048576
	}
	if l.Operation == "report.contact_sheet" {
		var params struct {
			Entries       []any `json:"entries"`
			Columns, Rows int
		}
		if json.Unmarshal(l.Params, &params) != nil {
			return false, errors.New("invalid contact sheet estimates")
		}
		pages := (int64(len(params.Entries)) + int64(params.Columns*params.Rows) - 1) / int64(params.Columns*params.Rows)
		pixels = int64(params.Columns*320) * int64(params.Rows*260) * pages
		out = pixels*3 + pages*1048576
		ram = 1 << 30
		inputBytes = 0
	}
	profile, e := canonical.Marshal(map[string]any{"backend": l.Backend, "precision": "float32", "tile_size": 512, "model_lock_id": l.RuntimeLock, "resource_estimate": map[string]int64{"ram_bytes": ram, "vram_bytes": map[string]int64{"cpu": 0, "mps": 2 << 30, "cuda": 2 << 30}[l.Backend], "scratch_bytes": inputBytes + out, "output_bytes": out, "pixel_count": pixels}})
	if e != nil {
		return false, e
	}
	ok, e := c.Store.Materialize(ctx, *l, p, inputs, digest, profile)
	if e != nil {
		return false, e
	}
	if !ok {
		return false, errors.New("interactive task materialization rejected")
	}
	return true, nil
}

func (c InteractiveCoordinator) failInvalidManual(ctx context.Context, l InteractiveLease, code string, cause error) (bool, error) {
	if err := ctx.Err(); err != nil {
		return true, err
	}
	finish, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var recorded bool
	err := c.Store.DB.QueryRow(finish, "SELECT photo.fail_interactive_manual_preflight($1,$2,$3,$4,$5,$6::jsonb,$7)", l.TenantID, l.RunID, l.TaskID, l.Token, l.ParamsSHA, string(l.Params), code).Scan(&recorded)
	if err != nil {
		return true, errors.Join(cause, err)
	}
	if !recorded {
		return true, errors.Join(cause, artifactjson.ErrStale)
	}
	return true, cause
}
