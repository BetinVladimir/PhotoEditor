package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestImportApprovalSQLCatalogRejectsMissingConfigurationAndBinaryContracts(t *testing.T){
 catalog:=SQLImportApprovalCatalog{}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("bytes"))}
 for _,contract:=range []string{"StyleV2","StyleBackgroundAsset","MaskPixels","Artifact","WorkingImage"}{
  if _,err:=catalog.LookupImportDocument(context.Background(),v2input.Entry{Ref:ref,Contract:contract});err==nil{t.Fatal("accepted missing scope",contract)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=catalog.LookupImportDocument(ctx,v2input.Entry{Ref:ref,Contract:"StyleV2"});!errors.Is(err,context.Canceled){t.Fatal("lost cancellation",err)}
}
