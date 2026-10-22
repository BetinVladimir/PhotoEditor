package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// This proof retains original declarations, never old accepted candidate IDs,
// source-independent mask authority or qualification for the new proposal.
type OriginalAuthoredDefectPolicy struct {
	contextJCS, repairJCS, paramsJCS string
	params                           v2input.Ref
	source                           review.DefectSource
	locator                          originalDefectPolicyLocator
}

func (p OriginalAuthoredDefectPolicy) ParamsRef() v2input.Ref { return p.params }
func (p OriginalAuthoredDefectPolicy) Source() review.DefectSource {
	out := p.source
	out.TransformChain = append(json.RawMessage(nil), p.source.TransformChain...)
	return out
}
func (p OriginalAuthoredDefectPolicy) Policy() review.DefectProposalInputPolicy {
	// Decode owned immutable original bytes afresh, including all nested masks,
	// faces, RawMessages and optional pointer fields. No caller aliases survive.
	out, _ := review.DefectProposalInputPolicyFromParams(review.ArtifactRef{ID: p.params.ArtifactID, SHA256: p.params.SHA256}, []byte(p.paramsJCS), p.Source())
	return out
}

type SQLAuthoredOriginalDefectPolicyReader struct {
	Scheduler *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}
type originalDefectPolicyObject struct {
	Ref     v2input.Ref `json:"ref"`
	Bucket  string      `json:"bucket"`
	Key     string      `json:"key"`
	Version string      `json:"version"`
	Bytes   int64       `json:"bytes"`
}
type originalDefectPolicyProducer struct {
	Run     uuid.UUID `json:"run_id"`
	Task    uuid.UUID `json:"task_id"`
	Result  uuid.UUID `json:"result_id"`
	Attempt int       `json:"attempt"`
	Fence   int64     `json:"fence"`
}
type originalDefectPolicyLocator struct {
	Repair   originalDefectPolicyObject    `json:"repair"`
	Proposal *originalDefectPolicyObject   `json:"proposal,omitempty"`
	Params   *originalDefectPolicyObject   `json:"params,omitempty"`
	Image    *originalDefectPolicyObject   `json:"image,omitempty"`
	Producer *originalDefectPolicyProducer `json:"producer,omitempty"`
}

func decodeOriginalDefectPolicyLocator(raw []byte, bucket string, stage int) (originalDefectPolicyLocator, error) {
	out := originalDefectPolicyLocator{}
	if len(raw) < 2 || len(raw) > 16384 || bytes.Equal(raw, []byte("null")) {
		return out, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return out, err
	}
	d := json.NewDecoder(bytes.NewReader(normalized))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil {
		return originalDefectPolicyLocator{}, v2input.ErrInvalid
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return originalDefectPolicyLocator{}, v2input.ErrInvalid
	}
	objects := []originalDefectPolicyObject{out.Repair}
	switch stage {
	case 0:
		if out.Proposal != nil || out.Params != nil || out.Image != nil || out.Producer != nil {
			return originalDefectPolicyLocator{}, v2input.ErrInvalid
		}
	case 1, 2:
		if out.Proposal == nil || out.Params == nil || out.Producer == nil || stage == 1 && out.Image != nil || stage == 2 && out.Image == nil {
			return originalDefectPolicyLocator{}, v2input.ErrInvalid
		}
		if out.Producer.Run == uuid.Nil || out.Producer.Task == uuid.Nil || out.Producer.Result == uuid.Nil || out.Producer.Attempt < 1 || out.Producer.Fence < 1 {
			return originalDefectPolicyLocator{}, v2input.ErrInvalid
		}
		objects = append(objects, *out.Proposal, *out.Params)
		if out.Image != nil {
			objects = append(objects, *out.Image)
		}
	default:
		return originalDefectPolicyLocator{}, v2input.ErrInvalid
	}
	for _, obj := range objects {
		if !validLayerArtifactRef(obj.Ref) || obj.Bucket != bucket || bucket == "" || obj.Key == "" || obj.Version == "" || obj.Version == "null" || obj.Bytes < 2 || obj.Bytes > 1048576 {
			return originalDefectPolicyLocator{}, v2input.ErrInvalid
		}
	}
	return out, nil
}
func sameOriginalDefectLocator(a, b originalDefectPolicyLocator) bool {
	aa, err := canonical.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := canonical.Marshal(b)
	return err == nil && bytes.Equal(aa, bb)
}
func (s SQLAuthoredOriginalDefectPolicyReader) lookup(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, sourceJCS string, repair, params *string) (originalDefectPolicyLocator, error) {
	empty := originalDefectPolicyLocator{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.Scheduler == nil || s.Objects == nil || s.Bucket == "" || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || source.Actor != who.UserID || source.Session != who.SessionID || source.Tenant == uuid.Nil || source.Parent == uuid.Nil || source.Instruction.Kind != "segment" || len(source.Instruction.Layers) != 1 {
		return empty, v2input.ErrInvalid
	}
	conn, err := s.Scheduler.Acquire(ctx)
	if err != nil {
		return empty, err
	}
	defer conn.Release()
	var role string
	if err = conn.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		return empty, err
	}
	if role != "photo_scheduler" {
		return empty, v2input.ErrInvalid
	}
	var raw []byte
	err = conn.QueryRow(ctx, `SELECT photo.authored_render_original_defect_policy_objects($1,$2,$3,$4,$5,$6,$7,$8)`, source.Tenant, source.Parent, source.Instruction.Ordinal, who.UserID, who.SessionID, sourceJCS, repair, params).Scan(&raw)
	if err != nil {
		return empty, err
	}
	stage := 0
	if repair != nil {
		stage = 1
	}
	if params != nil {
		stage = 2
	}
	return decodeOriginalDefectPolicyLocator(raw, s.Bucket, stage)
}
func (s SQLAuthoredOriginalDefectPolicyReader) read(ctx context.Context, obj originalDefectPolicyObject) ([]byte, error) {
	return v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objectstore.Object{Key: obj.Key, VersionID: obj.Version, SHA256: obj.Ref.SHA256, Bytes: obj.Bytes, ContentType: "application/json"}, obj.Ref)
}
func ownOriginalDefectInstructionSource(source AuthoredInstructionSource) (AuthoredInstructionSource, string, error) {
	raw, err := canonical.Marshal(source)
	if err != nil {
		return AuthoredInstructionSource{}, "", err
	}
	var owned AuthoredInstructionSource
	if err = json.Unmarshal(raw, &owned); err != nil {
		return AuthoredInstructionSource{}, "", err
	}
	return owned, string(raw), nil
}

