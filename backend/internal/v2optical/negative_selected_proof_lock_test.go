package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type negativeSelectedProofTx struct{negativeSourceScopeTx;packetCalls int;refused bool}
func(t *negativeSelectedProofTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row {
 if q!="SELECT photo.lock_v29_negative_selected_proof($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb)" {return t.negativeSourceScopeTx.QueryRow(ctx,q,args...)}
 t.packetCalls++
 return negativeLockRow(func(dest ...any)error{if len(args)!=11||args[0]!=t.tenant||args[8]==""||args[9]==""||args[10]=="" {return errors.New("missing physical proof identity")};*dest[0].(*bool)=!t.refused;return nil})
}
func TestNegativeSelectedProofLockBindsIdentityAndPinsBeforeSQL(t *testing.T){
 _,source,_,_:=negativeDraftFixture(t);spec,object,_:=perspectiveArrayFixture(t,3,[]float32{-0.5,1.5,0.25})
 image,err:=review.ParseWorkingImage(source);if err!=nil {t.Fatal(err)}
 image.Canvas.Width=1;image.Canvas.Height=1;image.Pixels.ID=spec.Ref.ArtifactID;image.Pixels.SHA256=spec.Ref.SHA256;image.Alpha=nil
 source,err=canonical.Marshal(image);if err!=nil {t.Fatal(err)};sum:=sha256.Sum256(source)
 tenant,project,reviewID:=uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};hash:="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 selection:=VerifiedNegativeSelection{source:negativeSelectedSource{SchemaVersion:1,Tenant:tenant,Project:project,Review:reviewID,Task:uuid.New(),Run:uuid.New(),Hash:hash,Route:"base",Source:&negativeSourceLocator{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:]),Bytes:int64(len(source)),Bucket:"photos",Key:"source",Version:"original",Media:"application/json"}},raw:string(source)}
 proof:=VerifiedNegativeSelectedArrays{selection:selection,specs:[]NegativeSourceArraySpec{spec},objects:[]objectstore.Object{object},who:who}
 for _,refused:=range []bool{false,true} {
  tx:=&negativeSelectedProofTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},refused:refused}
  err:=LockNegativeSelectedArrays(context.Background(),tx,"photos",who,tenant,project,reviewID,hash,proof)
  if (err==nil)==refused||tx.packetCalls!=1 {t.Fatal("lock outcome",err)}
 }
 tx:=&negativeSelectedProofTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}}}
 other:=who;other.SessionID=uuid.New()
 if err:=LockNegativeSelectedArrays(context.Background(),tx,"photos",other,tenant,project,reviewID,hash,proof);err!=v2input.ErrInvalid||tx.packetCalls!=0||len(tx.calls)!=0 {t.Fatal("foreign proof reached SQL",err)}
 proof.objects[0].VersionID="null"
 if _,_,err:=selectedNegativeProofPacket("photos",who,tenant,project,reviewID,hash,proof);err==nil {t.Fatal("invalid pin accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=LockNegativeSelectedArrays(ctx,nil,"photos",who,tenant,project,reviewID,hash,proof);err!=context.Canceled {t.Fatal("cancel ignored",err)}
}
