package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type signingDecodeParams struct {
	Source        v2input.Ref `json:"source"`
	Bytes         int64       `json:"expected_bytes"`
	Suffix        string      `json:"suffix"`
	AssumeProfile *string     `json:"assume_profile"`
	Analyze       bool        `json:"analyze"`
	Tile          int         `json:"tile"`
}
type DecodeSigningTaskResolver struct {
	Documents  SigningTaskDocumentReader
	Originals  SigningOriginalQualifier
	WorkICCSHA string
}

func (s DecodeSigningTaskResolver) ResolveSigningTaskLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask) (ResolvedSigningTask, error) {
	var zero ResolvedSigningTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Documents == nil || s.Originals == nil || !digest(s.WorkICCSHA) || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || e.Operation != "asset.decode" || e.Version != "1" || e.ReviewRequired || normalizeCanonicalSigningTask(&e) != nil {
		return zero, ErrInvalid
	}
	paramsRaw, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, e, e.Params, 1<<20)
	if err != nil {
		return zero, err
	}
	paramsRaw = append([]byte(nil), paramsRaw...)
	if !pinned(paramsRaw, e.Params, 1<<20) {
		return zero, ErrInvalid
	}
	p, err := decodeSigningDecodeParams(paramsRaw)
	if err != nil {
		return zero, err
	}
	inputs, err := canonicalTaskInputRefs(e.Inputs)
	if err != nil || len(inputs) != 1 || inputs[0] != p.Source {
		return zero, ErrInvalid
	}
	outputs, err := canonicalTaskOutputRefs(e.Outputs)
	if err != nil || len(outputs) < 2 || len(outputs) > 3 {
		return zero, ErrInvalid
	}
	master := outputs[len(outputs)-1]
	imageRaw, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, e, master, 1<<20)
	if err != nil {
		return zero, err
	}
	imageRaw = append([]byte(nil), imageRaw...)
	if !pinned(imageRaw, master, 1<<20) {
		return zero, ErrInvalid
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return zero, ErrInvalid
	}
	if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" || image.Color.ICCSHA256 != s.WorkICCSHA || image.Canvas.SourceSHA256 != p.Source.SHA256 || image.Canvas.Orientation != 1 || image.Canvas.SpaceID != "oriented:"+p.Source.SHA256 || image.Pixels.ID != outputs[0].ArtifactID || image.Pixels.SHA256 != outputs[0].SHA256 {
		return zero, ErrInvalid
	}
	if len(outputs) == 2 && image.Alpha != nil || len(outputs) == 3 && (image.Alpha == nil || image.Alpha.ID != outputs[1].ArtifactID || image.Alpha.SHA256 != outputs[1].SHA256) || p.Analyze && image.Alpha != nil {
		return zero, ErrInvalid
	}
	chainHash, err := canonical.Hash(map[string]any{"steps": []any{}})
	if err != nil {
		return zero, err
	}
	chain, err := canonical.Marshal(map[string]any{"steps": []any{}, "hash": chainHash})
	if err != nil || !bytes.Equal(chain, image.TransformChain) {
		return zero, ErrInvalid
	}
	if err := s.Originals.QualifySigningOriginal(ctx, who, ready, e, p.Source, master, p.Bytes); err != nil {
		return zero, err
	}
	for _, document := range []struct {
		ref v2input.Ref
		raw []byte
	}{{e.Params, paramsRaw}, {master, imageRaw}} {
		current, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, e, document.ref, 1<<20)
		if err != nil {
			return zero, err
		}
		if !bytes.Equal(current, document.raw) {
			return zero, ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ResolvedSigningTask{Sources: []v2input.Ref{p.Source}, Node: SigningExecutionNode{Task: e.Task, Run: e.Run, CanonicalResult: e.Result, Attempt: e.Attempt, Fence: e.Fence, Parents: []uuid.UUID{}, Inputs: []v2input.Ref{p.Source}, Outputs: outputs, Action: SigningAction{Operation: e.Operation, Version: e.Version, ParamsSHA: e.Params.SHA256}}}, nil
}

func decodeSigningDecodeParams(raw []byte) (signingDecodeParams, error) {
	var p signingDecodeParams
	if len(raw) < 2 || len(raw) > 1<<20 {
		return p, ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return p, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return p, ErrInvalid
	}
	for key, value := range map[string]json.RawMessage{"assume_profile": json.RawMessage(`null`), "analyze": json.RawMessage(`false`), "tile": json.RawMessage(`512`)} {
		if _, ok := fields[key]; !ok {
			fields[key] = value
		}
	}
	normalized, err = canonical.Marshal(fields)
	if err != nil || decode(normalized, &p, 1<<20) != nil || !ref(p.Source) || p.Bytes < 1 || p.Bytes > 512<<20 || !slices.Contains([]int{256, 512, 1024}, p.Tile) {
		return signingDecodeParams{}, ErrInvalid
	}
	suffixes := strings.Fields(".jpg .jpeg .png .tif .tiff .nef .nrw .crw .cr2 .cr3 .arw .srf .sr2 .mrw .dng .rw2 .orf .raf .pef .srw .3fr .fff .iiq .mos .kdc .mef .x3f")
	if !slices.Contains(suffixes, p.Suffix) || p.AssumeProfile != nil && !slices.Contains([]string{"srgb_v1", "linear_srgb_v1", "work_rec2020_gamma24_v1"}, *p.AssumeProfile) {
		return signingDecodeParams{}, ErrInvalid
	}
	return p, nil
}

var _ SigningTaskLineageResolver = DecodeSigningTaskResolver{}
