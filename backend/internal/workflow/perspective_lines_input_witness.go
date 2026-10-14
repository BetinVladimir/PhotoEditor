package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
)

type perspectiveLinesObjectWitness struct {
	Artifact uuid.UUID `json:"artifact_id"`
	SHA      string    `json:"sha256"`
	Bucket   string    `json:"bucket"`
	Key      string    `json:"object_key"`
	Version  string    `json:"object_version"`
	Bytes    int64     `json:"bytes"`
	Media    string    `json:"media_type"`
}

func linesObjectWitness(ref v2input.Ref, bucket string, o objectstore.Object) (perspectiveLinesObjectWitness, error) {
	if ref.ArtifactID == uuid.Nil || domainDigestInvalid(ref.SHA256) || o.SHA256 != ref.SHA256 || len(o.Key) < 1 || len(o.Key) > 1024 || o.VersionID == "" || o.VersionID == "null" || len(o.VersionID) > 1024 || o.Bytes < 1 || o.Bytes > 9007199254740991 {
		return perspectiveLinesObjectWitness{}, v2input.ErrInvalid
	}
	return perspectiveLinesObjectWitness{Artifact: ref.ArtifactID, SHA: ref.SHA256, Bucket: bucket, Key: o.Key, Version: o.VersionID, Bytes: o.Bytes, Media: o.ContentType}, nil
}

// AdmissionJSON is produced only from the private stream-verification witness.
// SQL must compare every object locator and repeat current source authorization.
func (v VerifiedPerspectiveLinesInputs) AdmissionJSON(plan PerspectiveLinesPlan, bucket string) (json.RawMessage, error) {
	if v.scope != plan.scope || v.task != plan.task || len(bucket) < 3 || len(bucket) > 63 {
		return nil, v2input.ErrInvalid
	}
	if _, err := CompilePerspectiveLinesIntent(plan, v.snapshot, v.who); err != nil {
		return nil, err
	}
	image, err := review.ParseWorkingImage(plan.image)
	if err != nil {
		return nil, err
	}
	expected := []v2optical.PerspectiveSourceArraySpec{{Ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, Role: "WorkingPixels", Width: image.Canvas.Width, Height: image.Canvas.Height, Channels: 3}}
	if image.Alpha != nil {
		expected = append(expected, v2optical.PerspectiveSourceArraySpec{Ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, Role: "WorkingAlpha", Width: image.Canvas.Width, Height: image.Canvas.Height, Alpha: true})
	}
	if len(v.arrays) != len(expected) || v.source.ContentType != "application/json" || v.source.Bytes != int64(len(plan.image)) {
		return nil, v2input.ErrInvalid
	}
	source, err := linesObjectWitness(plan.scope.Base.Source, bucket, v.source)
	if err != nil {
		return nil, err
	}
	arrays := []any{}
	for i, w := range v.arrays {
		if w.Spec != expected[i] || (w.Object.ContentType != "application/x-npy" && w.Object.ContentType != "application/octet-stream") {
			return nil, v2input.ErrInvalid
		}
		o, err := linesObjectWitness(w.Spec.Ref, bucket, w.Object)
		if err != nil {
			return nil, err
		}
		arrays = append(arrays, map[string]any{"role": w.Spec.Role, "object": o})
	}
	return canonical.Marshal(map[string]any{"schema_version": 1, "task_id": v.task, "snapshot_id": v.snapshot, "actor_id": v.who.UserID, "auth_session_id": v.who.SessionID, "source": source, "arrays": arrays})
}
