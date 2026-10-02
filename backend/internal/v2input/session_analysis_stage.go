package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

type sessionConsentGrant struct {
	ConsentID uuid.UUID  `json:"consent_id"`
	Epoch     string     `json:"epoch"`
	PersonID  *uuid.UUID `json:"person_id"`
	Purpose   string     `json:"purpose"`
}

type sessionConfirmedFace struct {
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	FaceID         uuid.UUID `json:"face_id"`
	PersonID       uuid.UUID `json:"person_id"`
	ConsentID      uuid.UUID `json:"consent_id"`
	Epoch          string    `json:"epoch"`
}

func validConsentEpoch(value string) bool {
	if len(value) < 1 || len(value) > 19 || value[0] == '0' {
		return false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0 && strconv.FormatInt(parsed, 10) == value
}

// validateSessionConsentSnapshot enforces the relationship that the worker's
// structural schema cannot express: every confirmed face has an active pinned
// identity-matching grant for exactly that person and epoch.
func validateSessionConsentSnapshot(raw []byte) error {
	if len(raw) < 2 || len(raw) > 8388608 {
		return ErrInvalid
	}
	if !exactJSONKeys(raw, "schema_version", "grants", "confirmed_faces") {
		return ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ErrInvalid
	}
	for field, keys := range map[string][]string{
		"grants":          {"consent_id", "epoch", "person_id", "purpose"},
		"confirmed_faces": {"asset_version_id", "face_id", "person_id", "consent_id", "epoch"},
	} {
		var packets []json.RawMessage
		if json.Unmarshal(fields[field], &packets) != nil || packets == nil {
			return ErrInvalid
		}
		for _, packet := range packets {
			if !exactJSONKeys(packet, keys...) {
				return ErrInvalid
			}
		}
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || len(canonicalBody) == 0 || canonicalBody[0] != '{' {
		return ErrInvalid
	}
	var doc struct {
		SchemaVersion  int                    `json:"schema_version"`
		Grants         []sessionConsentGrant  `json:"grants"`
		ConfirmedFaces []sessionConfirmedFace `json:"confirmed_faces"`
	}
	decoder := json.NewDecoder(bytes.NewReader(canonicalBody))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || decoder.Decode(new(any)) != io.EOF ||
		doc.SchemaVersion != 1 || doc.Grants == nil || doc.ConfirmedFaces == nil ||
		len(doc.Grants) > 5000 || len(doc.ConfirmedFaces) > 12800 {
		return ErrInvalid
	}
	grants := make(map[uuid.UUID]sessionConsentGrant, len(doc.Grants))
	for _, grant := range doc.Grants {
		if grant.ConsentID == uuid.Nil || !validConsentEpoch(grant.Epoch) ||
			(grant.PersonID != nil && *grant.PersonID == uuid.Nil) ||
			(grant.Purpose != "identity_matching" && grant.Purpose != "learning_person" &&
				grant.Purpose != "learning_photographer") ||
			(grant.Purpose != "learning_photographer" && grant.PersonID == nil) {
			return ErrInvalid
		}
		if _, exists := grants[grant.ConsentID]; exists {
			return ErrInvalid
		}
		grants[grant.ConsentID] = grant
	}
	faces := make(map[[2]uuid.UUID]bool, len(doc.ConfirmedFaces))
	for _, face := range doc.ConfirmedFaces {
		grant, found := grants[face.ConsentID]
		key := [2]uuid.UUID{face.AssetVersionID, face.FaceID}
		if face.AssetVersionID == uuid.Nil || face.FaceID == uuid.Nil ||
			face.PersonID == uuid.Nil || !validConsentEpoch(face.Epoch) ||
			!found || grant.Purpose != "identity_matching" || grant.PersonID == nil ||
			*grant.PersonID != face.PersonID || grant.Epoch != face.Epoch || faces[key] {
			return ErrInvalid
		}
		faces[key] = true
	}
	return nil
}

// ValidateSessionConsentSnapshot exposes the same strict consent and face
// relationship checks to consumers of an already frozen snapshot.
func ValidateSessionConsentSnapshot(raw []byte) error {
	return validateSessionConsentSnapshot(raw)
}

// SessionDescribeParams is the immutable, shard-local worker contract. The
// snapshot and consent refs are shared across all shards of one admission.
type SessionDescribeParams struct {
	SchemaVersion int    `json:"schema_version"`
	Snapshot      Ref    `json:"snapshot"`
	Ordinals      []int  `json:"ordinals"`
	ModelLockSHA  string `json:"model_lock_sha"`
	Calibration   Ref    `json:"calibration"`
	Consents      Ref    `json:"consents"`
}

type SessionAnalysisRootRefs struct {
	Snapshot       Ref
	SnapshotDigest string
	Context        Ref
	Consents       Ref
}

type sessionManualReference struct {
	AssetVersionID  uuid.UUID   `json:"asset_version_id"`
	PreviousMembers []uuid.UUID `json:"previous_members"`
}

func validateSessionPlanContext(raw []byte, snapshot SessionInputSnapshot) error {
	if len(raw) < 2 || len(raw) > 1048576 {
		return ErrInvalid
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || len(canonicalBody) == 0 || canonicalBody[0] != '{' {
		return ErrInvalid
	}
	var doc struct {
		SchemaVersion        int                      `json:"schema_version"`
		SnapshotID           uuid.UUID                `json:"snapshot_id"`
		SnapshotDigest       string                   `json:"snapshot_digest"`
		ManualReferences     []sessionManualReference `json:"manual_references"`
		ManualLockedOrdinals []int                    `json:"manual_locked_ordinals"`
	}
	decoder := json.NewDecoder(bytes.NewReader(canonicalBody))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || decoder.Decode(new(any)) != io.EOF ||
		doc.SchemaVersion != 1 || doc.SnapshotID != snapshot.SnapshotID ||
		doc.SnapshotDigest != snapshot.SnapshotDigest ||
		doc.ManualReferences == nil || doc.ManualLockedOrdinals == nil ||
		len(doc.ManualReferences) > 5000 || len(doc.ManualLockedOrdinals) > 5000 {
		return ErrInvalid
	}
	selected := make(map[uuid.UUID]bool, len(snapshot.Items))
	for _, item := range snapshot.Items {
		selected[item.Binding.AssetVersionID] = true
	}
	manual := make(map[uuid.UUID]bool, len(doc.ManualReferences))
	for _, choice := range doc.ManualReferences {
		if choice.AssetVersionID == uuid.Nil || !selected[choice.AssetVersionID] ||
			manual[choice.AssetVersionID] || len(choice.PreviousMembers) < 1 ||
			len(choice.PreviousMembers) > 5000 {
			return ErrInvalid
		}
		manual[choice.AssetVersionID] = true
		seen := make(map[uuid.UUID]bool, len(choice.PreviousMembers))
		for _, member := range choice.PreviousMembers {
			if member == uuid.Nil || !selected[member] || seen[member] {
				return ErrInvalid
			}
			seen[member] = true
		}
	}
	for i, ordinal := range doc.ManualLockedOrdinals {
		if ordinal < 0 || ordinal >= len(snapshot.Items) ||
			(i > 0 && ordinal <= doc.ManualLockedOrdinals[i-1]) {
			return ErrInvalid
		}
	}
	return nil
}

// PublishSessionAnalysisRoots runs after begin_v22_analysis_draft commits. The
// frozen snapshot digest and context document SHA must match immutable run
// config; SQL rechecks context SHA when staging and compiling session.plan.
func PublishSessionAnalysisRoots(ctx context.Context, documents DocumentPublisher,
	tenant, run uuid.UUID, items []SessionSnapshotItem, expectedSnapshotDigest string,
	contextBody []byte, expectedContextSHA string, consentsBody []byte) (SessionAnalysisRootRefs, error) {
	if documents == nil || tenant == uuid.Nil || run == uuid.Nil ||
		!digestPattern.MatchString(expectedSnapshotDigest) ||
		!digestPattern.MatchString(expectedContextSHA) {
		return SessionAnalysisRootRefs{}, ErrInvalid
	}
	snapshot, body, err := CompileSessionInputSnapshot(run, items)
	if err != nil || snapshot.SnapshotDigest != expectedSnapshotDigest {
		return SessionAnalysisRootRefs{}, ErrInvalid
	}
	if validateSessionPlanContext(contextBody, snapshot) != nil {
		return SessionAnalysisRootRefs{}, ErrInvalid
	}
	contextSHA, err := canonical.Hash(json.RawMessage(contextBody))
	if err != nil || contextSHA != expectedContextSHA || len(contextBody) > 1048576 {
		return SessionAnalysisRootRefs{}, ErrInvalid
	}
	if validateSessionConsentSnapshot(consentsBody) != nil {
		return SessionAnalysisRootRefs{}, ErrInvalid
	}
	requests := []struct {
		purpose string
		body    []byte
		sha     string
	}{
		{"v2_analysis_input_snapshot", body, ""},
		{"v2_analysis_context", contextBody, expectedContextSHA},
		{"v2_analysis_consents", consentsBody, ""},
	}
	var refs [3]Ref
	for i, request := range requests {
		sha, err := canonical.Hash(json.RawMessage(request.body))
		if err != nil || (request.sha != "" && sha != request.sha) {
			return SessionAnalysisRootRefs{}, ErrInvalid
		}
		published, err := documents.Publish(ctx, artifactjson.Scope{Tenant: tenant,
			Run: run, Owner: run, Purpose: request.purpose}, request.body)
		if err != nil {
			return SessionAnalysisRootRefs{}, err
		}
		refs[i] = Ref{ArtifactID: published.ID, SHA256: published.SHA256}
		if !validRef(refs[i]) || refs[i].SHA256 != sha {
			return SessionAnalysisRootRefs{}, ErrInvalid
		}
	}
	return SessionAnalysisRootRefs{refs[0], snapshot.SnapshotDigest,
		refs[1], refs[2]}, nil
}

type AnalysisDescribeStageTask struct {
	Params      Ref
	InputDigest string
	Profile     json.RawMessage
}

func validSessionDescribeProfile(raw json.RawMessage) bool {
	var profile map[string]json.RawMessage
	if json.Unmarshal(raw, &profile) != nil || profile == nil ||
		string(profile["renderer_version"]) != `"v2.2"` {
		return false
	}
	backend := string(profile["backend"])
	if backend != `"cuda"` && backend != `"cpu"` && backend != `"mps"` {
		return false
	}
	var estimate map[string]json.RawMessage
	if json.Unmarshal(profile["resource_estimate"], &estimate) != nil || len(estimate) != 5 {
		return false
	}
	for _, key := range []string{"ram_bytes", "vram_bytes", "scratch_bytes", "output_bytes", "pixel_count"} {
		var value int64
		if json.Unmarshal(estimate[key], &value) != nil || value < 0 || value > 9999999999999999 {
			return false
		}
		if (key == "ram_bytes" && value < 536870912) ||
			((key == "scratch_bytes" || key == "output_bytes") && value < 1) ||
			(key == "vram_bytes" && backend == `"cuda"` && value < 1073741824) {
			return false
		}
	}
	return true
}

// CompileSessionDescribeParams rejects noncanonical shard ranges before any
// document reservation. SQL later verifies the owner and task UUID again.
func CompileSessionDescribeParams(task SessionDescriptorTask, snapshot, calibration,
	consents Ref, modelLockSHA string) ([]byte, error) {
	if task.ID == uuid.Nil || !validRef(snapshot) || !validRef(calibration) ||
		!validRef(consents) || !digestPattern.MatchString(modelLockSHA) ||
		len(task.Ordinals) < 1 || len(task.Ordinals) > 128 {
		return nil, ErrInvalid
	}
	for i, ordinal := range task.Ordinals {
		if ordinal < 0 || ordinal >= 5000 || (i > 0 && ordinal != task.Ordinals[i-1]+1) {
			return nil, ErrInvalid
		}
	}
	return canonical.Marshal(SessionDescribeParams{1, snapshot, task.Ordinals,
		modelLockSHA, calibration, consents})
}

// PublishSessionDescribeParams uses one durable reservation per deterministic
// shard. The draft run must already exist; replay returns the same artifact.
func PublishSessionDescribeParams(ctx context.Context, documents DocumentPublisher,
	tenant, run uuid.UUID, ordinal int, task SessionDescriptorTask,
	snapshot, calibration, consents Ref, modelLockSHA string) (Ref, error) {
	if documents == nil || tenant == uuid.Nil || run == uuid.Nil ||
		ordinal < 0 || ordinal >= 40 || task.NodeKey != fmt.Sprintf("session.describe:%d", ordinal) ||
		len(task.Ordinals) == 0 || task.Ordinals[0] != ordinal*128 {
		return Ref{}, ErrInvalid
	}
	body, err := CompileSessionDescribeParams(task, snapshot, calibration,
		consents, modelLockSHA)
	if err != nil {
		return Ref{}, err
	}
	ref, err := documents.Publish(ctx, artifactjson.Scope{Tenant: tenant, Run: run,
		Owner: task.ID, Purpose: fmt.Sprintf("v2_analysis_describe_params_%d", ordinal)}, body)
	if err != nil {
		return Ref{}, err
	}
	result := Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}
	sha, err := canonical.Hash(json.RawMessage(body))
	if err != nil || !validRef(result) || result.SHA256 != sha {
		return Ref{}, ErrInvalid
	}
	return result, nil
}

// AnalysisStageQuerier permits a pool or a narrow test double. Stage is one
// database statement; it performs no object IO or transaction-wide retries.
type AnalysisStageQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type SQLAnalysisStageStore struct{ DB AnalysisStageQuerier }

func (s SQLAnalysisStageStore) Stage(ctx context.Context, tenant, run, workflow uuid.UUID,
	plan SessionDescriptorPlan, tasks []AnalysisDescribeStageTask) error {
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil || workflow == uuid.Nil ||
		len(plan.Tasks) < 1 || len(plan.Tasks) > 40 || len(tasks) != len(plan.Tasks) {
		return ErrInvalid
	}
	type row struct {
		ID               uuid.UUID       `json:"id"`
		NodeKey          string          `json:"node_key"`
		ParamsArtifactID uuid.UUID       `json:"params_artifact_id"`
		ParamsSHA        string          `json:"params_sha"`
		InputDigest      string          `json:"input_digest"`
		ExecutionProfile json.RawMessage `json:"execution_profile"`
	}
	rows := make([]row, len(tasks))
	for i, task := range tasks {
		expected, err := TaskID(workflow, "session.describe", i)
		key := fmt.Sprintf("session.describe:%d", i)
		if err != nil || plan.Tasks[i].ID != expected || plan.Tasks[i].NodeKey != key ||
			len(plan.Tasks[i].Ordinals) < 1 || len(plan.Tasks[i].Ordinals) > 128 ||
			(i < len(tasks)-1 && len(plan.Tasks[i].Ordinals) != 128) ||
			!validRef(task.Params) || !digestPattern.MatchString(task.InputDigest) ||
			!validSessionDescribeProfile(task.Profile) {
			return ErrInvalid
		}
		for j, ordinal := range plan.Tasks[i].Ordinals {
			if ordinal != i*128+j || ordinal >= 5000 {
				return ErrInvalid
			}
		}
		rows[i] = row{expected, key, task.Params.ArtifactID, task.Params.SHA256,
			task.InputDigest, task.Profile}
	}
	payload, err := json.Marshal(rows)
	if err != nil || len(payload) > 262144 {
		return ErrInvalid
	}
	var staged bool
	err = s.DB.QueryRow(ctx, "SELECT photo.stage_v22_analysis_describes($1,$2,$3)",
		tenant, run, json.RawMessage(payload)).Scan(&staged)
	if err != nil {
		return err
	}
	if !staged {
		return fmt.Errorf("%w: describe stage rejected", ErrInvalid)
	}
	return nil
}
