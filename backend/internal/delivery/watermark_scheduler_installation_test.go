package delivery

import (
 "context"
 "errors"
 "testing"
 "github.com/jackc/pgx/v5"
)
type watermarkSchedulerInstallationDB struct{installed bool;err error;calls int;api,private []string}
func(d *watermarkSchedulerInstallationDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=watermarkSchedulerInstallationQuery||len(args)!=2{panic("installation query")};d.calls++;d.api=args[0].([]string);d.private=args[1].([]string)
 return metadataStorageRow{func(dest []any)error{if d.err!=nil{return d.err};*dest[0].(*bool)=d.installed;return nil}}
}
func TestWatermarkSchedulerInstallationRequiresClosedABIAndRoleIsolation(t *testing.T){
 db:=&watermarkSchedulerInstallationDB{installed:true}
 if err:=ValidateWatermarkSchedulerInstallation(context.Background(),db);err!=nil{t.Fatal(err)}
 if len(db.api)!=37||len(db.private)!=16{t.Fatal("incomplete function checks")}
 seen:=map[string]bool{}
 for _,signature:=range append(append([]string(nil),db.api...),db.private...){if seen[signature]{t.Fatal("duplicate or conflicting ABI",signature)};seen[signature]=true}
 for _,signature:=range []string{
 "photo.reserve_v30_watermark_initial_params(uuid,uuid,integer,text,text,uuid,uuid,uuid,text)",
 "photo.complete_v30_watermark_encode_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid)",
 "photo.begin_v30_watermark_sharpen_continuation(uuid,uuid,integer,text,jsonb,text,text)",
 "photo.read_v30_watermark_pin(uuid,uuid,uuid,uuid,uuid,text)",
 "photo.v30_watermark_stage_result(uuid,uuid,integer,text)",
 }{if !seen[signature]{t.Fatal("runtime dependency missing",signature)}}
 db.installed=false
 if err:=ValidateWatermarkSchedulerInstallation(context.Background(),db);err==nil{t.Fatal("missing or exposed SQL accepted")}
 failure:=errors.New("database unavailable");db.err=failure
 if err:=ValidateWatermarkSchedulerInstallation(context.Background(),db);!errors.Is(err,failure){t.Fatal("lost database error",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel();before:=db.calls
 if err:=ValidateWatermarkSchedulerInstallation(ctx,db);err!=context.Canceled||db.calls!=before{t.Fatal("cancelled installation query",err)}
}
