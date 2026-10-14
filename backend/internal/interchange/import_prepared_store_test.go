package interchange

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestPreparedReservationAuthorityAndFence(t *testing.T) {
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 token:=uuid.New();foreign:=uuid.New();zero:=uuid.Nil
 cases:=[]struct{name,state string;id,fence *uuid.UUID;ready bool;want error}{
  {"created","created",&ref.ArtifactID,&token,false,nil},
  {"resumed","resumed",&ref.ArtifactID,&token,false,nil},
  {"replayed","replayed",&ref.ArtifactID,&token,true,nil},
  {"quota","quota",nil,nil,false,artifactjson.ErrQuota},
  {"conflict","conflict",nil,nil,false,artifactjson.ErrConflict},
  {"forbidden","forbidden",nil,nil,false,artifactjson.ErrStale},
  {"unknown","future_state",nil,nil,false,artifactjson.ErrStale},
  {"foreign artifact","created",&foreign,&token,false,objectstore.ErrIntegrity},
  {"missing artifact","created",nil,&token,false,objectstore.ErrIntegrity},
  {"missing fence","created",&ref.ArtifactID,nil,false,objectstore.ErrIntegrity},
  {"zero fence","created",&ref.ArtifactID,&zero,false,objectstore.ErrIntegrity},
  {"false replay","replayed",&ref.ArtifactID,&token,false,objectstore.ErrIntegrity},
  {"premature ready","resumed",&ref.ArtifactID,&token,true,objectstore.ErrIntegrity},
 }
 for _,c:=range cases {t.Run(c.name,func(t *testing.T){
  row:=ledgerRow(func(d ...any)error{*d[0].(*string)=c.state;*d[1].(**uuid.UUID)=c.id;*d[2].(**uuid.UUID)=c.fence;*d[3].(*bool)=c.ready;return nil})
  got,err:=scanPreparedImportReservation(row,ref)
  if !errors.Is(err,c.want){t.Fatalf("error=%v want=%v",err,c.want)}
  if c.want!=nil {if got.ArtifactID!=uuid.Nil||got.Ready!=nil{t.Fatal("failure returned authority")};return}
  if got.ArtifactID!=ref.ArtifactID||got.Token!=token{t.Fatal("reservation changed identity/fence")}
  if c.ready {if got.Ready==nil||got.Ready.ID!=ref.ArtifactID||got.Ready.SHA256!=ref.SHA256{t.Fatal("replay lost exact content identity")}} else if got.Ready!=nil{t.Fatal("pending reservation became ready")}
 })}
}

func TestPreparedReservationDatabaseFailure(t *testing.T){
 failure:=errors.New("database read failed")
 _,err:=scanPreparedImportReservation(ledgerRow(func(...any)error{return failure}),v2input.Ref{})
 if !errors.Is(err,failure){t.Fatal("database error lost",err)}
}

func TestPreparedStoreCancellationBeforeDatabase(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 store:=SQLPreparedImportArtifactStore{}
 if _,err:=store.ReservePreparedImport(ctx,ImportPreparedScope{},v2input.Ref{},2,"application/json");!errors.Is(err,context.Canceled){t.Fatal(err)}
 if _,err:=store.CompletePreparedImport(ctx,ImportPreparedScope{},v2input.Ref{},artifactjson.Reservation{},objectstore.Object{});!errors.Is(err,context.Canceled){t.Fatal(err)}
 if _,err:=store.ReservePreparedImport(context.Background(),ImportPreparedScope{},v2input.Ref{},2,"application/json");!errors.Is(err,v2input.ErrInvalid){t.Fatal("missing installation accepted",err)}
}
