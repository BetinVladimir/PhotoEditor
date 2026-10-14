package provenance

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2learning"
	"strings"
)

type signingRenderModelOrigin struct {
	Profile      uuid.UUID
	TrainingSHA  string
	ModelLockSHA string
	ICCLockSHA   string
	ObjectDigest string
}
type signingRenderModelOriginReader interface {
	read(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2draft.FrozenRenderLearning) (signingRenderModelOrigin, error)
}

// Validate the physical CandidateProfile witness against its adopted version's
// immutable training identity. No current active-pointer/consent grant is inferred.
func verifySigningRenderModelOrigins(ctx context.Context, reader signingRenderModelOriginReader, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, evidence SigningRenderClosureEvidence, expectedModel, expectedICC string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(evidence.inventory.Entries) > 4096 {
		return ErrInvalid
	}
	models := map[uuid.UUID]v2input.Ref{}
	seen := map[uuid.UUID]bool{}
	for _, entry := range evidence.inventory.Entries {
		if entry.Contract != "CandidateProfile" {
			continue
		}
		if !ref(entry.Ref) {
			return ErrInvalid
		}
		if _, exists := models[entry.Ref.ArtifactID]; exists {
			return ErrInvalid
		}
		models[entry.Ref.ArtifactID] = entry.Ref
	}
	modelDocuments := map[uuid.UUID]v2draft.DocumentWitness{}
	for _, document := range evidence.inventory.Documents {
		if _, exists := models[document.Ref.ArtifactID]; !exists {
			continue
		}
		if document.Ref != models[document.Ref.ArtifactID] || (document.Contract != "CandidateProfile" && document.Contract != "Artifact") {
			return ErrInvalid
		}
		if prior, exists := modelDocuments[document.Ref.ArtifactID]; exists {
			if prior.JCS != document.JCS || prior.Object != document.Object {
				return ErrInvalid
			}
		} else {
			modelDocuments[document.Ref.ArtifactID] = document
		}
	}
	for _, occurrence := range evidence.inventory.Occurrences {
		node := occurrence.Node
		if node.Contract != "CandidateProfile" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !digest(expectedModel) || !digest(expectedICC) || reader == nil || node.ExpectedLearning == nil || node.Ref != node.ExpectedLearning.Model {
			return ErrInvalid
		}
		if models[node.Ref.ArtifactID] != node.Ref {
			return ErrInvalid
		}
		seen[node.Ref.ArtifactID] = true
		learning := *node.ExpectedLearning
		document, exists := modelDocuments[node.Ref.ArtifactID]
		if !exists {
			return ErrInvalid
		}
		raw := []byte(document.JCS)
		if !pinned(raw, node.Ref, 1<<20) {
			return ErrInvalid
		}
		candidate, err := v2draft.DecodeCandidateProfile(raw)
		if err != nil {
			return ErrInvalid
		}
		expected, err := reader.read(ctx, who, ready, task, learning)
		if err != nil {
			return err
		}
		if expected.Profile == uuid.Nil || !digest(expected.TrainingSHA) || expected.TrainingSHA == strings.Repeat("0", 64) || candidate.ProfileID != expected.Profile || candidate.TrainingSHA != expected.TrainingSHA || expected.ModelLockSHA != expectedModel || expected.ICCLockSHA != expectedICC {
			return ErrInvalid
		}
		current, err := reader.read(ctx, who, ready, task, learning)
		if err != nil {
			return err
		}
		if current != expected {
			return ErrInvalid
		}
	}
	if len(seen) != len(models) {
		return ErrInvalid
	}
	for _, document := range evidence.inventory.Documents {
		if document.Contract == "CandidateProfile" && models[document.Ref.ArtifactID] != document.Ref {
			return ErrInvalid
		}
	}
	return ctx.Err()
}

type sqlSigningRenderModelOriginReader struct {
	DB      *pgxpool.Pool
	Objects SigningObjects
	Bucket  string
}

