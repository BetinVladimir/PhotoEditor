package workflow

import (
 "context"
 "errors"
 "io"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/masknpy"
)
func TestDownloadSourceFailurePreservesTransientErrors(t *testing.T){
 master:=pendingDownloadMaster{ordinal:3,object:inspectedObject{ID:uuid.New(),SHA:downloadDigest("master")}}
 for _,err:=range []error{context.Canceled,context.DeadlineExceeded,io.ErrUnexpectedEOF,errors.New("transport disconnected")}{
  var observation *downloadSourceFailure
  got:=downloadPhysicalSourceFailure(master,err)
  if got!=err||errors.As(got,&observation){t.Fatal("transient became terminal",got)}
 }
 for _,err:=range []error{objectstore.ErrMissing,objectstore.ErrIntegrity,masknpy.ErrInvalid}{
  var observation *downloadSourceFailure
  if !errors.As(downloadPhysicalSourceFailure(master,err),&observation)||observation.ordinal!=master.ordinal||observation.master!=master.object.ID||observation.sha!=master.object.SHA{t.Fatal("unbound physical observation")}
 }
}
