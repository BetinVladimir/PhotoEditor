package interchange

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type ImportLUTOccurrence struct {
	Original v2input.Ref `json:"original"`
	Parent   v2input.Ref `json:"parent"`
	Size     int         `json:"size"`
}

func validateImportLUTOccurrence(scope ImportPreparedScope, ref v2input.Ref) error {
	if scope.LUT == nil {
		return nil
	}
	item := *scope.LUT
	if scope.Contract != "AbsoluteLUTArray" || scope.Workflow == uuid.Nil || scope.Entry == uuid.Nil || scope.Warp != nil || scope.Background != nil || !validImportRef(ref) || !validImportRef(item.Original) || !validImportRef(item.Parent) || item.Parent.ArtifactID == item.Original.ArtifactID || item.Original.SHA256 != ref.SHA256 || (item.Size != 33 && item.Size != 65) {
		return v2input.ErrInvalid
	}
	expected := uuid.NewSHA1(scope.Workflow, []byte("interchange:dependency:"+scope.Entry.String()+":AbsoluteLUTArray:"+item.Original.ArtifactID.String()))
	if ref.ArtifactID != expected {
		return v2input.ErrInvalid
	}
	return nil
}