func (s sqlSigningRenderModelOriginReader) read(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, learning v2draft.FrozenRenderLearning) (signingRenderModelOrigin, error) {
	var origin signingRenderModelOrigin
	var training, evaluation objectstore.Object
	var trainingBucket, evaluationBucket string
	if s.DB == nil || s.Objects == nil || len(s.Bucket) < 3 || learning.VersionID == uuid.Nil || !ref(learning.Model) {
		return origin, ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, err := project.Authorize(ctx, tx, ready.Tenant, ready.Project, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT version.profile_id,training.sha256,
 training.bucket,training.object_key,training.object_version,training.bytes,training.media_type,
 evaluation.bucket,evaluation.object_key,evaluation.object_version,evaluation.bytes,evaluation.media_type,evaluation.sha256
 FROM photo.learning_versions version
 JOIN photo.artifacts training ON training.tenant_id=version.tenant_id AND training.id=version.training_artifact_id
 JOIN photo.artifacts evaluation ON evaluation.tenant_id=version.tenant_id AND evaluation.id=version.evaluation_artifact_id
   WHERE version.tenant_id=$1 AND version.project_id=$2 AND version.id=$3 AND (`+signingHistoricalLearningBindingSQL+`)`, ready.Tenant, ready.Project, learning.VersionID, learning.Model.ArtifactID, learning.Model.SHA256, task.Task).Scan(&origin.Profile, &origin.TrainingSHA, &trainingBucket, &training.Key, &training.VersionID, &training.Bytes, &training.ContentType, &evaluationBucket, &evaluation.Key, &evaluation.VersionID, &evaluation.Bytes, &evaluation.ContentType, &evaluation.SHA256)
		if err != nil {
			return err
		}
		_, err = project.Authorize(ctx, tx, ready.Tenant, ready.Project, who.UserID, role, domain.Editor, false)
		return err
	})
	if err != nil {
		return signingRenderModelOrigin{}, err
	}
	training.SHA256 = origin.TrainingSHA
	manifest, manifestSHA, err := v2learning.ReadFrozenEvaluationManifest(ctx, s.Objects, training, trainingBucket, s.Bucket)
	if err != nil {
		return signingRenderModelOrigin{}, err
	}
	if manifest.ProfileID != origin.Profile || manifestSHA != origin.TrainingSHA || !digest(manifest.ModelLockSHA) || !digest(manifest.ICCLockSHA) {
		return signingRenderModelOrigin{}, ErrInvalid
	}
	if evaluationBucket != s.Bucket {
		return signingRenderModelOrigin{}, ErrInvalid
	}
	raw, err := readSigningLearningJSON(ctx, s.Objects, evaluation)
	if err != nil {
		return signingRenderModelOrigin{}, err
	}
	if _, err := v2learning.VerifyEvaluationEvidence(raw, evaluation.SHA256, manifest, manifestSHA, learning.Model.SHA256); err != nil {
		return signingRenderModelOrigin{}, ErrInvalid
	}
	origin.ObjectDigest, err = canonical.Hash([]any{trainingBucket, training, evaluationBucket, evaluation})
	if err != nil {
		return signingRenderModelOrigin{}, err
	}
	origin.ModelLockSHA = manifest.ModelLockSHA
	origin.ICCLockSHA = manifest.ICCLockSHA
	return origin, nil
}

func readSigningLearningJSON(ctx context.Context, objects SigningObjects, object objectstore.Object) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if objects == nil || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.ContentType != "application/json" || object.Bytes < 2 || object.Bytes > 8<<20 || !digest(object.SHA256) {
		return nil, ErrInvalid
	}
	stream, err := objects.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return nil, err
	}
	if stream == nil {
		return nil, ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(signingContextReader{ctx: ctx, reader: stream}, object.Bytes+1))
	closeErr := stream.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(raw)) != object.Bytes || bodyDigest(raw) != object.SHA256 {
		return nil, ErrInvalid
	}
	return raw, nil
}
