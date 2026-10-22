package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
)

type authoredNativeInputCatalog struct {
	SQLAuthoredGeometryInputCatalog
	materialized AuthoredNativeMaterialization
	params       v2input.Ref
	multi        *authoredNativeMultiInputs
}

func (s authoredNativeInputCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	if e := ctx.Err(); e != nil {
		return objectstore.Object{}, e
	}
	if s.multi != nil {
		pin := s.multi.pin
		if node.Ref == pin.Report || node.Ref == pin.Calibration {
			bucket, report, cal, e := s.multi.catalog.ResolveMultiQualification(ctx, who, tenant, s.materialized.source.Project, pin, s.materialized.selected.Operation)
			if e != nil {
				return objectstore.Object{}, e
			}
			contract, object := "Report", report
			if node.Ref == pin.Calibration {
				contract, object = "Calibration", cal
			}
			if node.Contract != contract || bucket != s.Objects.Bucket {
				return objectstore.Object{}, v2input.ErrInvalid
			}
			return object, nil
		}
	}
	if node.Ref != s.params {
		return s.SQLAuthoredGeometryInputCatalog.Lookup(ctx, who, tenant, node)
	}
	var zero objectstore.Object
	m := s.materialized
	if tenant != m.source.Tenant || who.UserID != m.source.Actor || who.SessionID != m.source.Session || node.Contract != "Params" {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	e := database.TenantTransaction(ctx, s.Scheduler, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.authored_native_params_object($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, m.source.Parent, m.source.Instruction.Ordinal, m.selected.Operation, string(m.authored), string(m.selected.Params.Body), s.params.ArtifactID, s.params.SHA256).Scan(&raw)
	})
	if e != nil {
		return zero, e
	}
	if len(raw) < 2 || bytes.Equal(raw, []byte("null")) {
		return zero, v2input.ErrExpired
	}
	var o struct {
		Context AuthoredInstructionSource `json:"context"`
		Ref     v2input.Ref               `json:"ref"`
		Bucket  string                    `json:"bucket"`
		Key     string                    `json:"object_key"`
		Version string                    `json:"object_version"`
		Bytes   int64                     `json:"bytes"`
	}
	a, e := canonical.Marshal(m.source)
	if e != nil || decodeAuthoredParams(raw, &o) != nil {
		return zero, v2input.ErrInvalid
	}
	b, e := canonical.Marshal(o.Context)
	if e != nil || !bytes.Equal(a, b) || o.Ref != s.params || o.Bucket != s.Objects.Bucket || o.Key == "" || o.Version == "" || o.Version == "null" || o.Bytes != int64(len(m.selected.Params.Body)) {
		return zero, v2input.ErrInvalid
	}
	return objectstore.Object{Key: o.Key, VersionID: o.Version, SHA256: s.params.SHA256, Bytes: o.Bytes, ContentType: "application/json"}, nil
}

