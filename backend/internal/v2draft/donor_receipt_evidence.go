package v2draft

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type donorManualAdmissionWire struct {
	Guard    v2input.Ref  `json:"guard_pixel"`
	Original v2input.Ref  `json:"original_params"`
	Root     v2input.Ref  `json:"root"`
	Target   v2input.Ref  `json:"target_pixel"`
	Vectors  *v2input.Ref `json:"vectors"`
}

// Structural receipt evidence validation only. Original bytes and current SQL
// authority remain mandatory; a parsed receipt never authorizes execution.
func validateDonorReceiptEvidence(manualRaw, qualificationRaw []byte) error {
	var pin v2portrait.DonorQualificationPin
	if json.Unmarshal(qualificationRaw, &pin) != nil {
		return v2input.ErrInvalid
	}
	exact, err := pin.Compile()
	normal, e := canonical.Bytes(qualificationRaw)
	if err != nil || e != nil || !bytes.Equal(exact, normal) {
		return v2input.ErrInvalid
	}
	var manual donorManualAdmissionWire
	if json.Unmarshal(manualRaw, &manual) != nil {
		return v2input.ErrInvalid
	}
	exact, err = canonical.Marshal(manual)
	normal, e = canonical.Bytes(manualRaw)
	if err != nil || e != nil || !bytes.Equal(exact, normal) {
		return v2input.ErrInvalid
	}
	refs := []v2input.Ref{manual.Guard, manual.Original, manual.Root, manual.Target, pin.Report, pin.Calibration}
	if manual.Vectors != nil {
		refs = append(refs, *manual.Vectors)
	}
	seen := make(map[uuid.UUID]bool, len(refs))
	for _, ref := range refs {
		if !isDocumentRef(ref) || seen[ref.ArtifactID] {
			return v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	return nil
}
