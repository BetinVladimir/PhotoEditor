package v2render

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// AdmitDetector performs the actual draft, physical Params and input transport,
// closure registration and public seal. It returns no synthetic completion or
// selection receipt. The installed public chain must independently admit the
// dedicated namespace, or this operation fails with that original error.
func (p AuthoredFacePreparation) AdmitDetector(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, expected v2qualification.Expected, report v2input.Ref, score float64, maxFaces int, limits AuthoredFaceBudget) error {
	frozen, err := p.PrepareDetector(ctx, who, source, image, expected, report, score, maxFaces, limits)
	if err != nil {
		return err
	}
	prepared, err := p.PublishDetectorTransport(ctx, source, frozen)
	if err != nil {
		return err
	}
	return p.SealDetector(ctx, source, prepared)
}

// AdmitLandmarks reads the actual current detector; no detector/face identifiers
// or warning state can be supplied by the caller in place of its canonical root.
func (p AuthoredFacePreparation) AdmitLandmarks(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, expected v2qualification.Expected, report v2input.Ref, limits AuthoredFaceBudget) error {
	frozen, err := p.PrepareLandmarks(ctx, who, source, image, expected, report, limits)
	if err != nil {
		return err
	}
	prepared, err := p.PublishLandmarkTransport(ctx, source, frozen)
	if err != nil {
		return err
	}
	return p.SealLandmarks(ctx, source, prepared)
}

func (p AuthoredFacePreparation) AdmitParser(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, expected v2qualification.Expected, report v2input.Ref, limits AuthoredFaceBudget) error {
	frozen, err := p.PrepareParser(ctx, who, source, image, expected, report, limits)
	if err != nil {
		return err
	}
	prepared, err := p.PublishParserTransport(ctx, source, frozen)
	if err != nil {
		return err
	}
	return p.SealParser(ctx, source, prepared)
}
