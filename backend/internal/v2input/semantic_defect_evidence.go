package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// VerifySemanticDefectEvidence binds original producer Params to the report's
// complete embedded Params and validates exact proposal/source/protection refs.
// Caller separately proves canonical producer, qualified face/runtime evidence,
// catalog versions and actual protected pixel coverage.
func VerifySemanticDefectEvidence(ctx context.Context, paramsRef Ref, paramsBody json.RawMessage, reportRef Ref, reportBody json.RawMessage, proposalRef Ref, proposalBody json.RawMessage, binding SessionVersionBinding, imageBody json.RawMessage) (Ref, error) {
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	if !validRef(paramsRef) || paramsRef.ArtifactID == reportRef.ArtifactID || paramsRef.ArtifactID == proposalRef.ArtifactID || len(paramsBody) < 2 || len(paramsBody) > 1048576 {
		return Ref{}, ErrInvalid
	}
	digest := sha256.Sum256(paramsBody)
	if hex.EncodeToString(digest[:]) != paramsRef.SHA256 {
		return Ref{}, ErrInvalid
	}
	original, err := canonical.Bytes(paramsBody)
	if err != nil || !bytes.Equal(original, paramsBody) {
		return Ref{}, ErrInvalid
	}
	if len(reportBody) < 2 || len(reportBody) > 1048576 {
		return Ref{}, ErrInvalid
	}
	normalized, err := canonical.Bytes(reportBody)
	if err != nil || !bytes.Equal(normalized, reportBody) {
		return Ref{}, ErrInvalid
	}
	protection, err := ResolveSemanticDefectProtection(ctx, reportRef, reportBody, proposalRef, proposalBody, binding, imageBody)
	if err != nil {
		return Ref{}, err
	}
	var projection struct {
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(reportBody, &projection) != nil || !bytes.Equal(projection.Params, paramsBody) {
		return Ref{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	return protection, nil
}
