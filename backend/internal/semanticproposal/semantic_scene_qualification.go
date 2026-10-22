package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Independent operator-selected qualification for one actual analysis producer.
// A transfer qualification does not imply either producer qualification.
type TransferSceneProducer struct {
	Report   v2input.Ref
	Expected v2qualification.Expected
	Costs    v2input.SemanticQualifiedCosts
}

type semanticSceneProducerProof struct {
	tenant, project, actor, session uuid.UUID
	expected                        string
	object                          personEvidenceObject
}

func validSceneProducerOperation(operation string) bool {
	return operation == "scene.parse" || operation == "depth.predict"
}

func (s SQLTransferSourceReader) sceneProducerQualificationObject(ctx context.Context, who auth.Identity, tid uuid.UUID, configuration TransferSceneProducer) (personEvidenceObject, error) {
	var zero personEvidenceObject
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || s.ProjectID == uuid.Nil || !validSceneProducerOperation(configuration.Expected.Operation) || configuration.Expected.Version != "1" || (configuration.Expected.Backend != "cuda" && configuration.Expected.Backend != "mps") {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(configuration.Expected)
	if err != nil {
		return zero, err
	}
	var result personEvidenceObject
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_scene_qualification_object($1,$2,$3,$4,$5)", tid, s.ProjectID, who.UserID, who.SessionID, body)
		if err != nil {
			return err
		}
		defer rows.Close()
		found := false
		for rows.Next() {
			var entry personEvidenceObject
			var bucket string
			if err := rows.Scan(&entry.ref.ArtifactID, &entry.ref.SHA256, &bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes); err != nil {
				return err
			}
			if entry.ref != configuration.Report {
				continue
			}
			if found || bucket != s.Bucket {
				return v2qualification.ErrUnqualified
			}
			entry.contract = "QualificationReport"
			entry.backend = configuration.Expected.Backend
			entry.object.SHA256 = entry.ref.SHA256
			entry.object.ContentType = "application/json"
			result = entry
			found = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !found {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return result, nil
}

func (s SQLTransferSourceReader) readSceneProducerQualification(ctx context.Context, who auth.Identity, tid uuid.UUID, configuration TransferSceneProducer) (semanticSceneProducerProof, error) {
	var zero semanticSceneProducerProof
	configuration.Expected = ownQualificationExpected(configuration.Expected)
	object, err := s.sceneProducerQualificationObject(ctx, who, tid, configuration)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object.object, object.ref)
	if err != nil {
		return zero, err
	}
	report, err := v2qualification.VerifyTyped(raw, object.ref.SHA256, configuration.Expected)
	if err != nil {
		return zero, err
	}
	if err := validateTransferResourceCosts(configuration.Costs, report.Resources()); err != nil {
		return zero, err
	}
	current, err := s.sceneProducerQualificationObject(ctx, who, tid, configuration)
	if err != nil {
		return zero, err
	}
	if current != object {
		return zero, v2input.ErrConflict
	}
	expected, err := canonical.Marshal(configuration.Expected)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return semanticSceneProducerProof{tenant: tid, project: s.ProjectID, actor: who.UserID, session: who.SessionID, expected: string(expected), object: object}, nil
}

func (s SQLTransferSourceReader) lockSceneProducerQualification(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID, proof semanticSceneProducerProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tid == uuid.Nil || proof.tenant != tid || proof.project != s.ProjectID || proof.actor != who.UserID || proof.session != who.SessionID {
		return v2input.ErrInvalid
	}
	var expected v2qualification.Expected
	if json.Unmarshal([]byte(proof.expected), &expected) != nil || !validSceneProducerOperation(expected.Operation) || expected.Version != "1" || (expected.Backend != "cuda" && expected.Backend != "mps") {
		return v2input.ErrInvalid
	}
	canonicalExpected, err := canonical.Marshal(expected)
	if err != nil || string(canonicalExpected) != proof.expected {
		return v2input.ErrInvalid
	}
	object := proof.object
	pin, err := canonical.Marshal(personEvidencePin{object.contract, object.ref.ArtifactID, object.ref.SHA256, s.Bucket, object.object.Key, object.object.VersionID, object.object.Bytes, object.backend})
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_scene_qualification($1,$2,$3,$4,$5,$6)", tid, s.ProjectID, who.UserID, who.SessionID, canonicalExpected, pin).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
