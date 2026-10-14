package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/objectstore"
)
type archiveDrainDB struct{values []*string;calls int}
func(d *archiveDrainDB)QueryRow(context.Context,string,...any)pgx.Row{return archiveCleanupRow{scan:func(dest ...any)error{*dest[0].(**string)=d.values[d.calls];d.calls++;return nil}}}
type archiveDrainObjects struct{calls,genCalls int;generation uuid.UUID;multipart string;err error}
func(s *archiveDrainObjects)DrainInterchangeMultipart(_ context.Context,_,_ uuid.UUID,multipart string)error{s.calls++;s.multipart=multipart;return s.err}
func(s *archiveDrainObjects)DrainInterchangeGenerationMultipart(_ context.Context,_,_,generation uuid.UUID,multipart string)error{s.genCalls++;s.generation=generation;s.multipart=multipart;return s.err}
func TestArchiveStorageDrainBindsScopeAndRechecksAuthority(t *testing.T){
 for _,mode:=range []string{"ok","unbound","denied","changed","revoked","storage-error"}{t.Run(mode,func(t *testing.T){
  tid,upload:=uuid.New(),uuid.New();key,_:=objectstore.InterchangeUploadKey(tid,upload);value:="multipart";if mode=="unbound"{value=""};changed:="changed"
  db:=&archiveDrainDB{values:[]*string{&value,&value}};objects:=&archiveDrainObjects{}
  switch mode{case "denied":db.values[0]=nil;case "changed":db.values[1]=&changed;case "revoked":db.values[1]=nil;case "storage-error":objects.err=objectstore.ErrUnavailable}
  s:=SQLArchiveStorageDrain{DB:db,Objects:objects,Bucket:"photos"};err:=s.AssertArchiveDrained(context.Background(),tid,upload,"photos",key)
  if (err==nil)!=(mode=="ok"||mode=="unbound"){t.Fatal(mode,err)}
  if mode=="denied"&&objects.calls!=0{t.Fatal("denied drain reached storage")}
  if mode=="ok"||mode=="unbound"{if db.calls!=2||objects.calls!=1||objects.multipart!=value{t.Fatal("missing exact drain/recheck")}}
  before:=db.calls;if err:=s.AssertArchiveDrained(context.Background(),tid,upload,"foreign",key);err==nil||db.calls!=before{t.Fatal("foreign bucket reached DB")}
 })}
}
func TestArchiveStorageDrainRoutesExactGeneration(t *testing.T){
 tid,upload,generation:=uuid.New(),uuid.New(),uuid.New();key,_:=objectstore.InterchangeGenerationKey(tid,upload,generation);multipart:="multipart"
 db:=&archiveDrainDB{values:[]*string{&multipart,&multipart}};objects:=&archiveDrainObjects{}
 err:=(SQLArchiveStorageDrain{DB:db,Objects:objects,Bucket:"photos"}).AssertArchiveDrained(context.Background(),tid,upload,"photos",key)
 if err!=nil||db.calls!=2||objects.calls!=0||objects.genCalls!=1||objects.generation!=generation{t.Fatal("generation drain crossed key",err,db,objects)}
}
