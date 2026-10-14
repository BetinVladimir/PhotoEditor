package delivery

import (
 "context"
 "errors"
 "testing"
 "github.com/jackc/pgx/v5"
)
type watermarkInstallationDB struct{installed bool;err error;calls int;api,private []string}
func(d *watermarkInstallationDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=watermarkAPIInstallationQuery||len(args)!=2{panic("installation query")};d.calls++;d.api=args[0].([]string);d.private=args[1].([]string)
 return metadataStorageRow{func(dest []any)error{if d.err!=nil{return d.err};*dest[0].(*bool)=d.installed;return nil}}
}
func TestWatermarkInstallationRequiresClosedABIAndRoleIsolation(t *testing.T){
 db:=&watermarkInstallationDB{installed:true}
 if err:=ValidateWatermarkAPIInstallation(context.Background(),db);err!=nil{t.Fatal(err)}
 if len(db.api)!=4||len(db.private)!=4{t.Fatal("incomplete function checks")}
 db.installed=false
 if err:=ValidateWatermarkAPIInstallation(context.Background(),db);err==nil{t.Fatal("missing or exposed SQL accepted")}
 failure:=errors.New("database unavailable");db.err=failure
 if err:=ValidateWatermarkAPIInstallation(context.Background(),db);!errors.Is(err,failure){t.Fatal("lost database error",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel();before:=db.calls
 if err:=ValidateWatermarkAPIInstallation(ctx,db);err!=context.Canceled||db.calls!=before{t.Fatal("cancelled installation query",err)}
}
