package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestSQLDefectLabelAuthorityRejectsInvalidOccurrenceOrMissingInstallation(t *testing.T) {
 for _,mode:=range []string{"valid_without_db","parent_contract","array_contract","hash","edge","source","cancelled"} {t.Run(mode,func(t *testing.T){
  array:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("labels"))},Contract:"DefectLabelMap"}
  image:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))}
  raw,err:=canonical.Marshal(map[string]any{"image":image,"label_map":array.Ref});if err!=nil {t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"DefectCandidates"}
  ctx:=context.Background()
  switch mode {case "parent_contract":parent.Contract="MaskRef";case "array_contract":array.Contract="MaskPixels";case "hash":raw=[]byte(`{}`);case "edge":array.Ref.ArtifactID=uuid.New();case "source":raw=[]byte(`{"image":null,"label_map":null}`);parent.Ref.SHA256=rawSHA(raw);case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if err:=(SQLExportDocumentCatalog{}).QualifyExportDefectLabel(ctx,parent,raw,array);err==nil {t.Fatal("unqualified label occurrence accepted")}
 })}
}
