package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2sessions"
)

type ExportCorrectionProducerEvidence struct {
	Group    uuid.UUID   `json:"group_id"`
	Snapshot uuid.UUID   `json:"snapshot_id"`
	Digest   string      `json:"snapshot_digest"`
	Policy   string      `json:"policy_sha"`
	Count    int         `json:"item_count"`
	Root     v2input.Ref `json:"root"`
	Bucket   string      `json:"bucket"`
	Key      string      `json:"object_key"`
	Version  string      `json:"object_version"`
	Bytes    int64       `json:"bytes"`
	MIME     string      `json:"media_type"`
}

func (e ExportCorrectionProducerEvidence) valid() bool {
	return e.Group != uuid.Nil && e.Snapshot != uuid.Nil && validImportRef(e.Root) && validImportRef(v2input.Ref{ArtifactID: e.Group, SHA256: e.Digest}) && validImportRef(v2input.Ref{ArtifactID: e.Group, SHA256: e.Policy}) && e.Count >= 1 && e.Count <= 5000 && len(e.Bucket) >= 3 && len(e.Bucket) <= 63 && e.Key != "" && e.Version != "" && e.Version != "null" && e.Bytes >= 2 && e.Bytes <= 8<<20 && e.MIME == "application/json"
}

type ExportCorrectionProducerSource interface {
	LookupExportCorrectionEvidence(context.Context, v2input.Entry, []byte) (ExportCorrectionProducerEvidence, error)
}

func (s SQLExportDocumentCatalog) LookupExportCorrectionEvidence(ctx context.Context, entry v2input.Entry, raw []byte) (ExportCorrectionProducerEvidence, error) {
	return lookupExportCorrectionEvidence(ctx, s, s.DB, s.Tenant, s.Who, s.Request.ProjectID, s.Bucket, entry, raw)
}

func (s SQLExportResultInputCatalog) LookupExportCorrectionEvidence(ctx context.Context, entry v2input.Entry, raw []byte) (ExportCorrectionProducerEvidence, error) {
	config := s.Export
	return lookupExportCorrectionEvidence(ctx, s, config.DB, config.Tenant, config.Who, config.Request.ProjectID, config.Bucket, entry, raw)
}

