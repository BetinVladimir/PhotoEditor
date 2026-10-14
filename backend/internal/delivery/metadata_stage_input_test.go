package delivery

import (
 "context"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type metadataStageInputDB struct{raw,encoded string;id uuid.UUID;size int64;err error;calls int}
func(d *metadataStageInputDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if !strings.Contains(q,"photo.read_v30_metadata_stage_input(")||len(args)!=4{panic("wrong stage input authority")}
 d.calls++
 return metadataStorageRow{func(dest []any)error{
  if d.err!=nil{return d.err}
  *dest[0].(*string)=d.raw;*dest[1].(*uuid.UUID)=d.id;*dest[2].(*string)=d.encoded;*dest[3].(*int64)=d.size;return nil
 }}
}
func TestMetadataStageInputClosedConfigAndEvidence(t *testing.T){
 lease:=MetadataBarrierLease{Tenant:uuid.New(),ChildRun:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),ParentTask:uuid.New(),Until:time.Now().Add(time.Minute)}
 config:=MetadataChildConfig{QueueClass:"batch",ProjectID:uuid.New(),ExportID:uuid.New(),Request:FrozenExportRequest{ChildRunID:lease.ChildRun},
 Metadata:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},Approved:[]string{},FrozenSHA:strings.Repeat("b",64),PublicationSHA:strings.Repeat("c",64)}
 raw,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
 db:=&metadataStageInputDB{raw:string(raw),encoded:"{}",id:uuid.New(),size:100};store:=MetadataStageInputStore{db}
 input,err:=store.Load(context.Background(),lease)
 if err!=nil||input.Encoded.ArtifactID!=db.id||input.Encoded.SHA256!=digest([]byte(db.encoded))||input.EncodedBytes!=100{t.Fatal(err)}
 input.EncodedJCS[0]='!';if db.encoded!="{}"{t.Fatal("mutable evidence")}
 for _,mode:=range []string{"extra","child","null_approval","noncanonical_encoded","oversize","missing"}{
  db.raw=string(raw);db.encoded="{}";db.size=100;db.err=nil
  current:=config
  switch mode{
  case "extra":db.raw=strings.TrimSuffix(string(raw),"}")+`,"extra":true}`
  case "child":current.Request.ChildRunID=uuid.New();body,_:=canonical.Marshal(current);db.raw=string(body)
  case "null_approval":current.Approved=nil;body,_:=canonical.Marshal(current);db.raw=string(body)
  case "noncanonical_encoded":db.encoded="{ }"
  case "oversize":db.size=2*1024*1024*1024+1
  case "missing":db.err=pgx.ErrNoRows
  }
  if _,err:=store.Load(context.Background(),lease);err==nil{t.Fatal(mode)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel();calls:=db.calls
 if _,err=store.Load(ctx,lease);err!=context.Canceled||db.calls!=calls{t.Fatal("cancel",err)}
}
