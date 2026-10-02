package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

var ErrInvalidTrainingDraft = errors.New("INVALID_TRAINING_DRAFT")
var ErrTrainingDraftConflict = errors.New("TRAINING_DRAFT_CONFLICT")

type trainingDraftConfig struct {
	ProjectID uuid.UUID `json:"project_id"`
	Training  struct {
		SchemaVersion    int       `json:"schema_version"`
		ProfileID        uuid.UUID `json:"profile_id"`
		ExpectedRevision string    `json:"expected_revision"`
		ManifestSHA      string    `json:"manifest_sha"`
		SnapshotDigest   string    `json:"snapshot_digest"`
		SnapshotBodySHA  string    `json:"snapshot_body_sha"`
	} `json:"v23_training"`
}

type trainingDraftRecovery struct {
	SchemaVersion    int       `json:"schema_version"`
	ManifestJCS      string    `json:"manifest_jcs"`
	ManifestSHA      string    `json:"manifest_sha"`
	SnapshotJCS      string    `json:"snapshot_jcs"`
	SnapshotBodySHA  string    `json:"snapshot_body_sha"`
	SnapshotDigest   string    `json:"snapshot_digest"`
	PolicyJCS        string    `json:"policy_jcs"`
	PolicySHA        string    `json:"policy_sha"`
	FrozenJCS        string    `json:"frozen_jcs,omitempty"`
	ReservedBytes    string    `json:"reserved_bytes"`
	ExpectedRevision string    `json:"expected_revision"`
	ProfileID        uuid.UUID `json:"profile_id"`
	ProjectID        uuid.UUID `json:"project_id"`
}

type TrainingDraftPrepared struct {
	TenantID, ActorID, AuthSessionID, ProfileID, ProjectID uuid.UUID
	RunID, WorkflowID, SnapshotID                          uuid.UUID
	ExpectedRevision                                       int64
	Key                                                    string
	RequestSHA, PolicySHA, ManifestSHA                     string
	SnapshotBodySHA, SnapshotDigest, ConfigSHA             string
	ReservedBytes                                          int64
	Manifest                                               TrainingManifest
	ManifestBody, SnapshotBody, ConfigBody                 []byte
	RecoveryBody                                           []byte
}

// PrepareTrainingDraft uses already compiled immutable RenderCase references.
// It never reads mutable correction rows; the trusted caller performs that
// compilation before admission and FreezePublishedTrainingSnapshot rechecks it.
func PrepareTrainingDraft(tenantID, actorID, authSessionID, projectID,
	profileID uuid.UUID, key string, reservedBytes int64,
	request TrainCreate, manifest TrainingManifest) (TrainingDraftPrepared, error) {
	var empty TrainingDraftPrepared
	revision, err := ValidateTrainCreate(request)
	if err != nil || tenantID == uuid.Nil || actorID == uuid.Nil ||
		authSessionID == uuid.Nil || projectID == uuid.Nil ||
		profileID == uuid.Nil || reservedBytes < 1 ||
		manifest.SchemaVersion != 1 || manifest.ProfileID != profileID ||
		manifest.Mode != request.Policy.Mode ||
		manifest.Calibration != request.Policy.Calibration ||
		!validateDraftManifest(manifest) {
		return empty, ErrInvalidTrainingDraft
	}
	selected := make(map[uuid.UUID]struct{}, len(request.SessionIDs))
	for _, id := range request.SessionIDs {
		selected[id] = struct{}{}
	}
	for _, example := range manifest.Examples {
		if _, okay := selected[example.SessionID]; !okay {
			return empty, ErrInvalidTrainingDraft
		}
		if manifest.Mode == "photographer" &&
			example.SubjectGroupID != projectID {
			return empty, ErrInvalidTrainingDraft
		}
	}
	runID, err := v2input.RunID(tenantID, actorID, key)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	workflowID, err := v2input.WorkflowID(runID)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	policyBody, err := canonical.Marshal(request.Policy)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	policySHA, err := canonical.Hash(request.Policy)
	if err != nil || policySHA != manifest.PolicySHA {
		return empty, ErrInvalidTrainingDraft
	}
	manifestBody, err := canonical.Marshal(manifest)
	if err != nil || len(manifestBody) < 2 || len(manifestBody) > 8388608 {
		return empty, ErrInvalidTrainingDraft
	}
	manifestDigest := sha256.Sum256(manifestBody)
	manifestSHA := hex.EncodeToString(manifestDigest[:])
	snapshot, snapshotBody, snapshotBodySHA, err :=
		CompileTrainingArtifactSnapshot(runID, projectID, profileID, manifestSHA)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	config := trainingDraftConfig{ProjectID: projectID}
	config.Training.SchemaVersion = 1
	config.Training.ProfileID = profileID
	config.Training.ExpectedRevision = strconv.FormatInt(revision, 10)
	config.Training.ManifestSHA = manifestSHA
	config.Training.SnapshotDigest = snapshot.SnapshotDigest
	config.Training.SnapshotBodySHA = snapshotBodySHA
	configBody, err := canonical.Marshal(config)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	configSHA, err := canonical.Hash(config)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	requestSHA, err := canonical.Hash(request)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	recovery := trainingDraftRecovery{SchemaVersion: 1,
		ManifestJCS: string(manifestBody), ManifestSHA: manifestSHA,
		SnapshotJCS: string(snapshotBody), SnapshotBodySHA: snapshotBodySHA,
		SnapshotDigest: snapshot.SnapshotDigest, PolicyJCS: string(policyBody),
		PolicySHA: policySHA, ReservedBytes: strconv.FormatInt(reservedBytes, 10),
		ExpectedRevision: strconv.FormatInt(revision, 10), ProfileID: profileID,
		ProjectID: projectID}
	recoveryBody, err := canonical.Marshal(recovery)
	if err != nil || len(recoveryBody) > 16777216 {
		return empty, ErrInvalidTrainingDraft
	}
	return TrainingDraftPrepared{TenantID: tenantID, ActorID: actorID,
		AuthSessionID: authSessionID, ProfileID: profileID,
		ProjectID: projectID, RunID: runID, WorkflowID: workflowID,
		SnapshotID: snapshot.SnapshotID, ExpectedRevision: revision,
		Key: key, RequestSHA: requestSHA, PolicySHA: policySHA,
		ManifestSHA: manifestSHA, SnapshotBodySHA: snapshotBodySHA,
		SnapshotDigest: snapshot.SnapshotDigest, ConfigSHA: configSHA,
		ReservedBytes: reservedBytes, Manifest: manifest,
		ManifestBody: manifestBody, SnapshotBody: snapshotBody,
		ConfigBody: configBody, RecoveryBody: recoveryBody}, nil
}

