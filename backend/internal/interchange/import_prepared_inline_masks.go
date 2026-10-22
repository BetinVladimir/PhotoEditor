package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func inlinePreparedMasks(raw []byte) ([][]byte, error) {
 if isImportConservativeParams("Params",raw) {
  params,_,err:=decodeImportConservativeParams(raw);if err!=nil{return nil,err}
  result:=make([][]byte,0,len(params.Protected))
  for _,mask:=range params.Protected{result=append(result,append([]byte(nil),mask...))}
  return result,nil
 }
 var report struct{
  Algorithm string `json:"algorithm"`
  Protection json.RawMessage `json:"protection"`
  Params struct{Image v2input.Ref `json:"image"`;Permanent json.RawMessage `json:"permanent"`;Protections []json.RawMessage `json:"protections"`;Faces []struct{Skin json.RawMessage `json:"skin"`;Manual json.RawMessage `json:"manual"`} `json:"faces"`} `json:"params"`
 }
 if json.Unmarshal(raw,&report)==nil&&report.Algorithm=="defect_dog_v1" {
  if err:=validateImportDefectProposeReport(raw,report.Params.Image);err!=nil{return nil,err}
  masks:=[]json.RawMessage{report.Protection,report.Params.Permanent}
  masks=append(masks,report.Params.Protections...)
  for _,face:=range report.Params.Faces{masks=append(masks,face.Skin,face.Manual)}
  result:=[][]byte{}
  for _,mask:=range masks{if len(mask)!=0&&string(mask)!="null"{result=append(result,append([]byte(nil),mask...))}}
  return result,nil
 }
	if isImportStatsParams("Params", raw) {
		p, err := decodeImportStatsParams(raw)
		if err != nil {
			return nil, err
		}
		return [][]byte{append([]byte(nil), p.Person...)}, nil
	}
	var doc struct {
		Masks []struct {
			Mask json.RawMessage `json:"mask"`
		} `json:"mask_refs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Masks) > 128 {
		return nil, v2input.ErrInvalid
	}
	result := [][]byte{}
	for _, item := range doc.Masks {
		body, err := canonical.Bytes(item.Mask)
		if err != nil {
			return nil, err
		}
		if _, err := review.ParseMaskRef(body); err != nil {
			return nil, err
		}
		result = append(result, body)
	}
	return result, nil
}
func validateInlinePreparedMaskWitness(parent, maskRaw []byte, entry v2input.Entry) error {
	masks, err := inlinePreparedMasks(parent)
	if err != nil {
		return err
	}
	found := false
	for _, body := range masks {
		mask, err := review.ParseMaskRef(body)
		if err != nil {
			return err
		}
		if mask.Artifact.ID == entry.Ref.ArtifactID {
			if mask.Artifact.SHA256 != entry.Ref.SHA256 || !bytes.Equal(body, maskRaw) {
				return v2input.ErrInvalid
			}
			found = true
		}
	}
	if !found {
		return v2input.ErrInvalid
	}
	return nil
}
func (v ImportPreparedMaskDocumentValidator) rememberInlineOverrideMasks(ctx context.Context, entry v2input.Entry, raw []byte) error {
	if v.Witnesses == nil || v.Qualifier == nil || rawSHA(raw) != entry.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	parent := append([]byte(nil), raw...)
	masks, err := inlinePreparedMasks(parent)
	if err != nil {
		return err
	}
	for _, body := range masks {
		if err := ctx.Err(); err != nil {
			return err
		}
		mask, err := review.ParseMaskRef(body)
		if err != nil {
			return err
		}
		qualifiedCtx := ctx
		if entry.Contract == "Override" {
			qualifiedCtx = withPreparedOverrideMaskScope(ctx, entry.Ref, v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256})
		}
		if err := v.Qualifier.QualifyImportedMask(qualifiedCtx, mask, v.Binding); err != nil {
			return err
		}
		canvas, err := canonical.Hash(mask.Canvas)
		if err != nil || canvas != v.Binding.CanvasHash {
			return v2input.ErrInvalid
		}
		child := v2input.Entry{Contract: "MaskPixels", Ref: v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}}
		if !validImportRef(child.Ref) {
			return v2input.ErrInvalid
		}
		witness := preparedMaskWitness{parent: entry.Ref, contract: entry.Contract, body: append([]byte(nil), parent...), maskBody: append([]byte(nil), body...), child: child}
		if err := v.Witnesses.rememberQualified(witness); err != nil {
			return err
		}
	}
	return ctx.Err()
}
