package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type RestorationResultDocuments struct {
	Source    []byte
	Result    []byte
	Image     []byte
	Report    []byte
	Changed   []byte
	Generated []byte
}

// RestorationReviewCandidate is a validated document closure awaiting exact
// object-version, NPY/pixel checks and human review. It grants no Apply right.
type RestorationReviewCandidate struct {
	Result          v2input.Ref  `json:"result"`
	Image           v2input.Ref  `json:"image"`
	Report          v2input.Ref  `json:"report"`
	Changed         v2input.Ref  `json:"changed"`
	Generated       *v2input.Ref `json:"generated"`
	Pixels          v2input.Ref  `json:"pixels"`
	ChangedPixels   v2input.Ref  `json:"changed_pixels"`
	GeneratedPixels *v2input.Ref `json:"generated_pixels"`
}

func restorationExactJSON(ref v2input.Ref, raw []byte) error {
	if !isDocumentRef(ref) || len(raw) < 2 || len(raw) > 1048576 {
		return v2input.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return v2input.ErrInvalid
	}
	return nil
}

// VerifyRestorationResultDocuments validates the exact document graph and
// review-only semantics shared by reflection and both glare modes. The caller
// must separately prove output object versions and compare decoded pixels.
func VerifyRestorationResultDocuments(ctx context.Context, operation string,
	paramsRaw []byte, resultRef v2input.Ref,
	docs RestorationResultDocuments) (RestorationReviewCandidate, error) {
	var zero RestorationReviewCandidate
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if operation != "image.reflection" && operation != "portrait.glare" ||
		len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	var params struct {
		Base         v2input.SessionVersionBinding  `json:"base"`
		Donor        *v2input.SessionVersionBinding `json:"donor"`
		Mode         string                         `json:"mode"`
		ModelLockSHA string                         `json:"model_lock_sha"`
	}
	if json.Unmarshal(paramsRaw, &params) != nil || !isDocumentRef(params.Base.Source) ||
		len(params.ModelLockSHA) != 64 {
		return zero, v2input.ErrInvalid
	}
	if operation == "image.reflection" && (params.Donor != nil || params.Mode != "") ||
		operation == "portrait.glare" && (params.Mode != "donor" && params.Mode != "generative" ||
			params.Mode == "donor" && params.Donor == nil ||
			params.Mode == "generative" && params.Donor != nil) {
		return zero, v2input.ErrInvalid
	}
	source, err := v2portrait.VerifyDonorWorkingImage(params.Base, docs.Source)
	if err != nil {
		return zero, err
	}
	if err = restorationExactJSON(resultRef, docs.Result); err != nil {
		return zero, err
	}
	root, err := ParseImageResult(docs.Result, resultRef)
	if err != nil {
		return zero, err
	}
	for _, pair := range []struct {
		ref v2input.Ref
		raw []byte
	}{
		{root.WorkingImage, docs.Image}, {root.Report, docs.Report}, {root.ChangedMask, docs.Changed}} {
		if err = restorationExactJSON(pair.ref, pair.raw); err != nil {
			return zero, err
		}
	}
	output, err := review.ParseWorkingImage(docs.Image)
	if err != nil || output.DType != "float32" || output.Canvas != source.Canvas ||
		output.Color != source.Color || !bytes.Equal(output.TransformChain, source.TransformChain) ||
		(output.Alpha == nil) != (source.Alpha == nil) ||
		output.Alpha != nil && *output.Alpha != *source.Alpha ||
		output.Pixels.ID == source.Pixels.ID || output.Pixels.SHA256 == source.Pixels.SHA256 {
		return zero, v2input.ErrInvalid
	}
	report, err := ParseImageReport(docs.Report, root.Report)
	if err != nil || report.Status != "review" || report.Operation != operation ||
		report.SkippedCount != 0 || report.OutsideMaskMaxError != 0 ||
		report.Identity != nil || report.Transform != nil || len(report.Registrations) != 0 ||
		len(report.ParentReports) != 0 {
		return zero, v2input.ErrInvalid
	}
	if operation == "image.reflection" {
		if report.Reason == nil || *report.Reason != "reflection_model_proposal" ||
			len(report.Sources) != 1 || report.Sources[0] != params.Base {
			return zero, v2input.ErrInvalid
		}
	} else if params.Mode == "generative" {
		if report.Reason == nil || *report.Reason != "synthetic_glare_proposal" ||
			len(report.Sources) != 1 || report.Sources[0] != params.Base {
			return zero, v2input.ErrInvalid
		}
	} else {
		if report.Reason == nil || (*report.Reason != "donor_review" &&
			*report.Reason != "COLOR_MATCH_UNAVAILABLE") || report.ClippingFraction != 0 ||
			len(report.Sources) != 2 || report.Sources[0] != params.Base ||
			report.Sources[1] != *params.Donor {
			return zero, v2input.ErrInvalid
		}
	}
	changed, err := review.ParseMaskRef(docs.Changed)
	if err != nil || changed.Canvas != source.Canvas ||
		!bytes.Equal(changed.TransformChain, source.TransformChain) ||
		changed.Selector != "changed" || changed.Protected || changed.ModelLockID != nil ||
		changed.ObjectID != nil || changed.Artifact.ID == output.Pixels.ID {
		return zero, v2input.ErrInvalid
	}
	candidate := RestorationReviewCandidate{Result: resultRef, Image: root.WorkingImage,
		Report: root.Report, Changed: root.ChangedMask,
		Pixels:        v2input.Ref{ArtifactID: output.Pixels.ID, SHA256: output.Pixels.SHA256},
		ChangedPixels: v2input.Ref{ArtifactID: changed.Artifact.ID, SHA256: changed.Artifact.SHA256}}
	generatedRequired := operation == "image.reflection" || params.Mode == "generative"
	if generatedRequired {
		if root.GeneratedMask == nil || len(docs.Generated) < 2 ||
			restorationExactJSON(*root.GeneratedMask, docs.Generated) != nil {
			return zero, v2input.ErrInvalid
		}
		generated, e := review.ParseMaskRef(docs.Generated)
		if e != nil || generated.Canvas != source.Canvas ||
			!bytes.Equal(generated.TransformChain, source.TransformChain) ||
			generated.Selector != "generated" || generated.Protected ||
			generated.ModelLockID == nil || *generated.ModelLockID != params.ModelLockSHA ||
			generated.ObjectID != nil || generated.Artifact.ID == changed.Artifact.ID ||
			generated.Artifact.ID == output.Pixels.ID {
			return zero, v2input.ErrInvalid
		}
		pixel := v2input.Ref{ArtifactID: generated.Artifact.ID, SHA256: generated.Artifact.SHA256}
		candidate.Generated = root.GeneratedMask
		candidate.GeneratedPixels = &pixel
	} else if root.GeneratedMask != nil || len(docs.Generated) != 0 {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return candidate, nil
}
