package workflow

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func validApplyLocator(l ApplyLease, ref review.ArtifactRef) bool {
	if l.TenantID == uuid.Nil || l.RunID == uuid.Nil || l.Token == uuid.Nil || ref.ID == uuid.Nil {
		return false
	}
	h, e := hex.DecodeString(ref.SHA256)
	return e == nil && len(h) == 32 && hex.EncodeToString(h) == ref.SHA256
}
func validateApplyLocator(a styleArtifact, ref review.ArtifactRef) error {
	if a.ArtifactID != ref.ID || a.SHA256 != ref.SHA256 || a.Bytes < 1 || a.Bucket == "" || a.Key == "" || a.VersionID == "" || a.VersionID == "null" || a.MediaType == "" {
		return errors.New("apply artifact locator identity incomplete")
	}
	return nil
}
func (s ApplyStore) inputStyleArtifact(ctx context.Context, l ApplyLease, ref review.ArtifactRef) (styleArtifact, error) {
	var a styleArtifact
	if s.DB == nil || !validApplyLocator(l, ref) {
		return a, errors.New("apply style locator lease/ref invalid")
	}
	err := s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.apply_input_style_locator($1,$2,$3,$4,$5)`, l.TenantID, l.RunID, l.Token, ref.ID, ref.SHA256).Scan(&a.ArtifactID, &a.SHA256, &a.Bytes, &a.Bucket, &a.Key, &a.VersionID, &a.MediaType)
	if err == nil {
		err = validateApplyLocator(a, ref)
	}
	return a, err
}

// Metadata authority is the live Apply lease and stored source/canonical-output
// membership, never a tenant-only ready-artifact lookup or caller locator.
func (s ApplyStore) Artifact(ctx context.Context, l ApplyLease, ref review.ArtifactRef) (styleArtifact, error) {
	var a styleArtifact
	if s.DB == nil || !validApplyLocator(l, ref) {
		return a, errors.New("apply artifact locator lease/ref invalid")
	}
	err := s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.apply_artifact_locator($1,$2,$3,$4,$5)`, l.TenantID, l.RunID, l.Token, ref.ID, ref.SHA256).Scan(&a.ArtifactID, &a.SHA256, &a.Bytes, &a.Bucket, &a.Key, &a.VersionID, &a.MediaType)
	if err == nil {
		err = validateApplyLocator(a, ref)
	}
	return a, err
}
