package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

func TestArchiveBeginInspectionCancellationBeforeDependencies(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 got,err:=(SQLArchiveBeginInspector{}).Inspect(ctx,auth.Identity{},uuid.New(),ArchiveUploadIntent{},uuid.New())
 if err!=context.Canceled||got!=(ArchiveBeginInspection{}){t.Fatal("cancelled inspection reached dependencies",got,err)}
}