// BeginTrainingDraftInTransaction creates the draft and persists recovery
// bytes atomically. Commit this transaction before publishing either object.
func BeginTrainingDraftInTransaction(ctx context.Context, tx pgx.Tx,
	draft TrainingDraftPrepared) (string, error) {
	if tx == nil || draft.RunID == uuid.Nil || draft.WorkflowID == uuid.Nil ||
		draft.SnapshotID == uuid.Nil || len(draft.RecoveryBody) < 2 ||
		len(draft.ConfigBody) < 2 || !validSHA(draft.RequestSHA) {
		return "", ErrInvalidTrainingDraft
	}
	var outcome string
	var run, snapshot *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT outcome,run_id,snapshot_id FROM
	 photo.begin_v23_training_draft($1,$2,$3,$4,$5,$6,$7,$8,$9,
	 $10::jsonb,$11,$12,$13)`, draft.TenantID, draft.RunID,
		draft.WorkflowID, draft.ProfileID, draft.ExpectedRevision,
		draft.ActorID, draft.AuthSessionID, draft.PolicySHA, draft.ConfigSHA,
		string(draft.ConfigBody), draft.ReservedBytes, draft.Key,
		draft.RequestSHA).Scan(&outcome, &run, &snapshot)
	if err != nil {
		return "", err
	}
	if outcome == "conflict" || outcome == "expired" {
		return outcome, ErrTrainingDraftConflict
	}
	if run == nil || snapshot == nil || *run != draft.RunID ||
		*snapshot != draft.SnapshotID {
		return "", ErrTrainingDraftConflict
	}
	if outcome == "replayed" {
		return outcome, nil
	}
	if outcome != "created" && outcome != "pending" {
		return "", ErrTrainingDraftConflict
	}
	var saved bool
	err = tx.QueryRow(ctx, `SELECT photo.save_v23_training_recovery(
	 $1,$2,$3,$4,$5,$6,$7::jsonb)`, draft.TenantID, draft.ActorID,
		draft.AuthSessionID, draft.RunID, draft.Key, draft.RequestSHA,
		string(draft.RecoveryBody)).Scan(&saved)
	if err != nil {
		return "", err
	}
	if !saved {
		return "", ErrTrainingDraftConflict
	}
	return outcome, nil
}

type TrainingDraftProbe struct {
	Outcome           string
	RunID, WorkflowID uuid.UUID
	Recovery          json.RawMessage
}

// ProbeTrainingDraft lets an API replay a sealed ticket or resume the exact
// pending compiler bytes before touching live candidate inventory.
func ProbeTrainingDraft(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tenantID, actorID, authSessionID uuid.UUID, key string,
	request TrainCreate) (TrainingDraftProbe, error) {
	var empty TrainingDraftProbe
	if db == nil || authSessionID == uuid.Nil {
		return empty, ErrInvalidTrainingDraft
	}
	if _, err := ValidateTrainCreate(request); err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	runID, err := v2input.RunID(tenantID, actorID, key)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	requestSHA, err := canonical.Hash(request)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	var outcome string
	var run, workflow *uuid.UUID
	var recovery json.RawMessage
	err = db.QueryRow(ctx, `SELECT outcome,run_id,workflow_id,recovery FROM
	 photo.v23_training_replay($1,$2,$3,$4,$5,$6)`, tenantID,
		actorID, authSessionID, runID, key, requestSHA).
		Scan(&outcome, &run, &workflow, &recovery)
	if err != nil {
		return empty, err
	}
	if outcome != "missing" && outcome != "pending" && outcome != "replayed" &&
		outcome != "conflict" && outcome != "expired" {
		return empty, ErrInvalidTrainingDraft
	}
	if outcome == "pending" || outcome == "replayed" {
		if run == nil || workflow == nil || *run != runID ||
			*workflow != uuid.NewSHA1(runID, []byte("workflow")) {
			return empty, ErrInvalidTrainingDraft
		}
	}
	probe := TrainingDraftProbe{Outcome: outcome, RunID: runID}
	if workflow != nil {
		probe.WorkflowID = *workflow
	}
	if outcome == "pending" {
		if !json.Valid(recovery) {
			return empty, ErrInvalidTrainingDraft
		}
		probe.Recovery = recovery
	}
	return probe, nil
}

// ResumeTrainingDraft reconstructs exact transport bytes from the pending
// idempotency record. It never queries correction events or consent grants;
// those are revalidated only when freezing the already published manifest.
func ResumeTrainingDraft(tenantID, actorID, authSessionID, projectID,
	profileID uuid.UUID, key string, request TrainCreate,
	recoveryBody json.RawMessage) (TrainingDraftPrepared, error) {
	var empty TrainingDraftPrepared
	if len(recoveryBody) < 2 || len(recoveryBody) > 33554432 {
		return empty, ErrInvalidTrainingDraft
	}
	var saved trainingDraftRecovery
	if err := json.Unmarshal(recoveryBody, &saved); err != nil ||
		saved.SchemaVersion != 1 || saved.ProjectID != projectID ||
		saved.ProfileID != profileID || !validSHA(saved.ManifestSHA) ||
		!validSHA(saved.SnapshotBodySHA) || !validSHA(saved.SnapshotDigest) ||
		!validSHA(saved.PolicySHA) {
		return empty, ErrInvalidTrainingDraft
	}
	bytes, err := strconv.ParseInt(saved.ReservedBytes, 10, 64)
	if err != nil || bytes < 1 || saved.ExpectedRevision != request.ExpectedRevision {
		return empty, ErrInvalidTrainingDraft
	}
	var manifest TrainingManifest
	if json.Unmarshal([]byte(saved.ManifestJCS), &manifest) != nil {
		return empty, ErrInvalidTrainingDraft
	}
	prepared, err := PrepareTrainingDraft(tenantID, actorID, authSessionID,
		projectID, profileID, key, bytes, request, manifest)
	if err != nil {
		return empty, err
	}
	if saved.FrozenJCS != "" {
		if _, err = frozenRecipesFromRecovery(prepared,
			[]byte(saved.FrozenJCS)); err != nil {
			return empty, err
		}
		var expected trainingDraftRecovery
		if json.Unmarshal(prepared.RecoveryBody, &expected) != nil {
			return empty, ErrInvalidTrainingDraft
		}
		expected.FrozenJCS = saved.FrozenJCS
		prepared.RecoveryBody, err = canonical.Marshal(expected)
		if err != nil || len(prepared.RecoveryBody) > 33554432 {
			return empty, ErrInvalidTrainingDraft
		}
	}
	canonicalRecovery, err := canonical.Bytes(recoveryBody)
	if err != nil || string(canonicalRecovery) != string(prepared.RecoveryBody) ||
		string(prepared.ManifestBody) != saved.ManifestJCS ||
		string(prepared.SnapshotBody) != saved.SnapshotJCS ||
		prepared.ManifestSHA != saved.ManifestSHA ||
		prepared.SnapshotBodySHA != saved.SnapshotBodySHA ||
		prepared.SnapshotDigest != saved.SnapshotDigest ||
		prepared.PolicySHA != saved.PolicySHA {
		return empty, ErrInvalidTrainingDraft
	}
	return prepared, nil
}
