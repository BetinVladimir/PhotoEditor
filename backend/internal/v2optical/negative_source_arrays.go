package v2optical

import (
 "context"
 "encoding/json"
 "io"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// NegativeSourceArraySpec retains occurrence dimensions; shared artifacts must
// satisfy every source edge. Array numerics use the common float32 verifier.
type NegativeSourceArraySpec = PerspectiveSourceArraySpec

func NegativeSourceArrays(params NegativeParams, sourceRaw []byte) ([]NegativeSourceArraySpec, error) {
 image, err := VerifyNegativeWorkingImage(params, sourceRaw)
 if err != nil { return nil, err }
 var chain struct { Steps []struct {
  Kind string `json:"kind"`
  Inverse *v2input.Ref `json:"inverse_field"`
 } `json:"steps"` }
 if json.Unmarshal(image.TransformChain, &chain) != nil || len(chain.Steps)>128 { return nil, v2input.ErrInvalid }
 // Reject hidden inverse edges that the transport inventory would include but
 // the dense-warp physical verifier would otherwise omit.
 for _, step := range chain.Steps {
  if (step.Kind=="dense_warp") != (step.Inverse!=nil) { return nil, v2input.ErrInvalid }
 }
 return sourceImageArraySpecs(image)
}

// VerifyNegativeSourceArray checks original SHA-bound NPY bytes without clipping
// finite HDR RGB or inverse coordinates. Alpha alone must remain in [0,1].
func VerifyNegativeSourceArray(ctx context.Context, spec NegativeSourceArraySpec, object objectstore.Object, reader io.Reader) error {
 return VerifyPerspectiveSourceArray(ctx, spec, object, reader)
}
