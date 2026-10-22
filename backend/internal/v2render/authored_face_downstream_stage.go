package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func (p AuthoredFacePreparation) downstreamInputs(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, expected v2qualification.Expected, report v2input.Ref) (currentAuthoredFaceProducer, AuthoredFaceQualification, error) {
	var detector currentAuthoredFaceProducer
	var proof AuthoredFaceQualification
	if p.Scheduler == nil || p.Params.Scheduler != p.Scheduler || p.Producers.Scheduler != p.Scheduler || who.UserID != source.Actor || who.SessionID != source.Session || expected.Version != "1" || (expected.Operation != "face.landmarks" && expected.Operation != "face.parse") {
		return detector, proof, v2input.ErrInvalid
	}
	var err error
	proof, err = p.Qualification.Read(ctx, who, source.Tenant, source.Project, expected, report)
	if err != nil {
		return detector, proof, err
	}
	detector, err = p.Producers.read(ctx, source, "face.detect")
	return detector, proof, err
}
func (p AuthoredFacePreparation) PrepareLandmarks(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, expected v2qualification.Expected, report v2input.Ref, limits AuthoredFaceBudget) (v2input.FrozenSemanticLandmarkTask, error) {
	var zero v2input.FrozenSemanticLandmarkTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected = ownAuthoredDefectExpected(expected)
	image = append(json.RawMessage(nil), image...)
	if expected.Operation != "face.landmarks" {
		return zero, v2input.ErrInvalid
	}
	detector, proof, err := p.downstreamInputs(ctx, who, source, expected, report)
	if err != nil {
		return zero, err
	}
	d := detector.observation
	prepared, err := v2input.PrepareSemanticLandmarkParams(ctx, source.Source, image, d.Roots[0], detector.root, d.Params, detector.params, expected.ModelLockSHA)
	if err != nil {
		return zero, err
	}
	_, workflow, err := AuthoredFacePrerequisiteIDs(source, expected.Operation)
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, expected.Operation, 0)
	if err != nil {
		return zero, err
	}
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("params")), SHA256: authoredFaceCandidateSHA(prepared.Params)}
	sizes, err := readAuthoredFaceStageSizes(ctx, p.Scheduler, source.Tenant, prepared.Inputs, ref, int64(len(prepared.Params)))
	if err != nil {
		return zero, err
	}
	limits.Backend = expected.Backend
	limits.Costs = proof.costs
	limits.Sizes = sizes
	frozen, err := CompileAuthoredFaceLandmarks(ctx, source, image, d.Roots[0], detector.root, d.Params, detector.params, ref, expected.ModelLockSHA, limits)
	if err != nil {
		return zero, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if err = p.stageNative(ctx, who, source, expected, proof, draft, ref, prepared.Params, &d); err != nil {
		return zero, err
	}
	return frozen, ctx.Err()
}
func (p AuthoredFacePreparation) PrepareParser(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, expected v2qualification.Expected, report v2input.Ref, limits AuthoredFaceBudget) (v2input.FrozenSemanticFaceParseTask, error) {
	var zero v2input.FrozenSemanticFaceParseTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected = ownAuthoredDefectExpected(expected)
	image = append(json.RawMessage(nil), image...)
	if expected.Operation != "face.parse" {
		return zero, v2input.ErrInvalid
	}
	detector, proof, err := p.downstreamInputs(ctx, who, source, expected, report)
	if err != nil {
		return zero, err
	}
	d := detector.observation
	prepared, err := v2input.PrepareSemanticFaceParseParams(ctx, source.Source, image, d.Roots[0], detector.root, d.Params, detector.params, expected.ModelLockSHA)
	if err != nil {
		return zero, err
	}
	_, workflow, err := AuthoredFacePrerequisiteIDs(source, expected.Operation)
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, expected.Operation, 0)
	if err != nil {
		return zero, err
	}
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("params")), SHA256: authoredFaceCandidateSHA(prepared.Params)}
	sizes, err := readAuthoredFaceStageSizes(ctx, p.Scheduler, source.Tenant, prepared.Inputs, ref, int64(len(prepared.Params)))
	if err != nil {
		return zero, err
	}
	limits.Backend = expected.Backend
	limits.Costs = proof.costs
	limits.Sizes = sizes
	frozen, err := CompileAuthoredFaceParser(ctx, source, image, d.Roots[0], detector.root, d.Params, detector.params, ref, expected.ModelLockSHA, limits)
	if err != nil {
		return zero, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if err = p.stageNative(ctx, who, source, expected, proof, draft, ref, prepared.Params, &d); err != nil {
		return zero, err
	}
	return frozen, ctx.Err()
}
