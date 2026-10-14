package semanticproposal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Internal prerequisite identities, not executable tasks or output references.
// Persistence, original snapshot authorization, qualification, resource admission
// and actual target compiler support are mandatory before these can be scheduled.
type nativeTargetPreparationStage struct {
	ID               uuid.UUID   `json:"id"`
	Operation        string      `json:"operation"`
	OperationVersion string      `json:"operation_version"`
	Requires         []uuid.UUID `json:"requires"`
}
type nativeTargetPreparationEntry struct {
	Ordinal        int                            `json:"ordinal"`
	Binding        v2input.SessionVersionBinding  `json:"binding"`
	Namespace      uuid.UUID                      `json:"namespace"`
	ReuseReference bool                           `json:"reuse_reference"`
	Stages         []nativeTargetPreparationStage `json:"stages"`
}
type nativeTargetPreparationManifestBody struct {
	SchemaVersion int                            `json:"schema_version"`
	Tenant        uuid.UUID                      `json:"tenant_id"`
	Project       uuid.UUID                      `json:"project_id"`
	Actor         uuid.UUID                      `json:"actor_id"`
	Session       uuid.UUID                      `json:"session_id"`
	Parent        uuid.UUID                      `json:"parent_run_id"`
	RequestSHA    string                         `json:"request_sha"`
	Intent        v2input.Ref                    `json:"intent"`
	Snapshot      v2input.Ref                    `json:"snapshot"`
	Entries       []nativeTargetPreparationEntry `json:"entries"`
}
type nativeTargetPreparationManifest struct {
	body json.RawMessage
	sha  string
}

func compileNativeTargetPreparationManifest(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage) (nativeTargetPreparationManifest, error) {
	var zero nativeTargetPreparationManifest
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return zero, err
	}
	targets, err := BindTargets(owned, snapshot, snapshotBody)
	if err != nil {
		return zero, err
	}
	if published.Inventory.Scope.ProjectID == uuid.Nil || len(targets) < 1 || len(targets) > 5000 || published.Inventory.Scope.ItemCount != len(targets) || len(published.Inventory.Targets) != len(targets) || published.Ref.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	intentSHA, err := canonical.Hash(owned.Intent)
	if err != nil || intentSHA != published.Ref.SHA256 {
		return zero, v2input.ErrConflict
	}
	result := nativeTargetPreparationManifestBody{SchemaVersion: 1, Tenant: owned.TenantID, Project: published.Inventory.Scope.ProjectID, Actor: who.UserID, Session: who.SessionID, Parent: owned.RunID, RequestSHA: owned.RequestSHA, Intent: published.Ref, Snapshot: snapshot, Entries: make([]nativeTargetPreparationEntry, len(targets))}
	for i, binding := range targets {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if binding != published.Inventory.Targets[i] {
			return zero, v2input.ErrConflict
		}
		digest, err := canonical.Hash(binding)
		if err != nil {
			return zero, err
		}
		namespace := uuid.NewSHA1(owned.RunID, []byte(fmt.Sprintf("semantic.target.prepare:1:%s:%d:%s", snapshot.SHA256, i, digest)))
		entry := nativeTargetPreparationEntry{Ordinal: i, Binding: binding, Namespace: namespace, ReuseReference: binding == owned.Intent.Reference, Stages: []nativeTargetPreparationStage{}}
		if !entry.ReuseReference {
			operations := []string{"mask.person", "face.detect", "face.landmarks", "face.parse", "mask.face_skin", "defects.propose"}
			ids := make(map[string]uuid.UUID, len(operations))
			for _, op := range operations {
				ids[op] = uuid.NewSHA1(namespace, []byte("stage:"+op))
			}
			dependencies := map[string][]string{"face.landmarks": {"face.detect"}, "face.parse": {"face.detect"}, "mask.face_skin": {"face.parse"}, "defects.propose": {"mask.person", "face.landmarks", "mask.face_skin"}}
			for _, op := range operations {
				stage := nativeTargetPreparationStage{ID: ids[op], Operation: op, OperationVersion: "1", Requires: []uuid.UUID{}}
				for _, dependency := range dependencies[op] {
					stage.Requires = append(stage.Requires, ids[dependency])
				}
				entry.Stages = append(entry.Stages, stage)
			}
		}
		result.Entries[i] = entry
	}
	body, err := canonical.Marshal(result)
	if err != nil {
		return zero, err
	}
	if len(body) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	sha, err := canonical.Hash(result)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeTargetPreparationManifest{body: body, sha: sha}, nil
}

// A seal binds the original request and complete pinned target inventory. It
// conveys no permission to regenerate ambiguous/corrupt evidence or skip IO.
func verifyNativeTargetPreparationManifest(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage, saved nativeTargetPreparationManifest) error {
	expected, err := compileNativeTargetPreparationManifest(ctx, who, request, key, published, snapshot, snapshotBody)
	if err != nil {
		return err
	}
	if saved.sha != expected.sha || !bytes.Equal(saved.body, expected.body) {
		return v2input.ErrConflict
	}
	return nil
}

// Private continuation payload. Public dependency-unavailable classification is
// unchanged; only the server orchestrator may inspect the owned frozen plan.
// Receipt/admission authority is never carried by this error.
type nativeTargetPreparationRequired struct {
	manifest nativeTargetPreparationManifest
}

func (e *nativeTargetPreparationRequired) Error() string {
	return "target prerequisite preparation required"
}
func (e *nativeTargetPreparationRequired) Unwrap() error { return errTransferProtectionUnavailable }

// restoreNativeTargetPreparationManifest treats persisted bytes as untrusted.
// Recompilation binds every identity/dependency to the original immutable inputs;
// neither a matching checksum alone nor parsed JSON conveys admission authority.
// The returned bytes are owned independently of the database driver's buffer.
func restoreNativeTargetPreparationManifest(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody, body json.RawMessage, sha string) (nativeTargetPreparationManifest, error) {
	var zero nativeTargetPreparationManifest
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(body) < 2 || len(body) > 8<<20 || len(sha) != 64 {
		return zero, v2input.ErrConflict
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != sha {
		return zero, v2input.ErrConflict
	}
	saved := nativeTargetPreparationManifest{body: append(json.RawMessage(nil), body...), sha: sha}
	if err := verifyNativeTargetPreparationManifest(ctx, who, request, key, published, snapshot, snapshotBody, saved); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return saved, nil
}
