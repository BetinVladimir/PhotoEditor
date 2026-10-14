package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

// PublishSemanticAssemblyParams keeps params immutable across lease recovery.
// The publisher repeats SQL lease authority before and after versioned IO.
func PublishSemanticAssemblyParams(ctx context.Context, documents DocumentPublisher, lease SemanticAssemblyLease, params SemanticAssembleParams) (Ref, error) {
	if documents == nil || !validSemanticAssemblyLease(lease) {
		return Ref{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	body, err := canonical.Marshal(params)
	if err != nil {
		return Ref{}, ErrInvalid
	}
	if _, err := DecodeSemanticAssembleParams(body); err != nil {
		return Ref{}, err
	}
	ref, err := documents.Publish(ctx, artifactjson.Scope{Tenant: lease.TenantID, Run: lease.RunID, Owner: lease.ExpansionID, Purpose: "task_params", AssemblyLease: lease.Token}, body)
	if err != nil {
		return Ref{}, err
	}
	result := Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}
	digest := sha256.Sum256(body)
	if !validRef(result) || result.SHA256 != hex.EncodeToString(digest[:]) {
		return Ref{}, ErrInvalid
	}
	return result, nil
}
