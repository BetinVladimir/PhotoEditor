package review

import "errors"

// LabelBinding exposes only immutable pins needed for physical verification.
// It does not expose decisions or grant catalogue/source authorization.
type DefectLabelBinding struct {
 Proposal, Labels ArtifactRef
 Width, Height int64
}

func (p VerifiedProposal) LabelBinding() (DefectLabelBinding,error) {
 if !p.ref.valid() || !p.document.LabelMap.valid() || p.document.Canvas.Width<1 || p.document.Canvas.Height<1 {
  return DefectLabelBinding{},errors.New("invalid defect label binding")
 }
 return DefectLabelBinding{Proposal:p.ref,Labels:p.document.LabelMap,Width:p.document.Canvas.Width,Height:p.document.Canvas.Height},nil
}
