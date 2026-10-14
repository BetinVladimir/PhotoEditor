package interchange

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type ImportDefectLabelOccurrence struct {
	Original v2input.Ref `json:"original"`
	Parent   v2input.Ref `json:"parent"`
	Width    int64       `json:"width"`
	Height   int64       `json:"height"`
}

func validateImportDefectLabelOccurrence(scope ImportPreparedScope, ref v2input.Ref) error {
	if scope.DefectLabel == nil {
		return nil
	}
	item := *scope.DefectLabel
	if scope.Contract != "DefectLabelMap" || scope.Workflow == uuid.Nil || scope.Entry == uuid.Nil || scope.Warp != nil || scope.LUT != nil || scope.Background != nil || !validImportRef(ref) || !validImportRef(item.Original) || !validImportRef(item.Parent) || item.Parent.ArtifactID == item.Original.ArtifactID || item.Original.SHA256 != ref.SHA256 || item.Width < 1 || item.Height < 1 || item.Width > 200000000 || item.Height > 200000000 || item.Width*item.Height > 200000000 {
		return v2input.ErrInvalid
	}
	expected := uuid.NewSHA1(scope.Workflow, []byte("interchange:dependency:"+scope.Entry.String()+":DefectLabelMap:"+item.Original.ArtifactID.String()))
	if ref.ArtifactID != expected {
		return v2input.ErrInvalid
	}
	return nil
}
