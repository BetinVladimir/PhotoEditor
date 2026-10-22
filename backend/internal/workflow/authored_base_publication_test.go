package workflow

import (
 "context"
 "errors"
 "io"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
)

// These are publication-transport units, not SQL authority or physical tests.
// Exact SQL predecessor/adversarial tests remain a separate required lane.
type authoredPublicationRow struct {values []any;err error}
func(r authoredPublicationRow)Scan(target ...any)error{
 if r.err!=nil{return r.err};if len(target)!=len(r.values){return errors.New("unexpected test scan")}
 for i,value:=range r.values{switch p:=target[i].(type){case *bool:*p=value.(bool);case *uuid.UUID:*p=value.(uuid.UUID);default:return errors.New("unexpected test destination")}}
 return nil
}
type authoredPublicationDB struct {calls []string;id,token uuid.UUID;stage,complete,commit bool;fail error}
func(d *authoredPublicationDB)QueryRow(ctx context.Context,q string,args ...any)pgx.Row{
 d.calls=append(d.calls,q);if d.fail!=nil{return authoredPublicationRow{err:d.fail}}
 if strings.Contains(q,"stage_authored_render_base_receipt"){return authoredPublicationRow{values:[]any{d.stage}}}
 if strings.Contains(q,"reserve_authored_render_base_document"){return authoredPublicationRow{values:[]any{d.id,d.token,false}}}
 if strings.Contains(q,"complete_authored_render_base_document"){return authoredPublicationRow{values:[]any{d.complete}}}
 if strings.Contains(q,"commit_authored_render_base_phase"){return authoredPublicationRow{values:[]any{d.commit}}}
 return authoredPublicationRow{err:errors.New("unrecognized publication query")}
}
type authoredPublicationObjects struct {puts int;wrongVersion bool;cause error}
func(o *authoredPublicationObjects)PutImmutable(ctx context.Context,key string,reader io.Reader,size int64,sha,media string)(objectstore.Object,error){
 o.puts++;if o.cause!=nil{return objectstore.Object{},o.cause};raw,err:=io.ReadAll(reader);if err!=nil||int64(len(raw))!=size{return objectstore.Object{},objectstore.ErrIntegrity}
 version:="version-1";if o.wrongVersion{version="null"};return objectstore.Object{Key:key,VersionID:version,SHA256:sha,ContentType:media,Bytes:size},nil
}
func(o *authoredPublicationObjects)RecoverImmutable(context.Context,string,int64,string,string)(objectstore.Object,error){return objectstore.Object{},objectstore.ErrUnavailable}
func authoredPublicationFixture()(AuthoredBasePublication,ApplyLease,uuid.UUID,*authoredPublicationDB,*authoredPublicationObjects){
 db:=&authoredPublicationDB{id:uuid.New(),token:uuid.New(),stage:true,complete:true,commit:true};objects:=&authoredPublicationObjects{}
 return AuthoredBasePublication{DB:db,Objects:objects,Bucket:"artifacts"},ApplyLease{TenantID:uuid.New(),RunID:uuid.New(),Token:uuid.New(),ItemCount:1,Phase:"adopt"},uuid.New(),db,objects
}
func TestAuthoredBasePublicationOrdersPhysicalCompletionBeforeAppend(t *testing.T){
 p,l,parent,db,objects:=authoredPublicationFixture();ref,err:=p.Complete(context.Background(),l,parent,[]byte(`{"schema_version":1}`))
 if err!=nil||ref.ID!=db.id||objects.puts!=1||len(db.calls)!=4{t.Fatalf("publication %v %v",ref,err)}
 for i,name:=range []string{"stage_authored_render_base_receipt","reserve_authored_render_base_document","complete_authored_render_base_document","commit_authored_render_base_phase"}{if !strings.Contains(db.calls[i],name){t.Fatal("publication order changed")}}
}
func TestAuthoredBasePublicationStopsAtRejectedAuthority(t *testing.T){
 p,l,parent,db,objects:=authoredPublicationFixture();db.stage=false
 _,err:=p.Complete(context.Background(),l,parent,[]byte(`{"schema_version":1}`))
 if !errors.Is(err,artifactjson.ErrStale)||objects.puts!=0||len(db.calls)!=1{t.Fatal("rejected stage performed IO")}
}
func TestAuthoredBasePublicationDoesNotAppendUnverifiedObject(t *testing.T){
 p,l,parent,db,objects:=authoredPublicationFixture();objects.wrongVersion=true
 _,err:=p.Complete(context.Background(),l,parent,[]byte(`{"schema_version":1}`))
 if !errors.Is(err,objectstore.ErrIntegrity)||len(db.calls)!=2{t.Fatal("unverified object reached completion")}
}
func TestAuthoredBasePublicationRetainsFailedPhysicalCause(t *testing.T){
 p,l,parent,db,objects:=authoredPublicationFixture();cause:=errors.New("isolated transport failure");objects.cause=cause
 _,err:=p.Complete(context.Background(),l,parent,[]byte(`{"schema_version":1}`))
 if !errors.Is(err,cause)||len(db.calls)!=2{t.Fatal("transport failure masked or appended")}
}
func TestAuthoredBasePublicationRejectedCompleteNeverAppends(t *testing.T){
 p,l,parent,db,_:=authoredPublicationFixture();db.complete=false
 _,err:=p.Complete(context.Background(),l,parent,[]byte(`{"schema_version":1}`))
 if !errors.Is(err,artifactjson.ErrStale)||len(db.calls)!=3{t.Fatal("stale completion appended")}
}
func TestAuthoredBasePublicationCancelledBeforeStage(t *testing.T){
 p,l,parent,db,objects:=authoredPublicationFixture();ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err:=p.Complete(ctx,l,parent,[]byte(`{"schema_version":1}`))
 if !errors.Is(err,context.Canceled)||len(db.calls)!=0||objects.puts!=0{t.Fatal("cancelled publication performed IO")}
}
func TestAuthoredBasePublicationDedicatedPurposeRejectsMixedAuthorities(t *testing.T){
 s:=artifactjson.Scope{Tenant:uuid.New(),Run:uuid.New(),Owner:uuid.New(),Purpose:"v26_authored_base_receipt"}
 if _,err:=artifactjson.DocumentKey(s,uuid.New());err!=nil{t.Fatal(err)}
 for _,mutate:=range []func(*artifactjson.Scope){func(s *artifactjson.Scope){s.LocalPortrait=true},func(s *artifactjson.Scope){s.DonorPortrait=true},func(s *artifactjson.Scope){s.RestorationPortrait=true},func(s *artifactjson.Scope){s.AssemblyLease=uuid.New()}}{
  bad:=s;mutate(&bad);if _,err:=artifactjson.DocumentKey(bad,uuid.New());err==nil{t.Fatal("mixed publisher authority")}
 }
}
