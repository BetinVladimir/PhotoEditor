package semanticproposal

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Reconciles the frozen transport with retained file metadata and every native
// Params dependency. This is a private consistency check, not SQL authorization.
func validateDefectAdmissionClosure(filesBody, contentBody, registrationBody json.RawMessage) error {
	var files []struct {
		Contract  string     `json:"contract"`
		ID        uuid.UUID  `json:"artifact_id"`
		SHA       string     `json:"sha256"`
		ParentID  *uuid.UUID `json:"parent_id"`
		ParentSHA *string    `json:"parent_sha"`
	}
	var content defectActivationContent
	var registration defectRegistrationBinding
	if json.Unmarshal(filesBody, &files) != nil || len(files) < 2 || len(files) > 4097 || json.Unmarshal(contentBody, &content) != nil || json.Unmarshal(registrationBody, &registration) != nil {
		return v2input.ErrInvalid
	}
	if registration.Params != content.Params {
		return v2input.ErrInvalid
	}
	var rows []v2input.Entry
	var direct []v2input.Ref
	if json.Unmarshal(registration.Transport.Rows, &rows) != nil || len(rows) != registration.Transport.Count || len(rows) < 3 || len(rows) > 4097 || json.Unmarshal(registration.Transport.Direct, &direct) != nil || len(direct) != 3 {
		return v2input.ErrInvalid
	}
	for index, row := range rows {
		if index > 0 && rows[index-1].Ref.ArtifactID.String() >= row.Ref.ArtifactID.String() {
			return v2input.ErrInvalid
		}
	}
	entriesSHA, err := canonical.Hash(rows)
	if err != nil || entriesSHA != registration.Transport.EntriesSHA {
		return fmt.Errorf("defect closure entries digest: %w", v2input.ErrInvalid)
	}
	inputDigest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{registration.TaskID, "defects.propose", "1", rows})
	if err != nil || inputDigest != registration.InputDigest {
		return fmt.Errorf("defect closure task digest: %w", v2input.ErrInvalid)
	}
	catalog := make(map[uuid.UUID]v2input.Entry, len(files))
	for _, file := range files {
		if file.ID == uuid.Nil {
			return v2input.ErrInvalid
		}
		if _, found := catalog[file.ID]; found {
			return v2input.ErrInvalid
		}
		contract := defectNativeInputContract(file.Contract)
		catalog[file.ID] = v2input.Entry{Ref: v2input.Ref{ArtifactID: file.ID, SHA256: file.SHA}, Contract: contract}
	}
	seen := make(map[uuid.UUID]v2input.Entry, len(rows))
	inputs := make([]review.ArtifactRef, 0, len(rows)-1)
	paramsCount := 0
	for _, row := range rows {
		if _, found := seen[row.Ref.ArtifactID]; found {
			return v2input.ErrInvalid
		}
		seen[row.Ref.ArtifactID] = row
		if row.Ref == content.Params {
			if row.Contract != "Params" {
				return v2input.ErrInvalid
			}
			paramsCount++
			continue
		}
		if row.Contract == "Params" || catalog[row.Ref.ArtifactID] != row {
			return v2input.ErrInvalid
		}
		inputs = append(inputs, review.ArtifactRef{ID: row.Ref.ArtifactID, SHA256: row.Ref.SHA256})
	}
	// Selected WorkingImage/MaskRef documents require their retained array children.
	// Physical content and parent authority are checked by the existing locks.
	for _, file := range files {
		if (file.ParentID == nil) != (file.ParentSHA == nil) {
			return v2input.ErrInvalid
		}
		if file.ParentID == nil {
			continue
		}
		parent, selected := seen[*file.ParentID]
		if !selected || (parent.Contract != "WorkingImage" && parent.Contract != "MaskRef") {
			continue
		}
		if defectNativeInputContract(file.Contract) != "NpyArray" {
			continue
		}
		if parent.Ref.SHA256 != *file.ParentSHA || seen[file.ID] != catalog[file.ID] {
			return fmt.Errorf("defect closure retained array dependency %s: %w", file.Contract, v2input.ErrInvalid)
		}
	}
	if paramsCount != 1 {
		return v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage([]byte(content.WorkingImageText))
	if err != nil {
		return v2input.ErrInvalid
	}
	source := image.Source(review.ArtifactRef{ID: content.WorkingImage.ArtifactID, SHA256: content.WorkingImage.SHA256})
	if err := review.VerifyDefectProposalDeclaredInputs(review.ArtifactRef{ID: content.Params.ArtifactID, SHA256: content.Params.SHA256}, []byte(content.ParamsText), source, inputs); err != nil {
		return v2input.ErrInvalid
	}
	required := map[v2input.Ref]bool{content.Params: true, content.WorkingImage: true, {ArtifactID: source.Pixels.ID, SHA256: source.Pixels.SHA256}: true}
	if len(required) != 3 {
		return v2input.ErrInvalid
	}
	for _, ref := range direct {
		if !required[ref] {
			return v2input.ErrInvalid
		}
		delete(required, ref)
	}
	if len(required) != 0 {
		return v2input.ErrInvalid
	}
	return nil
}

// Catalog array roles share the native NPY transport contract. Preserve semantic
// document contracts for separate producer/content validation.
func defectNativeInputContract(contract string) string {
	switch contract {
	case "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarp", "DenseWarpField":
		return "NpyArray"
	default:
		return contract
	}
}