func lookupExportCorrectionEvidence(ctx context.Context, catalog ExportDocumentCatalog, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, project uuid.UUID, bucketName string, entry v2input.Entry, raw []byte) (ExportCorrectionProducerEvidence, error) {
	var zero ExportCorrectionProducerEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || catalog == nil || tenantID == uuid.Nil || project == uuid.Nil || len(bucketName) < 3 || entry.Contract != "CorrectionPlan" || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != entry.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	// The concrete catalog checks either draft authority or exact canonical
	// result input membership before producer metadata can be observed.
	before, err := catalog.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	var packet []byte
	err = tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT photo.v30_export_correction_producer_evidence($1,$2,$3,$4,$5,$6,$7)`, tenantID, project, who.UserID, who.SessionID, entry.Ref.ArtifactID, entry.Ref.SHA256, string(raw)).Scan(&packet)
	})
	if err != nil {
		return zero, err
	}
	normalized, err := canonical.Bytes(packet)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var evidence ExportCorrectionProducerEvidence
	if err := decodeImportDocument(normalized, &evidence); err != nil {
		return zero, err
	}
	if !evidence.valid() || evidence.Bucket != bucketName {
		return zero, v2input.ErrInvalid
	}
	after, err := catalog.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	if after != before {
		return zero, v2input.ErrInvalid
	}
	return evidence, ctx.Err()
}

type ExportCorrectionProducerSemantics struct {
	Source   ExportCorrectionProducerSource
	Objects  ImportDocumentObjects
	Fallback ExportDependencySemantics
}

func (s ExportCorrectionProducerSemantics) QualifyExportDependency(ctx context.Context, entry v2input.Entry, raw []byte, hints []v2draft.LayerRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Source == nil || s.Objects == nil || s.Fallback == nil {
		return v2input.ErrInvalid
	}
	if entry.Contract == "CorrectionPlan" {
		body := append([]byte(nil), raw...)
		if !validImportRef(entry.Ref) || rawSHA(body) != entry.Ref.SHA256 {
			return v2input.ErrInvalid
		}
		var plan struct {
			Items []struct {
				Binding v2input.SessionVersionBinding `json:"binding"`
				Ordinal int                           `json:"ordinal"`
			} `json:"items"`
		}
		if len(body) < 2 || len(body) > 8<<20 || json.Unmarshal(body, &plan) != nil || len(plan.Items) == 0 {
			return v2input.ErrInvalid
		}
		normalized, err := canonical.Bytes(body)
		if err != nil || !bytes.Equal(normalized, body) {
			return v2input.ErrInvalid
		}
		if err := v2draft.ValidateCorrectionPlan(body, plan.Items[0].Binding); err != nil {
			return err
		}
		evidence, err := s.Source.LookupExportCorrectionEvidence(ctx, entry, append([]byte(nil), body...))
		if err != nil {
			return err
		}
		if !evidence.valid() {
			return v2input.ErrInvalid
		}
		var header struct {
			Digest string `json:"snapshot_digest"`
		}
		if json.Unmarshal(body, &header) != nil || header.Digest != evidence.Digest {
			return v2input.ErrInvalid
		}
		ordinals := make([]int, len(plan.Items))
		for index, item := range plan.Items {
			ordinals[index] = item.Ordinal
		}
		reader := ImportJSONNodeResolver{Catalog: exportCorrectionRootCatalog{source: s.Source, entry: entry, body: body, expected: evidence}, Objects: s.Objects,
			Validator: exportCorrectionRootValidator{expected: evidence, correction: entry.Ref, ordinals: ordinals}}
		if _, err := reader.ResolveImportNode(ctx, v2input.Entry{Ref: evidence.Root, Contract: "ScenePlan"}); err != nil {
			return err
		}
	}
	if err := s.Fallback.QualifyExportDependency(ctx, entry, append([]byte(nil), raw...), hints); err != nil {
		return err
	}
	return ctx.Err()
}

type exportCorrectionRootCatalog struct {
	source   ExportCorrectionProducerSource
	entry    v2input.Entry
	body     []byte
	expected ExportCorrectionProducerEvidence
}

func (s exportCorrectionRootCatalog) LookupImportDocument(ctx context.Context, entry v2input.Entry) (objectstore.Object, error) {
	if entry.Contract != "ScenePlan" || entry.Ref != s.expected.Root {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	current, err := s.source.LookupExportCorrectionEvidence(ctx, s.entry, append([]byte(nil), s.body...))
	if err != nil {
		return objectstore.Object{}, err
	}
	if current != s.expected {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return objectstore.Object{Key: current.Key, VersionID: current.Version, SHA256: current.Root.SHA256, Bytes: current.Bytes, ContentType: current.MIME}, ctx.Err()
}

type exportCorrectionRootValidator struct {
	expected   ExportCorrectionProducerEvidence
	correction v2input.Ref
	ordinals   []int
}

func (v exportCorrectionRootValidator) ImportDocumentChildren(ctx context.Context, entry v2input.Entry, raw []byte) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entry.Contract != "ScenePlan" || entry.Ref != v.expected.Root {
		return nil, v2input.ErrInvalid
	}
	if err := v2sessions.ValidateSceneCorrectionEdge(raw, entry.Ref.SHA256, v.expected.Snapshot, v.expected.Digest, v.expected.Policy, v.expected.Count, v.expected.Group,
		v2sessions.ArtifactRef{ArtifactID: v.correction.ArtifactID, SHA256: v.correction.SHA256}, v.ordinals); err != nil {
		return nil, err
	}
	return nil, ctx.Err()
}

var _ ExportCorrectionProducerSource = SQLExportDocumentCatalog{}
var _ ExportCorrectionProducerSource = SQLExportResultInputCatalog{}
var _ ExportDependencySemantics = ExportCorrectionProducerSemantics{}
