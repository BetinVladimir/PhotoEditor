package semanticreview

import (
 "bytes"
 "context"
 "io"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)

// Physical removal documents are independent from cleanup. Only generic
// transport scanners and the removal document-query fixture are reused.
type removalPhysicalStore struct {
 q *removalLinkageQuery
 mode string
 cancel context.CancelFunc
 opened, closed int
}
type removalPhysicalStream struct { io.Reader; store *removalPhysicalStore }
func (s *removalPhysicalStream) Close() error { s.store.closed++; return nil }
func (s *removalPhysicalStore) Open(_ context.Context, o objectstore.Object) (io.ReadCloser,error) {
 raw,ok:=s.q.bodies[o.Key]
 if !ok || o.VersionID!="immutable-v1" || o.Bytes!=int64(len(raw)) || o.SHA256!=physicalFixtureSHA(raw) {s.q.t.Fatal("unbound physical object",o)}
 s.opened++
 p:=s.q.presentation
 refs:=map[string]string{"pixels":p.Pixels.ArtifactID.String(),"before":p.Before.ArtifactID.String(),"after":p.After.ArtifactID.String(),"affected":p.Affected.ArtifactID.String(),"protected":p.Protected.ArtifactID.String()}
 for role,key:=range refs {
  if o.Key==key && s.mode=="corrupt_"+role {raw=append([]byte(nil),raw...);raw[len(raw)-1]^=1}
 }
 if o.Key==p.Protected.ArtifactID.String() {
  switch s.mode {
  case "changed_result":s.q.result=s.q.replacement
  case "revoked":s.q.mode="revoked"
  case "cancel":s.cancel()
  }
 }
 return &removalPhysicalStream{Reader:bytes.NewReader(raw),store:s},nil
}
func TestRemovalPhysicalReaderCompletePipeline(t *testing.T) {
 for _,mode:=range []string{"valid","corrupt_pixels","corrupt_before","corrupt_after","corrupt_affected","corrupt_protected","changed_result","revoked","cancel","dimensions_before","dimensions_after","dimensions_affected","dimensions_protected","icc_before","icc_after","color_affected","color_protected"} {
  t.Run(mode,func(t *testing.T) {
   d:=buildRemovalPhysicalDocuments(t,mode)
   run:=uuid.New()
   q:=&removalLinkageQuery{t:t,presentation:d.presentation,ref:d.presentationRef,bodies:d.bodies,task:uuid.NewSHA1(run,[]byte("semantic.preview_remove_object:"+d.params.TargetID.String())),result:uuid.New(),replacement:uuid.New()}
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   store:=&removalPhysicalStore{q:q,mode:mode,cancel:cancel}
   got,err:=ReadPhysicalRemovalPreview(ctx,q,store,"fixture",auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},run,d.params,d.source,d.params.Plan.SHA256,d.srgbSHA)
   if mode=="valid" {
    if err!=nil || !got.verified || got.linkage.Document.TaskID!=q.task || got.linkage.Document.ResultID!=q.result || !bytes.Equal(got.image.ImageBytes,d.bodies[d.presentation.WorkingImage.ArtifactID.String()]) || q.rows!=12 || q.queries!=6 {t.Fatal("incomplete physical removal",err,q.rows,q.queries)}
   } else if err==nil || got.verified || got.linkage.Document.TaskID!=uuid.Nil || len(got.image.ImageBytes)!=0 {t.Fatal("partial physical result escaped",mode,err)}
   if store.opened!=store.closed {t.Fatal("physical stream leaked",store.opened,store.closed)}
  })
 }
}