// Frames are source-selected, original-frame order retained. Source documents
// come from the same scoped original/prefix catalog used by the real resolver.
func readAuthoredNativeSourceDocuments(ctx context.Context, who auth.Identity, m AuthoredNativeMaterialization, catalog SQLAuthoredGeometryInputCatalog) ([][]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	var refs []v2input.Ref
	switch m.selected.Operation {
	case "image.upscale":
		var p struct {
			Image v2input.Ref `json:"image"`
		}
		if json.Unmarshal(m.selected.Params.Body, &p) != nil {
			return nil, v2input.ErrInvalid
		}
		refs = []v2input.Ref{p.Image}
	case "optical.panorama":
		p, e := v2optical.ParsePanoramaParams(m.selected.Params.Body)
		if e != nil {
			return nil, e
		}
		for _, f := range p.Frames {
			refs = append(refs, f.Source)
		}
	case "optical.focus_stack":
		p, e := v2optical.ParseFocusStackParams(m.selected.Params.Body)
		if e != nil {
			return nil, e
		}
		for _, f := range p.Frames {
			refs = append(refs, f.Source)
		}
	default:
		return nil, v2input.ErrInvalid
	}
	if len(refs) < 1 || len(refs) > 128 {
		return nil, v2input.ErrInvalid
	}
	out := make([][]byte, len(refs))
	for i, ref := range refs {
		node := v2draft.LayerRef{Path: "AuthoredNative.frame", Ref: ref, Contract: "WorkingImage"}
		before, e := catalog.Lookup(ctx, who, m.source.Tenant, node)
		if e != nil {
			return nil, e
		}
		raw, e := v2input.ReadPinnedSemanticDocument(ctx, catalog.Objects.Objects, before, ref)
		if e != nil {
			return nil, e
		}
		after, e := catalog.Lookup(ctx, who, m.source.Tenant, node)
		if e != nil {
			return nil, e
		}
		if before != after {
			return nil, v2input.ErrExpired
		}
		out[i] = append([]byte(nil), raw...)
	}
	return out, ctx.Err()
}
func readAuthoredNativeInputs(ctx context.Context, who auth.Identity, m AuthoredNativeMaterialization, params v2input.Ref, catalog SQLAuthoredGeometryInputCatalog, multi *authoredNativeMultiInputs) (v2draft.TaskInputEvidence, error) {
	var zero v2draft.TaskInputEvidence
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	prefix, found, e := ReadAuthoredGeometryPrefix(ctx, catalog.Scheduler, m.source.Tenant, m.source.Parent, m.source.Instruction.Ordinal)
	if e != nil {
		return zero, e
	}
	if !found {
		return zero, v2input.ErrExpired
	}
	var base struct {
		Original v2input.SessionVersionBinding `json:"original_source"`
	}
	if json.Unmarshal([]byte(prefix.BaseReceiptJCS), &base) != nil || !authoredBindingValid(base.Original) {
		return zero, v2input.ErrInvalid
	}
	scoped := authoredNativeInputCatalog{SQLAuthoredGeometryInputCatalog: catalog, materialized: m, params: params, multi: multi}
	resolver := v2draft.ClosureResolver{Catalog: scoped, Objects: catalog.Objects.Objects}
	roots := []v2draft.LayerRef{{Path: "AuthoredNative.original_source", Ref: base.Original.Source, Contract: "WorkingImage", CanvasHash: base.Original.CanvasHash}, {Path: "AuthoredNative.params", Ref: params, Contract: "Params"}}
	if multi != nil {
		if m.selected.Operation != "optical.panorama" && m.selected.Operation != "optical.focus_stack" {
			return zero, v2input.ErrInvalid
		}
		roots = append(roots, v2draft.LayerRef{Path: "AuthoredNative.calibration", Ref: multi.pin.Calibration, Contract: "Calibration"}, v2draft.LayerRef{Path: "AuthoredNative.report", Ref: multi.pin.Report, Contract: "Report"})
	}
	evidence, e := resolver.ResolveBoundProvenanceEvidence(ctx, who, m.source.Tenant, roots, base.Original)
	if e != nil {
		return zero, e
	}
	after, present, e := ReadAuthoredGeometryPrefix(ctx, catalog.Scheduler, m.source.Tenant, m.source.Parent, m.source.Instruction.Ordinal)
	if e != nil {
		return zero, e
	}
	a, e := canonical.Marshal(prefix)
	if e != nil {
		return zero, e
	}
	b, e := canonical.Marshal(after)
	if e != nil || !present || !bytes.Equal(a, b) {
		return zero, v2input.ErrExpired
	}
	return evidence, ctx.Err()
}

// Only an actually loaded and independently verified multi policy selects these
// two additional roots. Every resolver lookup repeats the real SQL authority.
type authoredNativeMultiInputs struct {
	pin     v2optical.MultiQualificationPin
	catalog v2optical.SQLMultiQualificationCatalog
}