func (s SQLAuthoredOriginalDefectPolicyReader) Read(ctx context.Context, who auth.Identity, source AuthoredInstructionSource) (OriginalAuthoredDefectPolicy, error) {
	empty := OriginalAuthoredDefectPolicy{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	source, sourceJCS, err := ownOriginalDefectInstructionSource(source)
	if err != nil {
		return empty, err
	}
	first, err := s.lookup(ctx, who, source, sourceJCS, nil, nil)
	if err != nil {
		return empty, err
	}
	repairRaw, err := s.read(ctx, first.Repair)
	if err != nil {
		return empty, err
	}
	repairJCS := string(repairRaw)
	again, err := s.lookup(ctx, who, source, sourceJCS, nil, nil)
	if err != nil {
		return empty, err
	}
	if !sameOriginalDefectLocator(first, again) {
		return empty, v2input.ErrConflict
	}
	second, err := s.lookup(ctx, who, source, sourceJCS, &repairJCS, nil)
	if err != nil {
		return empty, err
	}
	paramsRaw, err := s.read(ctx, *second.Params)
	if err != nil {
		return empty, err
	}
	proposalRaw, err := s.read(ctx, *second.Proposal)
	if err != nil {
		return empty, err
	}
	again, err = s.lookup(ctx, who, source, sourceJCS, &repairJCS, nil)
	if err != nil {
		return empty, err
	}
	if !sameOriginalDefectLocator(second, again) {
		return empty, v2input.ErrConflict
	}
	paramsJCS := string(paramsRaw)
	third, err := s.lookup(ctx, who, source, sourceJCS, &repairJCS, &paramsJCS)
	if err != nil {
		return empty, err
	}
	if third.Repair != second.Repair || *third.Params != *second.Params || *third.Proposal != *second.Proposal || *third.Producer != *second.Producer {
		return empty, v2input.ErrConflict
	}
	imageRaw, err := s.read(ctx, *third.Image)
	if err != nil {
		return empty, err
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return empty, err
	}
	original := image.Source(review.ArtifactRef{ID: third.Image.Ref.ArtifactID, SHA256: third.Image.Ref.SHA256})
	if _, err = review.DefectProposalInputPolicyFromParams(review.ArtifactRef{ID: third.Params.Ref.ArtifactID, SHA256: third.Params.Ref.SHA256}, paramsRaw, original); err != nil {
		return empty, err
	}
	if _, err = review.ReadDefectProposal(ctx, bytes.NewReader(proposalRaw), review.ArtifactRef{ID: third.Proposal.Ref.ArtifactID, SHA256: third.Proposal.Ref.SHA256}, original); err != nil {
		return empty, err
	}
	again, err = s.lookup(ctx, who, source, sourceJCS, &repairJCS, &paramsJCS)
	if err != nil {
		return empty, err
	}
	if !sameOriginalDefectLocator(third, again) {
		return empty, v2input.ErrConflict
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return OriginalAuthoredDefectPolicy{contextJCS: sourceJCS, repairJCS: repairJCS, paramsJCS: paramsJCS, params: third.Params.Ref, source: original, locator: third}, nil
}
func (s SQLAuthoredOriginalDefectPolicyReader) Recheck(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, proof OriginalAuthoredDefectPolicy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wire, err := canonical.Marshal(source)
	if err != nil {
		return err
	}
	if string(wire) != proof.contextJCS {
		return v2input.ErrConflict
	}
	current, err := s.lookup(ctx, who, source, string(wire), &proof.repairJCS, &proof.paramsJCS)
	if err != nil {
		return err
	}
	if !sameOriginalDefectLocator(current, proof.locator) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
