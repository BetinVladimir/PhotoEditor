package v2render

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// SQLAuthoredDetailContinuation publishes each protected intermediate document
// before admitting its real segment. Compiler bytes are untrusted: each concrete
// publisher repeats its original-occurrence/current-source SQL authority before
// reservation and completion. No selected face or skin is substituted for the
// authored portrait regions, protection, or ResolvedStyle.
type SQLAuthoredDetailContinuation struct {
	Style   AuthoredStyleBindingPublication
	Params  AuthoredDetailParamsPublication
	Layers  AuthoredCurrentDetailLayersPublication
	Segment SQLAuthoredSegmentContinuation
}

func (s SQLAuthoredDetailContinuation) Resume(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int, styleBody []byte, styleWitnesses map[string]string, paramsBody []byte, transforms json.RawMessage, layerTemplate []byte, maskBindings json.RawMessage, protected *v2input.Ref) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	db := s.Style.Scheduler
	if db == nil || s.Params.Scheduler != db || s.Layers.Scheduler != db || s.Segment.Producers.Scheduler != db || s.Style.Objects == nil || s.Params.Objects == nil || s.Layers.Objects == nil || s.Style.Bucket == "" || s.Params.Bucket != s.Style.Bucket || s.Layers.Bucket != s.Style.Bucket || s.Segment.Producers.Bucket != s.Style.Bucket {
		return false, v2input.ErrInvalid
	}
	before, current, err := ReadAuthoredInstructionSource(ctx, db, tenant, parent, ordinal)
	if err != nil {
		return false, err
	}
	if !current {
		return false, v2input.ErrExpired
	}
	if before.Actor != who.UserID || before.Session != who.SessionID {
		return false, v2input.ErrInvalid
	}
	style, err := s.Style.Publish(ctx, tenant, parent, ordinal, styleBody, styleWitnesses)
	if err != nil {
		return false, err
	}
	params, err := s.Params.Publish(ctx, tenant, parent, ordinal, paramsBody, style, transforms)
	if err != nil {
		return false, err
	}
	layersBody, err := bindAuthoredDetailParams(layerTemplate, params)
	if err != nil {
		return false, err
	}
	layers, err := s.Layers.Publish(ctx, tenant, parent, ordinal, layersBody, maskBindings)
	if err != nil {
		return false, err
	}
	after, current, err := ReadAuthoredInstructionSource(ctx, db, tenant, parent, ordinal)
	if err != nil {
		return false, err
	}
	if !current {
		return false, v2input.ErrExpired
	}
	a, err := canonical.Marshal(before)
	if err != nil {
		return false, err
	}
	b, err := canonical.Marshal(after)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(a, b) {
		return false, v2input.ErrExpired
	}
	return s.Segment.Resume(ctx, who, tenant, parent, ordinal, &layers, protected)
}

// Replace only the Params reference with the actually published document. The
// layer publisher independently checks the entire original layer, source,
// projected mask DAG, order, settings, and exact published Params authority.
func bindAuthoredDetailParams(raw []byte, params v2input.Ref) ([]byte, error) {
	if !validLayerArtifactRef(params) || len(raw) < 2 || len(raw) > 1048576 {
		return nil, v2input.ErrInvalid
	}
	wire, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(wire, raw) {
		return nil, v2input.ErrInvalid
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return nil, v2input.ErrInvalid
	}
	var layers []map[string]json.RawMessage
	if json.Unmarshal(body["layers"], &layers) != nil || len(layers) != 1 || layers[0] == nil {
		return nil, v2input.ErrInvalid
	}
	var op string
	var version string
	if json.Unmarshal(layers[0]["operation"], &op) != nil || op != "portrait.detail" || json.Unmarshal(layers[0]["operation_version"], &version) != nil || version != "1" {
		return nil, v2input.ErrInvalid
	}
	ref, err := canonical.Marshal(params)
	if err != nil {
		return nil, err
	}
	layers[0]["params"] = ref
	layerRaw, err := canonical.Marshal(layers)
	if err != nil {
		return nil, err
	}
	body["layers"] = layerRaw
	return canonical.Marshal(body)
}
