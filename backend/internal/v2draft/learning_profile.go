package v2draft

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

const zeroTrainingSHA = "0000000000000000000000000000000000000000000000000000000000000000"

type EligibleLearningModel struct {
	Profile      CandidateProfile
	ModelRef     v2input.Ref
	ManifestRef  v2input.Ref
	ICCLockSHA   string
	ModelLockSHA string
}

// ReadEligibleLearningProfile pins an active version's exact model bytes and
// verifies their TrainingManifest provenance. Approval still repeats the
// eligibility and live-consent check under the tenant lock at commit time.
func (s Service) ReadEligibleLearningProfile(ctx context.Context,
	who auth.Identity, tenantID, projectID, documentID,
	versionID uuid.UUID) (EligibleLearningModel, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return EligibleLearningModel{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if projectID == uuid.Nil || documentID == uuid.Nil || versionID == uuid.Nil {
		return EligibleLearningModel{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	var profileID uuid.UUID
	var modelRef v2input.Ref
	var model objectstore.Object
	var trainingRef v2input.Ref
	var training objectstore.Object
	var modelBucket, trainingBucket string
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			if _, e := project.Authorize(ctx, tx, tenantID, projectID,
				who.UserID, role, domain.Editor, false); e != nil {
				return e
			}
			e := tx.QueryRow(ctx, `SELECT profile.id,model.id,model.sha256,
			 model.bytes,model.bucket,model.object_key,model.object_version,
			 model.media_type,training.id,training.sha256,training.bytes,
			 training.bucket,training.object_key,training.object_version,
			 training.media_type
			 FROM photo.model_use_links used
			 JOIN photo.learning_versions version ON version.tenant_id=used.tenant_id
			  AND version.project_id=used.project_id AND version.id=used.version_id
			 JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
			  AND profile.project_id=version.project_id AND profile.id=version.profile_id
			 JOIN photo.version_eligibility state ON state.tenant_id=version.tenant_id
			  AND state.project_id=version.project_id AND state.version_id=version.id
			 JOIN photo.artifacts model ON model.tenant_id=version.tenant_id
			  AND model.id=version.model_artifact_id
			 JOIN photo.artifacts training ON training.tenant_id=version.tenant_id
			  AND training.id=version.training_artifact_id
			 WHERE used.tenant_id=$1 AND used.project_id=$2
			  AND used.document_id=$3 AND used.version_id=$4
			  AND state.status='eligible' AND profile.active_version_id=version.id
			  AND model.state='ready' AND model.output_role='document'
			  AND model.verified_at IS NOT NULL AND model.object_version IS NOT NULL
			  AND model.object_version NOT IN('','null')
			  AND (model.expires_at IS NULL OR model.expires_at>clock_timestamp())
			  AND training.state='ready' AND training.output_role IN('document','v23_training_manifest')
			  AND training.verified_at IS NOT NULL AND training.object_version IS NOT NULL
			  AND training.object_version NOT IN('','null')
			  AND (training.expires_at IS NULL OR training.expires_at>clock_timestamp())
			  AND EXISTS(SELECT 1 FROM photo.learning_example_links link
			   WHERE link.tenant_id=version.tenant_id AND link.version_id=version.id)
			  AND NOT EXISTS(SELECT 1 FROM photo.learning_example_links link
			   LEFT JOIN photo.consent_grants grant_row ON grant_row.tenant_id=link.tenant_id
			    AND grant_row.project_id=link.project_id AND grant_row.id=link.consent_id
			   WHERE link.tenant_id=version.tenant_id AND link.version_id=version.id
			    AND (grant_row.id IS NULL OR grant_row.status<>'granted'
			     OR grant_row.epoch<>link.consent_epoch
			     OR grant_row.purpose<>CASE WHEN profile.person_id IS NULL THEN
			      'learning_photographer' ELSE 'learning_person' END
			     OR grant_row.person_id IS DISTINCT FROM profile.person_id))
			  AND NOT EXISTS(SELECT 1 FROM photo.project_persons person
			   WHERE person.tenant_id=profile.tenant_id
			    AND person.project_id=profile.project_id AND person.id=profile.person_id
			    AND person.state<>'active')`,
				tenantID, projectID, documentID, versionID).Scan(&profileID,
				&modelRef.ArtifactID, &modelRef.SHA256, &model.Bytes,
				&modelBucket, &model.Key, &model.VersionID, &model.ContentType,
				&trainingRef.ArtifactID, &trainingRef.SHA256, &training.Bytes,
				&trainingBucket, &training.Key, &training.VersionID,
				&training.ContentType)
			if errors.Is(e, pgx.ErrNoRows) {
				return domain.NewError("REVISION_CONFLICT", 409)
			}
			return e
		})
	if err != nil {
		return EligibleLearningModel{}, draftMutationError(err)
	}
	if modelBucket != s.Bucket || trainingBucket != s.Bucket ||
		!isDocumentRef(modelRef) || !isDocumentRef(trainingRef) {
		return EligibleLearningModel{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	training.SHA256 = trainingRef.SHA256
	manifestBody, err := readJSONArtifact(ctx, s.Objects, training, trainingRef)
	if err != nil {
		return EligibleLearningModel{}, draftMutationError(err)
	}
	modelLock, iccLock, okay := modelTrainingLocks(manifestBody, profileID)
	if !okay {
		return EligibleLearningModel{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	model.SHA256 = modelRef.SHA256
	body, err := readJSONArtifact(ctx, s.Objects, model, modelRef)
	if err != nil {
		return EligibleLearningModel{}, draftMutationError(err)
	}
	profile, err := candidateFromModel(body, profileID, trainingRef.SHA256)
	if err != nil {
		return EligibleLearningModel{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	return EligibleLearningModel{Profile: profile, ModelRef: modelRef,
		ManifestRef: trainingRef, ICCLockSHA: iccLock,
		ModelLockSHA: modelLock}, nil
}

func modelTrainingLocks(body []byte, profileID uuid.UUID) (string, string, bool) {
	var manifest struct {
		SchemaVersion   int               `json:"schema_version"`
		ProfileID       uuid.UUID         `json:"profile_id"`
		RendererVersion string            `json:"renderer_version"`
		ICCLockSHA      string            `json:"icc_lock_sha"`
		ModelLockSHA    string            `json:"model_lock_sha"`
		Examples        []json.RawMessage `json:"examples"`
		Consents        []json.RawMessage `json:"consents"`
	}
	okay := json.Unmarshal(body, &manifest) == nil &&
		manifest.SchemaVersion == 1 && manifest.ProfileID == profileID &&
		manifest.RendererVersion == "v2.2" &&
		correctionDigest(manifest.ICCLockSHA) &&
		correctionDigest(manifest.ModelLockSHA) &&
		len(manifest.Examples) >= 40 && len(manifest.Examples) <= 5000 &&
		len(manifest.Consents) >= 1 && len(manifest.Consents) <= 5000
	return manifest.ModelLockSHA, manifest.ICCLockSHA, okay
}

func candidateFromModel(body []byte, profileID uuid.UUID,
	trainingSHA string) (CandidateProfile, error) {
	profile, err := DecodeCandidateProfile(body)
	if err != nil || profile.ProfileID != profileID ||
		profile.TrainingSHA != trainingSHA || trainingSHA == zeroTrainingSHA {
		return CandidateProfile{}, v2input.ErrInvalid
	}
	return profile, nil
}
