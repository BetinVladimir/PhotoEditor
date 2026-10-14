package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestExportParamsOperationSQLRequiresDatabaseAndHonorsCancellation(t *testing.T){
 raw:=[]byte(`{"image":{"artifact_id":"00000000-0000-4000-8000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"person":{}}`)
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"Params"}
 source:=SQLExportDocumentCatalog{}
 evidence,err:=source.LookupExportParamsOperation(context.Background(),entry,raw)
 if !errors.Is(err,v2input.ErrInvalid)||evidence!=(ExportParamsOperationEvidence{}){t.Fatal("uninstalled SQL source accepted Params",evidence,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 evidence,err=source.LookupExportParamsOperation(ctx,entry,raw)
 if !errors.Is(err,context.Canceled)||evidence!=(ExportParamsOperationEvidence{}){t.Fatal("cancelled SQL lookup returned authority",evidence,err)}
}
