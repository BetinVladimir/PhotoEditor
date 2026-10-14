package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
type metadataRetryDB struct{outcome string;raw,pin *string;err error}
func(d metadataRetryDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=metadataSourceRetryQuery||len(args)!=9{panic("retry query boundary")}
 return metadataStorageRow{func(dest []any)error{if d.err!=nil{return d.err};*dest[0].(*string)=d.outcome;*dest[1].(**string)=d.raw;*dest[2].(**string)=d.pin;return nil}}
}
func TestMetadataRetryRestoresOriginSessionUnderCurrentScope(t *testing.T){
 repository,_:=sourceStorageFixture(t);current:=repository.scope;current.Session=uuid.New()
 raw,pin:=string(repository.plan.JCS()),repository.plan.SHA256()
 db:=metadataRetryDB{outcome:"saved",raw:&raw,pin:&pin}
 restored,found,err:=ReadMetadataSourceRetry(context.Background(),db,repository.run,current,"copyright",nil)
 if err!=nil||!found||restored.SHA256()!=repository.plan.SHA256(){t.Fatal(found,err)}
 changed:=current;changed.OriginalBytes++
 if _,_,err=ReadMetadataSourceRetry(context.Background(),db,repository.run,changed,"copyright",nil);err==nil{t.Fatal("changed original accepted")}
 if _,_,err=ReadMetadataSourceRetry(context.Background(),db,uuid.New(),current,"copyright",nil);err==nil{t.Fatal("foreign parent accepted")}
 _,found,err=ReadMetadataSourceRetry(context.Background(),metadataRetryDB{outcome:"absent"},repository.run,current,"copyright",nil)
 if err!=nil||found{t.Fatal("absent",err)}
 if _,_,err=ReadMetadataSourceRetry(context.Background(),metadataRetryDB{err:pgx.ErrNoRows},repository.run,current,"copyright",nil);err!=v2input.ErrExpired{t.Fatal("stale authority treated as absence",err)}
}
