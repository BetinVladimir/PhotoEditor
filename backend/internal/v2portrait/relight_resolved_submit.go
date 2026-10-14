package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// SubmitResolvedRelight obtains original source and qualification bytes from
// server catalogs. Caller-provided SourceRaw/Evidence are ignored. Pin, runtime
// and capacity must be selected by the authenticated server/deployment layer;
// these fields are not a public request schema.
func (s RelightBootstrapCoordinator) SubmitResolvedRelight(ctx context.Context, objects artifactjson.Objects, request RelightBootstrapRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Objects == nil || objects == nil {
		return nil, v2input.ErrInvalid
	}
	request.ParamsRaw = append([]byte(nil), request.ParamsRaw...)
	request.SourceRaw = nil
	request.Evidence = RelightQualificationEvidence{}
	if len(request.ParamsRaw) < 2 || len(request.ParamsRaw) > 65536 {
		return nil, v2input.ErrInvalid
	}
	var params RelightParams
	if json.Unmarshal(request.ParamsRaw, &params) != nil {
		return nil, v2input.ErrInvalid
	}
	// Strict schema/canonical Params before reading any server-owned objects.
	compiled, _, err := params.Compile()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(compiled, request.ParamsRaw) {
		return nil, v2input.ErrInvalid
	}
	source, err := (SQLRelightSourceReader{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket}).Load(ctx, request.Who, request.Tenant, request.Project, request.Snapshot, params.Base)
	if err != nil {
		return nil, err
	}
	evidence, err := LoadRelightQualificationEvidence(ctx, SQLRelightQualificationCatalog{DB: s.DB, Bucket: s.Bucket}, s.Objects, request.Who, request.Tenant, request.Project, params, request.Pin, s.Expected)
	if err != nil {
		return nil, err
	}
	request.SourceRaw = source
	request.Evidence = evidence
	return s.SubmitRelight(ctx, objects, request)
}
