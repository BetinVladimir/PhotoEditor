package semanticproposal

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Independent original person input, without a coverage or parent-admission claim.
// The consumer must separately authorize its Params, union and output provenance.
type nativePersonInputAuthority struct {
	tenant, project, actor, session                       uuid.UUID
	bucket, seal, body, expected, evidence, qualification string
	binding                                               v2input.SessionVersionBinding
	root, pixels, report                                  v2input.Ref
	objects                                               [2]objectstore.Object
	policy                                                qualificationPolicySnapshot
}

func nativePersonInputAuthorityDigest(p nativePersonInputAuthority) (string, error) {
	return canonical.Hash(nativePersonInputAuthorityPayload(p))
}

func nativePersonInputAuthorityPayload(p nativePersonInputAuthority) map[string]any {
	return map[string]any{"tenant": p.tenant, "project": p.project, "actor": p.actor, "session": p.session, "bucket": p.bucket, "binding": p.binding, "root": p.root, "pixels": p.pixels, "report": p.report, "body": p.body, "expected": p.expected, "evidence": p.evidence, "qualification": p.qualification, "objects": p.objects, "policy": p.policy.body}
}

func (s SQLTransferSourceReader) validateNativePersonInputAuthority(ctx context.Context, who auth.Identity, request Request, p nativePersonInputAuthority) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.seal == "" || p.tenant != request.TenantID || p.project != s.ProjectID || p.actor != who.UserID || p.actor != request.ActorID || p.session != who.SessionID || p.bucket != s.Bucket || p.binding != request.Intent.Reference || !validQualificationPolicySnapshot(s, who, p.tenant, p.policy) {
		return v2input.ErrInvalid
	}
	digest, err := nativePersonInputAuthorityDigest(p)
	if err != nil {
		return err
	}
	if digest != p.seal {
		return v2input.ErrConflict
	}
	return nil
}

// All physical IO precedes admission TX. Repeats policy, candidate, report,
// producer and immutable object pins; caller cannot nominate arbitrary pixels.
func (s SQLTransferSourceReader) readNativePersonInputAuthority(ctx context.Context, who auth.Identity, request Request, report v2input.Ref) (nativePersonInputAuthority, error) {
	return s.readNativePersonInputAuthorityCandidate(ctx, who, request, report, nil)
}

