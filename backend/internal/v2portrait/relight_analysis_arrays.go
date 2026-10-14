package v2portrait

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type RelightAnalysisArraySpec struct {
	Ref           v2input.Ref
	Role          string
	Width, Height int64
	Channels      int
	Alpha         bool
}

// AnalysisSourceArrays keeps occurrences rather than deduplicating roles: one
// shared artifact must satisfy every manifest edge's dimensions and type.
func AnalysisSourceArrays(request RelightAnalysisPlanRequest) ([]RelightAnalysisArraySpec, error) {
	image, err := VerifyDonorWorkingImage(request.Base, request.SourceRaw)
	if err != nil {
		return nil, err
	}
	specs := []RelightAnalysisArraySpec{{Ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, Role: "WorkingPixels", Width: image.Canvas.Width, Height: image.Canvas.Height, Channels: 3}}
	if image.Alpha != nil {
		specs = append(specs, RelightAnalysisArraySpec{Ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, Role: "WorkingAlpha", Width: image.Canvas.Width, Height: image.Canvas.Height, Alpha: true})
	}
	var chain struct {
		Steps []struct {
			Kind    string        `json:"kind"`
			To      review.Canvas `json:"to_canvas"`
			Inverse *v2input.Ref  `json:"inverse_field"`
		} `json:"steps"`
	}
	if json.Unmarshal(image.TransformChain, &chain) != nil {
		return nil, v2input.ErrInvalid
	}
	for i, step := range chain.Steps {
		if step.Kind != "dense_warp" {
			continue
		}
		if step.Inverse == nil || !validRelightTransportRef(*step.Inverse) || step.To.Width < 1 || step.To.Height < 1 || step.To.Width > 200000000/step.To.Height {
			return nil, v2input.ErrInvalid
		}
		specs = append(specs, RelightAnalysisArraySpec{Ref: *step.Inverse, Role: "InverseField/" + strconv.Itoa(i), Width: step.To.Width, Height: step.To.Height, Channels: 2})
	}
	if len(specs) > 61 {
		return nil, v2input.ErrInvalid
	}
	return specs, nil
}

// VerifyRelightAnalysisArray streams SHA-bound NPY data with bounded memory.
// Caller owns reader closure. This validates content, not catalog permission.
func VerifyRelightAnalysisArray(ctx context.Context, spec RelightAnalysisArraySpec, object objectstore.Object, reader io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil || !validRelightTransportRef(spec.Ref) || object.SHA256 != spec.Ref.SHA256 || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || spec.Width < 1 || spec.Height < 1 || spec.Width > 200000000/spec.Height {
		return v2input.ErrInvalid
	}
	if object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream" {
		return v2input.ErrInvalid
	}
	components := int64(spec.Channels)
	if spec.Alpha {
		if spec.Role != "WorkingAlpha" || spec.Channels != 0 {
			return v2input.ErrInvalid
		}
		components = 1
	} else if spec.Role == "WorkingPixels" {
		if spec.Channels != 3 {
			return v2input.ErrInvalid
		}
	} else if len(spec.Role) > 13 && spec.Role[:13] == "InverseField/" {
		if spec.Channels != 2 || object.ContentType != "application/x-npy" {
			return v2input.ErrInvalid
		}
	} else {
		return v2input.ErrInvalid
	}
	if object.Bytes < 1 || object.Bytes > spec.Width*spec.Height*components*4+65536 {
		return v2input.ErrInvalid
	}
	var err error
	if spec.Alpha {
		err = masknpy.VerifyAlphaNPY(ctx, reader, object.Bytes, object.SHA256, spec.Width, spec.Height)
	} else {
		err = masknpy.VerifyFloat32ArrayNPY(ctx, reader, object.Bytes, object.SHA256, spec.Width, spec.Height, spec.Channels, false)
	}
	if errors.Is(err, masknpy.ErrInvalid) {
		return v2input.ErrInvalid
	}
	return err
}
