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
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type SQLAuthoredGeometryNativeReader struct {
	Scheduler *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}
type CurrentAuthoredGeometryNative struct {
	witness   AuthoredGeometryPhaseWitness
	documents map[v2input.Ref][]byte
	proof     []byte
}

func (r SQLAuthoredGeometryNativeReader) Read(ctx context.Context, source AuthoredInstructionSource) (CurrentAuthoredGeometryNative, error) {
	zero := CurrentAuthoredGeometryNative{}
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if r.Scheduler == nil || r.Objects == nil || r.Bucket == "" || source.Instruction.Kind != "geometry" {
		return zero, v2input.ErrInvalid
	}
	readCandidate := func() ([]byte, error) {
		var raw []byte
		e := database.TenantTransaction(ctx, r.Scheduler, source.Tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT photo.read_authored_render_geometry_native_candidate($1,$2,$3)`, source.Tenant, source.Parent, source.Instruction.Ordinal).Scan(&raw)
		})
		return raw, e
	}
	before, e := readCandidate()
	if e != nil {
		return zero, e
	}
	if len(before) == 0 {
		return zero, ErrAuthoredGeometryPending
	}
	var candidate struct {
		Context       AuthoredInstructionSource      `json:"context"`
		Task          AuthoredPhaseTask              `json:"task"`
		Root          authoredGeometryProducerObject `json:"root"`
		UpscaleReport *v2input.Ref                   `json:"upscale_report"`
	}
	if json.Unmarshal(before, &candidate) != nil {
		return zero, v2input.ErrInvalid
	}
	a, e := canonical.Marshal(source)
	if e != nil {
		return zero, e
	}
	b, e := canonical.Marshal(candidate.Context)
	if e != nil || !bytes.Equal(a, b) {
		return zero, v2input.ErrExpired
	}
	physical := func(p authoredGeometryProducerObject) ([]byte, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if !validLayerArtifactRef(p.Ref) || p.Bucket != r.Bucket || p.Key == "" || p.Version == "" || p.Version == "null" || p.Bytes < 2 || p.Bytes > 8388608 {
			return nil, v2input.ErrInvalid
		}
		raw, e := v2input.ReadPinnedSemanticDocument(ctx, r.Objects, objectstore.Object{Key: p.Key, VersionID: p.Version, SHA256: p.Ref.SHA256, Bytes: p.Bytes, ContentType: "application/json"}, p.Ref)
		if e != nil {
			return nil, e
		}
		wire, e := canonical.Bytes(raw)
		if e != nil || !bytes.Equal(wire, raw) {
			return nil, v2input.ErrInvalid
		}
		return raw, nil
	}
	rootRaw, e := physical(candidate.Root)
	if e != nil {
		return zero, e
	}
	native := AuthoredGeometryPhaseWitness{Task: candidate.Task, Root: candidate.Root.Ref}
	if candidate.Task.Operation == "image.upscale" {
		if candidate.UpscaleReport == nil {
			return zero, v2input.ErrInvalid
		}
		native.Image = native.Root
		native.Report = *candidate.UpscaleReport
		if _, e = review.ParseWorkingImage(rootRaw); e != nil {
			return zero, e
		}
	} else {
		result, e := v2draft.ParseImageResult(rootRaw, native.Root)
		if e != nil {
			return zero, e
		}
		native.Image = result.WorkingImage
		native.Report = result.Report
	}
	nativeRaw, e := canonical.Marshal(native)
	if e != nil {
		return zero, e
	}
	readDocuments := func() ([]byte, error) {
		var raw []byte
		e := database.TenantTransaction(ctx, r.Scheduler, source.Tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT photo.read_authored_render_geometry_native_documents($1,$2,$3,$4::jsonb)`, source.Tenant, source.Parent, source.Instruction.Ordinal, nativeRaw).Scan(&raw)
		})
		return raw, e
	}
	proof, e := readDocuments()
	if e != nil {
		return zero, e
	}
	if len(proof) == 0 {
		return zero, v2input.ErrExpired
	}
	var row struct {
		Native  AuthoredGeometryPhaseWitness     `json:"native"`
		Objects []authoredGeometryProducerObject `json:"objects"`
	}
	if json.Unmarshal(proof, &row) != nil || len(row.Objects) != 3 {
		return zero, v2input.ErrInvalid
	}
	rowNative, e := canonical.Marshal(row.Native)
	if e != nil || !bytes.Equal(nativeRaw, rowNative) {
		return zero, v2input.ErrInvalid
	}
	docs := map[v2input.Ref][]byte{native.Root: rootRaw}
	for i, ref := range []v2input.Ref{native.Task.Params, native.Image, native.Report} {
		if row.Objects[i].Ref != ref {
			return zero, v2input.ErrInvalid
		}
		raw, e := physical(row.Objects[i])
		if e != nil {
			return zero, e
		}
		if prior, ok := docs[ref]; ok && !bytes.Equal(prior, raw) {
			return zero, v2input.ErrInvalid
		}
		docs[ref] = raw
	}
	if _, e = review.ParseWorkingImage(docs[native.Image]); e != nil {
		return zero, e
	}
	if native.Task.Operation == "image.upscale" {
		var params, report struct {
			Image v2input.Ref `json:"image"`
		}
		if json.Unmarshal(docs[native.Task.Params], &params) != nil || json.Unmarshal(docs[native.Report], &report) != nil || params.Image != source.Source.Source || report.Image != params.Image {
			return zero, v2input.ErrInvalid
		}
		if _, e = v2draft.DocumentChildren(docs[native.Report], "UpscaleReport", native.Report); e != nil {
			return zero, e
		}
	} else {
		if _, e = v2draft.DocumentChildren(docs[native.Report], "ImageReport", native.Report); e != nil {
			return zero, e
		}
	}
	after, e := readDocuments()
	if e != nil {
		return zero, e
	}
	again, e := readCandidate()
	if e != nil {
		return zero, e
	}
	if !bytes.Equal(proof, after) || !bytes.Equal(before, again) {
		return zero, v2input.ErrExpired
	}
	if e = ctx.Err(); e != nil {
		return zero, e
	}
	return CurrentAuthoredGeometryNative{witness: native, documents: docs, proof: append([]byte(nil), after...)}, nil
}
