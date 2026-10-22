package v2render

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// verifyLensResultGeometry binds the reviewed result to its accepted
// source and to the exact final half-pixel-center transform in WorkingImage.
func verifyLensResultGeometry(binding AdoptedLensBinding, result v2draft.ImageResultDocument, sourceRaw []byte, documents map[string][]byte) error {
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return err
	}
	sourceHash, err := canonical.Hash(source)
	if err != nil || sourceHash != binding.Base.Source.SHA256 {
		return v2input.ErrInvalid
	}
	sourceCanvas, err := canonical.Hash(source.Canvas)
	if err != nil || sourceCanvas != binding.Base.CanvasHash {
		return v2input.ErrInvalid
	}
	if result.GeneratedMask != nil {
		return v2input.ErrInvalid
	}
	targetRaw := documents[result.WorkingImage.ArtifactID.String()]
	if result.WorkingImage == binding.Base.Source {
		targetRaw = sourceRaw
	}
	target, err := review.ParseWorkingImage(targetRaw)
	if err != nil {
		return err
	}
	if target.Color != source.Color || target.DType != "float32" || target.Canvas.SourceSHA256 != source.Canvas.SourceSHA256 {
		return v2input.ErrInvalid
	}
	mask, err := review.ParseMaskRef(documents[result.ChangedMask.ArtifactID.String()])
	if err != nil {
		return err
	}
	if mask.Canvas != target.Canvas || mask.Selector != "changed" || mask.Protected || mask.ObjectID != nil || mask.ModelLockID != nil {
		return v2input.ErrInvalid
	}
	maskChain, err := canonical.Bytes(mask.TransformChain)
	if err != nil {
		return err
	}
	targetChain, err := canonical.Bytes(target.TransformChain)
	if err != nil || !bytes.Equal(maskChain, targetChain) {
		return v2input.ErrInvalid
	}
	var report struct {
		Operation     string                          `json:"operation"`
		Status        string                          `json:"status"`
		Reason        *string                         `json:"reason"`
		Sources       []v2input.SessionVersionBinding `json:"sources"`
		Transform     *v2input.Ref                    `json:"transform"`
		Identity      *v2input.Ref                    `json:"identity"`
		Registrations []v2input.Ref                   `json:"registrations"`
	}
	if json.Unmarshal(documents[result.Report.ArtifactID.String()], &report) != nil || report.Operation != "optical.lens" || len(report.Sources) != 1 || report.Sources[0] != binding.Base || report.Identity != nil || len(report.Registrations) != 0 {
		return v2input.ErrInvalid
	}

 if report.Transform!=nil {return v2input.ErrInvalid}
 if result.WorkingImage==binding.Base.Source {
  if report.Status!="pass" || report.Reason!=nil {return v2input.ErrInvalid};return nil
 }
 if target.Alpha==nil || target.Canvas.Orientation!=1 || target.Canvas.Width!=source.Canvas.Width || target.Canvas.Height!=source.Canvas.Height ||
  !((report.Status=="pass" && report.Reason==nil)||(report.Status=="review" && report.Reason!=nil && *report.Reason=="uncovered_area")){return v2input.ErrInvalid}
 var before,after struct {Hash string `json:"hash"`;Steps []json.RawMessage `json:"steps"`}
 if json.Unmarshal(source.TransformChain,&before)!=nil || json.Unmarshal(target.TransformChain,&after)!=nil || len(after.Steps)!=len(before.Steps)+1{return v2input.ErrInvalid}
 for i:=range before.Steps {a,e:=canonical.Bytes(before.Steps[i]);b,e2:=canonical.Bytes(after.Steps[i]);if e!=nil||e2!=nil||!bytes.Equal(a,b){return v2input.ErrInvalid}}
 var step struct {Kind string `json:"kind"`;From review.Canvas `json:"from_canvas"`;To review.Canvas `json:"to_canvas"`;Invertibility string `json:"invertibility"`;Valid struct {X int64 `json:"x"`;Y int64 `json:"y"`;Width int64 `json:"width"`;Height int64 `json:"height"`} `json:"valid_region"`;Field v2input.Ref `json:"inverse_field"`;Centers string `json:"pixel_centers"`}
 last:=after.Steps[len(after.Steps)-1]
 var keys map[string]json.RawMessage
 if json.Unmarshal(last,&keys)!=nil || len(keys)!=7 || json.Unmarshal(last,&step)!=nil || step.Kind!="dense_warp" || step.From!=source.Canvas || step.To!=target.Canvas || step.Invertibility!="partial" || step.Centers!="half_pixel" || !validLayerArtifactRef(step.Field) || step.Valid.X!=0 || step.Valid.Y!=0 || step.Valid.Width!=target.Canvas.Width || step.Valid.Height!=target.Canvas.Height{return v2input.ErrInvalid}
 space,err:=canonical.Hash(map[string]any{"canvas":source.Canvas,"chain":before.Hash,"field":step.Field})
 if err!=nil || target.Canvas.SpaceID!="lens:"+space[:32]{return v2input.ErrInvalid}
 return nil
}
