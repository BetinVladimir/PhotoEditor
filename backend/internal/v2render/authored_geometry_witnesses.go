package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type SQLAuthoredGeometryWitnessReader struct {
	Scheduler *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}

// Read derives every locator from the current geometry prefix. Compound bytes
// are read at canonical receipt hashes before selecting registered-original;
// the full owning context independently validates the complete witness vector.
func (r SQLAuthoredGeometryWitnessReader) Read(ctx context.Context, prefix AuthoredGeometryPrefix) (AuthoredGeometryScopeWitnesses, v2input.Ref, error) {
	zero := AuthoredGeometryScopeWitnesses{}
	if e := ctx.Err(); e != nil {
		return zero, v2input.Ref{}, e
	}
	s := prefix.Context
	if r.Scheduler == nil || r.Objects == nil || r.Bucket == "" || s.Instruction.Kind != "geometry" {
		return zero, v2input.Ref{}, v2input.ErrInvalid
	}
	var base struct {
		Original v2input.SessionVersionBinding `json:"original_source"`
	}
	if json.Unmarshal([]byte(prefix.BaseReceiptJCS), &base) != nil || !authoredBindingValid(base.Original) {
		return zero, v2input.Ref{}, v2input.ErrInvalid
	}
	type locator struct {
		Ref     v2input.Ref `json:"ref"`
		Bucket  string      `json:"bucket"`
		Key     string      `json:"object_key"`
		Version string      `json:"object_version"`
		Bytes   int64       `json:"bytes"`
	}
	read := func(ref v2input.Ref, compound *string) ([]byte, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		selectObject := func() ([]byte, error) {
			var raw []byte
			e := database.TenantTransaction(ctx, r.Scheduler, s.Tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT photo.read_authored_render_geometry_prefix_object($1,$2,$3,$4::jsonb,$5)`, s.Tenant, s.Parent, s.Instruction.Ordinal, mustAuthoredGeometryRefJSON(ref), compound).Scan(&raw)
			})
			return raw, e
		}
		before, e := selectObject()
		if e != nil {
			return nil, e
		}
		var obj locator
		if len(before) == 0 || decodeAuthoredParams(before, &obj) != nil || obj.Ref != ref || obj.Bucket != r.Bucket || obj.Key == "" || obj.Version == "" || obj.Version == "null" || obj.Bytes < 2 || obj.Bytes > 1048576 {
			return nil, v2input.ErrExpired
		}
		raw, e := v2input.ReadPinnedSemanticDocument(ctx, r.Objects, objectstore.Object{Key: obj.Key, VersionID: obj.Version, SHA256: ref.SHA256, Bytes: obj.Bytes, ContentType: "application/json"}, ref)
		if e != nil {
			return nil, e
		}
		wire, e := canonical.Bytes(raw)
		if e != nil || !bytes.Equal(wire, raw) {
			return nil, v2input.ErrInvalid
		}
		after, e := selectObject()
		if e != nil {
			return nil, e
		}
		if !bytes.Equal(before, after) {
			return nil, v2input.ErrExpired
		}
		return raw, ctx.Err()
	}
	compounds := []struct {
		Ordinal int    `json:"ordinal"`
		JCS     string `json:"jcs"`
	}{}
	parallel := base.Original.Source
	var lastCompound *string
	for _, item := range prefix.Phases {
		if item.Kind != "geometry" {
			continue
		}
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) != nil {
			return zero, v2input.Ref{}, v2input.ErrInvalid
		}
		raw, e := read(phase.Result, nil)
		if e != nil {
			return zero, v2input.Ref{}, e
		}
		var compound struct {
			Registered v2input.Ref `json:"registered_original"`
		}
		if json.Unmarshal(raw, &compound) != nil || !validLayerArtifactRef(compound.Registered) {
			return zero, v2input.Ref{}, v2input.ErrInvalid
		}
		text := string(raw)
		lastCompound = &text
		parallel = compound.Registered
		compounds = append(compounds, struct {
			Ordinal int    `json:"ordinal"`
			JCS     string `json:"jcs"`
		}{item.Ordinal, text})
	}
	sourceRaw, e := read(s.Source.Source, nil)
	if e != nil {
		return zero, v2input.Ref{}, e
	}
	parallelRaw, e := read(parallel, lastCompound)
	if e != nil {
		return zero, v2input.Ref{}, e
	}
	if _, e = review.ParseWorkingImage(sourceRaw); e != nil {
		return zero, v2input.Ref{}, e
	}
	if _, e = review.ParseWorkingImage(parallelRaw); e != nil {
		return zero, v2input.Ref{}, e
	}
	encoded, e := canonical.Marshal(compounds)
	if e != nil {
		return zero, v2input.Ref{}, e
	}
	witness := AuthoredGeometryScopeWitnesses{Compounds: encoded, SourceJCS: string(sourceRaw), ParallelJCS: string(parallelRaw)}
	var checked []byte
	e = database.TenantTransaction(ctx, r.Scheduler, s.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.authored_render_geometry_input_context($1,$2,$3,$4::jsonb,$5,$6)`, s.Tenant, s.Parent, s.Instruction.Ordinal, encoded, witness.SourceJCS, witness.ParallelJCS).Scan(&checked)
	})
	if e != nil {
		return zero, v2input.Ref{}, e
	}
	if len(checked) == 0 {
		return zero, v2input.Ref{}, v2input.ErrExpired
	}
	fresh, found, e := ReadAuthoredGeometryPrefix(ctx, r.Scheduler, s.Tenant, s.Parent, s.Instruction.Ordinal)
	if e != nil {
		return zero, v2input.Ref{}, e
	}
	a, e := canonical.Marshal(prefix)
	if e != nil {
		return zero, v2input.Ref{}, e
	}
	b, e := canonical.Marshal(fresh)
	if e != nil || !found || !bytes.Equal(a, b) {
		return zero, v2input.Ref{}, v2input.ErrExpired
	}
	if e = ctx.Err(); e != nil {
		return zero, v2input.Ref{}, e
	}
	return witness, parallel, nil
}
func mustAuthoredGeometryRefJSON(ref v2input.Ref) []byte {
	raw, _ := canonical.Marshal(ref)
	return raw
}
