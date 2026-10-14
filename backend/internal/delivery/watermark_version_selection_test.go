package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
type watermarkSelectionDB struct{source WatermarkSource;err error;calls int}
func(d *watermarkSelectionDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=watermarkVersionSelectionQuery||len(args)!=9{panic("selection ABI")};d.calls++
 return metadataStorageRow{func(dest []any)error{if d.err!=nil{return d.err};*dest[0].(*uuid.UUID)=d.source.Asset;*dest[1].(*uuid.UUID)=d.source.Version;*dest[2].(*uuid.UUID)=d.source.Ref.ArtifactID;*dest[3].(*string)=d.source.Ref.SHA256;return nil}}
}
func TestWatermarkVersionSelectionBoundary(t *testing.T){
 intent,_,_,_:=createDocumentsFixture(t);scope,config,e:=watermarkPinFixture(t)
 scope.Tenant=intent.tenant;scope.Project=intent.request.ProjectID;scope.Actor=intent.actor;scope.Parent=intent.identity.Run;scope.Preset=intent.request.Policy.RequiredPresets[0]
 db:=&watermarkSelectionDB{source:e.Source};session:=uuid.New()
 selected,err:=intent.SelectWatermarkVersion(context.Background(),db,session,scope,config)
 if err!=nil||selected.Version!=e.Source.Version||selected.Ref!=e.Source.Ref{t.Fatal("selection",err)}
 db.err=pgx.ErrNoRows
 if _,err=intent.SelectWatermarkVersion(context.Background(),db,session,scope,config);err!=v2input.ErrExpired{t.Fatal("missing selection",err)}
 db.err=nil;db.source.Asset=uuid.New()
 if _,err=intent.SelectWatermarkVersion(context.Background(),db,session,scope,config);err==nil{t.Fatal("foreign asset")}
 foreign:=scope;foreign.Preset=uuid.New();before:=db.calls
 if _,err=intent.SelectWatermarkVersion(context.Background(),db,session,foreign,config);err==nil||db.calls!=before{t.Fatal("foreign preset queried")}
}
