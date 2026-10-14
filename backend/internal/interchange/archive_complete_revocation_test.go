package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/objectstore"
)
type archiveRevokeDB struct{tid,upload,cleanup,writer uuid.UUID;bucket,key,multipart string;calls int;accepted bool;args []any}
func(d *archiveRevokeDB)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{
 d.calls++;d.args=append([]any(nil),args...)
 return archiveCleanupRow{scan:func(dest ...any)error{
  if len(dest)==1{*dest[0].(*bool)=d.accepted;return nil}
  *dest[0].(*uuid.UUID)=d.tid;*dest[1].(*uuid.UUID)=d.upload;*dest[2].(*uuid.UUID)=d.cleanup;*dest[3].(*uuid.UUID)=d.writer;*dest[4].(*string)=d.bucket;*dest[5].(*string)=d.key;*dest[6].(*string)=d.multipart;return nil
 }}
}
func TestArchiveCompleteRevocationRequiresPhysicalProofAndExactCommit(t *testing.T){
 for _,mode:=range []string{"ok","storage-error","refused","foreign-key","foreign-bucket","cancelled"}{t.Run(mode,func(t *testing.T){
  db:=&archiveRevokeDB{tid:uuid.New(),upload:uuid.New(),cleanup:uuid.New(),writer:uuid.New(),bucket:"photos",multipart:"multipart",accepted:mode!="refused"};db.key,_=objectstore.InterchangeUploadKey(db.tid,db.upload);objects:=&archiveDrainObjects{}
  if mode=="foreign-key"{db.key="foreign"};if mode=="foreign-bucket"{db.bucket="foreign"};if mode=="storage-error"{objects.err=objectstore.ErrUnavailable}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  ok,err:=(ArchiveCompleteRevocation{DB:db,Objects:objects,Bucket:"photos"}).ProcessOne(ctx)
  if mode=="ok"{if err!=nil||!ok||db.calls!=2||objects.calls!=1||len(db.args)!=7||db.args[3]!=db.writer||db.args[6]!=db.multipart{t.Fatal("revocation not exactly committed",err)}}else if err==nil||ok{t.Fatal("unproven revocation accepted",mode)}
  if mode=="storage-error"&&db.calls!=1{t.Fatal("storage failure reached commit")}
  if (mode=="foreign-key"||mode=="foreign-bucket"||mode=="cancelled")&&objects.calls!=0{t.Fatal("invalid authority reached storage")}
 })}
}