// Explicit recovery locators are requalified physically; callers must match the
// complete retained proof or persisted authority SHA before using the result.
func (s SQLTransferSourceReader) readNativePersonInputAuthorityCandidate(ctx context.Context, who auth.Identity, request Request, report v2input.Ref, exact *personCandidate) (nativePersonInputAuthority, error) {
	var zero nativePersonInputAuthority
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.ProjectID == uuid.Nil || s.Bucket == "" || who.UserID != request.ActorID || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	policies, err := selectNativeQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	expected := policies["mask.person"]
	selectCandidate := func() (personCandidate, error) {
		if exact == nil {
			return s.personCandidate(ctx, who, request.TenantID, request.Intent.Reference, expected)
		}
		if exact.root.ArtifactID == uuid.Nil || exact.pixels.ArtifactID == uuid.Nil || exact.root == exact.pixels {
			return personCandidate{}, v2input.ErrInvalid
		}
		return *exact, nil
	}
	candidate, err := selectCandidate()
	if err != nil {
		return zero, err
	}
	person, err := s.ReadQualifiedPersonEvidence(ctx, who, request.TenantID, request.Intent.Reference, candidate.root, report, expected)
	if err != nil {
		return zero, err
	}
	if person.Root != candidate.root || person.Pixels != candidate.pixels {
		return zero, v2input.ErrConflict
	}
	evidence, err := s.personEvidenceObjects(ctx, who, request.TenantID, request.Intent.Reference, person)
	if err != nil {
		return zero, err
	}
	qualification, err := s.personQualificationObject(ctx, who, request.TenantID, expected, report)
	if err != nil {
		return zero, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	nodes := [2]v2draft.LayerRef{{Ref: person.Root, Contract: "MaskRef", CanvasHash: request.Intent.Reference.CanvasHash}, {Ref: person.Pixels, Contract: "MaskPixels", ParentRef: person.Root}}
	var objects [2]objectstore.Object
	for i, node := range nodes {
		objects[i], err = catalog.Lookup(ctx, who, request.TenantID, node)
		if err != nil {
			return zero, err
		}
	}
	// Re-read verified bytes after observing the retained pins, then all catalogs.
	after, err := s.ReadQualifiedPersonEvidence(ctx, who, request.TenantID, request.Intent.Reference, candidate.root, report, expected)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(after, person) {
		return zero, v2input.ErrConflict
	}
	currentEvidence, err := s.personEvidenceObjects(ctx, who, request.TenantID, request.Intent.Reference, person)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(currentEvidence, evidence) {
		return zero, v2input.ErrConflict
	}
	currentQualification, err := s.personQualificationObject(ctx, who, request.TenantID, expected, report)
	if err != nil {
		return zero, err
	}
	if currentQualification != qualification {
		return zero, v2input.ErrConflict
	}
	currentCandidate, err := selectCandidate()
	if err != nil {
		return zero, err
	}
	if currentCandidate != candidate {
		return zero, v2input.ErrConflict
	}
	for i, node := range nodes {
		current, err := catalog.Lookup(ctx, who, request.TenantID, node)
		if err != nil {
			return zero, err
		}
		if current != objects[i] {
			return zero, v2input.ErrConflict
		}
	}
	if err := s.recheckNativePlanPolicy(ctx, who, request.TenantID, policy); err != nil {
		return zero, err
	}
	expectedRaw, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	evidenceRaw, err := marshalPersonEvidencePins(evidence, s.Bucket)
	if err != nil {
		return zero, err
	}
	qualificationRaw, err := canonical.Marshal(personEvidencePin{qualification.contract, qualification.ref.ArtifactID, qualification.ref.SHA256, s.Bucket, qualification.object.Key, qualification.object.VersionID, qualification.object.Bytes, qualification.backend})
	if err != nil {
		return zero, err
	}
	proof := nativePersonInputAuthority{tenant: request.TenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, binding: request.Intent.Reference, root: person.Root, pixels: person.Pixels, report: report, body: string(person.Body), objects: objects, policy: policy, expected: string(expectedRaw), evidence: string(evidenceRaw), qualification: string(qualificationRaw)}
	proof.seal, err = nativePersonInputAuthorityDigest(proof)
	if err != nil {
		return zero, err
	}
	if err := s.validateNativePersonInputAuthority(ctx, who, request, proof); err != nil {
		return zero, err
	}
	return proof, nil
}

// SQL-only original authority locks. No fabricated protection/coverage proof,
// object IO, publication, queue transition or parent admission occurs here.
func (s SQLTransferSourceReader) lockNativePersonInputAuthority(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, p nativePersonInputAuthority) error {
	if err := s.validateNativePersonInputAuthority(ctx, who, request, p); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	query := func(sql string, args ...any) error {
		var ready bool
		if err := tx.QueryRow(ctx, sql, args...).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
		return ctx.Err()
	}
	if err := query("SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", p.tenant, p.project, p.actor, p.session, p.policy.body); err != nil {
		return err
	}
	if err := query("SELECT photo.lock_semantic_proposal_person_inventory($1,$2,$3,$4)", p.tenant, p.project, p.actor, p.session); err != nil {
		return err
	}
	b := p.binding
	if err := query("SELECT photo.lock_semantic_proposal_person_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", p.tenant, p.project, p.actor, p.session, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, p.root.ArtifactID, p.root.SHA256, p.pixels.ArtifactID, p.pixels.SHA256, p.expected, p.evidence); err != nil {
		return err
	}
	if err := query("SELECT photo.lock_semantic_proposal_person_qualification($1,$2,$3,$4,$5,$6)", p.tenant, p.project, p.actor, p.session, p.expected, p.qualification); err != nil {
		return err
	}
	pins := make([]map[string]any, 2)
	for i, ref := range []v2input.Ref{p.root, p.pixels} {
		obj := p.objects[i]
		pins[i] = map[string]any{"artifact_id": ref.ArtifactID, "sha256": ref.SHA256, "bucket": p.bucket, "key": obj.Key, "version": obj.VersionID, "bytes": obj.Bytes, "media_type": obj.ContentType}
	}
	raw, err := canonical.Marshal(pins)
	if err != nil {
		return err
	}
	if err := query("SELECT photo.lock_semantic_native_person_input_objects($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", p.tenant, p.project, p.actor, p.session, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, p.root.ArtifactID, p.root.SHA256, p.pixels.ArtifactID, p.pixels.SHA256, p.expected, string(raw)); err != nil {
		return err
	}
	return query("SELECT photo.semantic_proposal_person_candidate_matches($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", p.tenant, p.project, p.actor, p.session, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, p.root.ArtifactID, p.root.SHA256, p.pixels.ArtifactID, p.pixels.SHA256, p.expected)
}
