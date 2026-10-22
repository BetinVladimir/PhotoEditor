package v2render

import (
	"bytes"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type SQLAuthoredFinalGenerationReader struct {
	Scheduler *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}
type authoredGenerationObject struct {
	Ref     v2input.Ref `json:"ref"`
	Bucket  string      `json:"bucket"`
	Key     string      `json:"key"`
	Version string      `json:"version"`
	Bytes   int64       `json:"bytes"`
}
type authoredGenerationObjects struct {
	Context       AuthoredInstructionSource        `json:"context"`
	Evidence      authoredGenerationObject         `json:"evidence"`
	Report        *authoredGenerationObject        `json:"report"`
	Qualification *RenderQualification             `json:"qualification,omitempty"`
	Policy        *v2qualification.DeploymentLocks `json:"policy,omitempty"`
}

func (s SQLAuthoredFinalGenerationReader) locator(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, evidence v2input.Ref, report *v2input.Ref) (authoredGenerationObjects, []byte, error) {
	var zero authoredGenerationObjects
	if e := ctx.Err(); e != nil {
		return zero, nil, e
	}
	if s.Scheduler == nil || s.Objects == nil || s.Bucket == "" || who.UserID != source.Actor || who.SessionID != source.Session || source.Instruction.Kind != "finalize" || !authoredGenerationRefValid(evidence) {
		return zero, nil, v2input.ErrInvalid
	}
	var raw []byte
	var id, sha any
	if report != nil {
		id, sha = report.ArtifactID, report.SHA256
	}
	e := database.TenantTransaction(ctx, s.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		var role string
		if e := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); e != nil {
			return e
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT photo.authored_render_final_generation_objects($1,$2,$3,$4,$5,$6,$7,$8,$9)`, source.Tenant, source.Parent, source.Instruction.Ordinal, who.UserID, who.SessionID, evidence.ArtifactID, evidence.SHA256, id, sha).Scan(&raw)
	})
	if e != nil {
		return zero, nil, e
	}
	if len(raw) < 2 || bytes.Equal(raw, []byte("null")) {
		return zero, nil, v2input.ErrExpired
	}
	wire, e := canonical.Bytes(raw)
	if e != nil || decodeAuthoredParams(wire, &zero) != nil {
		return zero, nil, v2input.ErrInvalid
	}
	expected, e := canonical.Marshal(source)
	actual, x := canonical.Marshal(zero.Context)
	if e != nil || x != nil || !bytes.Equal(expected, actual) || zero.Evidence.Ref != evidence {
		return zero, nil, v2input.ErrExpired
	}
	for _, o := range []*authoredGenerationObject{&zero.Evidence, zero.Report} {
		if o == nil {
			continue
		}
		if !authoredGenerationRefValid(o.Ref) || o.Bucket != s.Bucket || o.Key == "" || o.Version == "" || o.Version == "null" || o.Bytes < 2 || o.Bytes > 4194304 {
			return zero, nil, v2input.ErrInvalid
		}
	}
	if report != nil && (zero.Report == nil || zero.Report.Ref != *report || zero.Policy == nil || zero.Qualification == nil) {
		return zero, nil, v2input.ErrInvalid
	}
	return zero, wire, nil
}
func (s SQLAuthoredFinalGenerationReader) read(ctx context.Context, o authoredGenerationObject) ([]byte, error) {
	return v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objectstore.Object{Key: o.Key, VersionID: o.Version, SHA256: o.Ref.SHA256, Bytes: o.Bytes, ContentType: "application/json"}, o.Ref)
}
func (s SQLAuthoredFinalGenerationReader) Read(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, ref v2input.Ref) (VerifiedAuthoredFinalGeneration, error) {
	var zero VerifiedAuthoredFinalGeneration
	first, firstJCS, e := s.locator(ctx, who, source, ref, nil)
	if e != nil {
		return zero, e
	}
	raw, e := s.read(ctx, first.Evidence)
	if e != nil {
		return zero, e
	}
	var doc AuthoredFinalGenerationEvidence
	if decodeAuthoredParams(raw, &doc) != nil || !authoredGenerationRefValid(doc.Report) {
		return zero, v2input.ErrInvalid
	}
	_, after, e := s.locator(ctx, who, source, ref, nil)
	if e != nil {
		return zero, e
	}
	if !bytes.Equal(firstJCS, after) {
		return zero, v2input.ErrExpired
	}
	current, before, e := s.locator(ctx, who, source, ref, &doc.Report)
	if e != nil {
		return zero, e
	}
	report, e := s.read(ctx, *current.Report)
	if e != nil {
		return zero, e
	}
	proof, e := VerifyAuthoredFinalGenerationEvidence(ctx, raw, ref, report, *current.Policy)
	if e != nil {
		return zero, e
	}
	verified, e := proof.Evidence()
	if e != nil || verified.Qualification != *current.Qualification {
		return zero, v2input.ErrInvalid
	}
	_, after, e = s.locator(ctx, who, source, ref, &doc.Report)
	if e != nil {
		return zero, e
	}
	if !bytes.Equal(before, after) {
		return zero, v2input.ErrExpired
	}
	// Publish only this actually verified physical report/document pair. The
	// protected writer rechecks exact qualified report, policy and current source.
	e = database.TenantTransaction(ctx, s.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.record_authored_final_generation_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9)`, source.Tenant, source.Parent, source.Instruction.Ordinal, who.UserID, who.SessionID, ref.ArtifactID, ref.SHA256, string(raw), string(report)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
	if e != nil {
		return zero, e
	}
	proof.documents = []v2draft.DocumentWitness{
		{Path: "AuthoredFinal.generation", Contract: "AuthoredFinalGenerationEvidence", Ref: ref, Object: objectstore.Object{Key: current.Evidence.Key, VersionID: current.Evidence.Version, SHA256: ref.SHA256, Bytes: current.Evidence.Bytes, ContentType: "application/json"}, JCS: string(raw)},
		{Path: "AuthoredFinal.generation.qualification_report", Contract: "QualificationReport", Ref: doc.Report, Parent: ref, Object: objectstore.Object{Key: current.Report.Key, VersionID: current.Report.Version, SHA256: doc.Report.SHA256, Bytes: current.Report.Bytes, ContentType: "application/json"}, JCS: string(report)},
	}
	return proof, ctx.Err()
}
