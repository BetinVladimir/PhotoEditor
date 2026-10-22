package semanticreview

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// Shared cleanupReadRow/Rows are column scanners only. All documents, operation
// names, expected task identity and query behavior below belong to removal.
type removalLinkageQuery struct {
 t *testing.T
 presentation RemovalPreviewPresentation
 ref v2input.Ref
 bodies map[string][]byte
 task,result,replacement uuid.UUID
 rows,queries int
 mode string
}
func (q *removalLinkageQuery) source(ref v2input.Ref,media string) PlanSource {
 return PlanSource{Ref:ref,Bucket:"fixture",Key:ref.ArtifactID.String(),Version:"immutable-v1",Bytes:int64(len(q.bodies[ref.ArtifactID.String()])),MediaType:media}
}
func (q *removalLinkageQuery) QueryRow(_ context.Context,query string,args ...any) pgx.Row {
 q.rows++
 if !strings.Contains(query,"photo.semantic_removal_preview_presentation_source(") || len(args)!=6 { q.t.Fatal("foreign query",query) }
 result:=q.result
 if q.rows>=3 {
  // Keep the replacement stable across both final lookups: the inner
  // document reader succeeds; the outer linkage must reject the old result.
  if q.mode=="changed_result" { result=q.replacement }
  if q.mode=="revoked" { return cleanupReadRow{err:pgx.ErrNoRows} }
 }
 s:=q.source(q.ref,"application/json")
 return cleanupReadRow{values:[]any{q.task,result,1,int64(1),s.Ref.ArtifactID,s.Ref.SHA256,s.Bucket,s.Key,s.Version,s.Bytes,s.MediaType}}
}
func (q *removalLinkageQuery) Query(_ context.Context,query string,args ...any)(pgx.Rows,error) {
 q.queries++
 if !strings.Contains(query,"photo.semantic_removal_preview_presentation_sidecars(") || len(args)!=7 { q.t.Fatal("foreign query",query) }
 p:=q.presentation
 if q.mode=="sidecar_alias" {p.Protected=p.After}
 if q.mode=="sidecar_hash" {p.Protected.SHA256=p.Before.SHA256}
 refs:=[]v2input.Ref{p.NumericalEvidence,p.WorkingImage,p.Pixels,p.Before,p.After,p.Affected,p.Protected}
 roles:=[]string{"preview_evidence","working_image","pixels","before","after","affected","protected"}
 rows:=&cleanupReadRows{}
 for i,ref:=range refs {
  media:="application/json"
  if i==2 {media="application/x-npy"};if i>=3 {media="image/png"}
  s:=q.source(ref,media)
  if q.queries==2 && q.mode=="changed_sidecar" && i==6 { s.Version="replacement-v2" }
  rows.values=append(rows.values,[]any{roles[i],ref.ArtifactID,ref.SHA256,s.Bucket,s.Key,s.Version,s.Bytes,s.MediaType})
 }
 return rows,nil
}
type removalLinkageStore struct { q *removalLinkageQuery; reads int }
func (s *removalLinkageStore) Open(_ context.Context,o objectstore.Object)(io.ReadCloser,error) {
 s.reads++
 raw:=s.q.bodies[o.Key]
 if s.q.mode=="evidence_corrupt" && o.Key==s.q.presentation.NumericalEvidence.ArtifactID.String() {raw=append([]byte(nil),raw...);raw[0]='!'}
 if o.VersionID!="immutable-v1" || int64(len(raw))!=o.Bytes {s.q.t.Fatal("unbound object",o)}
 return io.NopCloser(bytes.NewReader(raw)),nil
}
func TestRemovalCompleteDocumentLinkageRechecksCanonicalProducer(t *testing.T) {
 for _,mode:=range []string{"valid","changed_result","changed_sidecar","revoked","evidence_corrupt","sidecar_alias","sidecar_hash"} {
  t.Run(mode,func(t *testing.T) {
   p:=RemovalPreviewParams{ExposurePreviewParams:previewParamsFixture(),Kind:"remove_object",ModelID:"big_lama",ContextPixels:128,FeatherPixels:8}
   bodies:=map[string][]byte{}
   put:=func(raw []byte)v2input.Ref {sum:=sha256.Sum256(raw);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])};bodies[ref.ArtifactID.String()]=raw;return ref}
   image,pixels:=put([]byte(`{"image":1}`)),put([]byte("npy-metadata-only"))
   e:=RemovalPreviewEvidence{SchemaVersion:1,Params:p,WorkingImage:image,Pixels:pixels,Algorithm:"big_lama_work_srgb_delta_v1",ProtectedPixelsUnchanged:true,OutsideMaskUnchanged:true,ChangedPixels:4,ROI:[]int64{0,0,2,2}}
   evidenceRaw,err:=canonical.Marshal(e);if err!=nil {t.Fatal(err)}
   presentation:=RemovalPreviewPresentation{SchemaVersion:1,Params:p,NumericalEvidence:put(evidenceRaw),WorkingImage:image,Pixels:pixels,
    Before:put([]byte("before")),After:put([]byte("after")),Affected:put([]byte("affected")),Protected:put([]byte("protected")),Width:2,Height:2,
    ICCLockSHA:p.Plan.SHA256,SRGBSHA:p.Plan.SHA256,Resize:"area_linear_premult_v1",Overlay:"max_coverage_ceil_alpha_v1"}
   raw,err:=canonical.Marshal(presentation);if err!=nil {t.Fatal(err)}
   ref:=put(raw);run:=uuid.New();task:=uuid.NewSHA1(run,[]byte("semantic.preview_remove_object:"+p.TargetID.String()))
   q:=&removalLinkageQuery{t:t,presentation:presentation,ref:ref,bodies:bodies,task:task,result:uuid.New(),replacement:uuid.New(),mode:mode}
   store:=&removalLinkageStore{q:q}
   got,err:=ReadRemovalPreviewPresentationLinkage(context.Background(),q,store,"fixture",auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},run,p,p.Plan.SHA256,p.Plan.SHA256)
   if mode=="valid" {
    if err!=nil || got.Document.TaskID!=task || got.Document.ResultID!=q.result || got.Evidence.Evidence.Params.TargetID!=p.TargetID || got.Evidence.Evidence.Params.Kind!="remove_object" || q.rows!=4 || q.queries!=2 || store.reads!=3 {t.Fatal("incomplete own linkage",got,err,q.rows,q.queries,store.reads)}
   } else if err==nil || got.Document.TaskID!=uuid.Nil {t.Fatal("stale linkage escaped",mode,got,err)}
  })
 }
}
