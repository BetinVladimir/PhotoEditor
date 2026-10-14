package delivery

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type inventoryEntryReadDB struct{entry,body,pin,snapshot string;err error;calls int}
func(d *inventoryEntryReadDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=metadataInventoryEntryQuery||len(args)!=8{panic("wrong inventory read boundary")};d.calls++
 return metadataStorageRow{func(dest []any)error{if d.err!=nil{return d.err};*dest[0].(*string)=d.entry;*dest[1].(*string)=d.body;*dest[2].(*string)=d.pin;*dest[3].(*string)=d.snapshot;return nil}}
}
func TestMetadataInventoryEntryReadAndChildConfig(t *testing.T){
 plan,lease,_:=expansionPlanFixture(t);page,err:=plan.ExportChildren(lease.Run,0,1);if err!=nil{t.Fatal(err)}
 request,_,err:=plan.ExportRequest(lease.Run,page.Items[0]);if err!=nil{t.Fatal(err)}
 var config map[string]any;if json.Unmarshal([]byte(request.ConfigJCS),&config)!=nil{t.Fatal("config")};config["metadata_policy"]="copyright"
 raw,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)};request.ConfigJCS=string(raw);request.Preset.PresetSHA=digest(raw);request.Preset.Config.SHA256=digest(raw)
 repository,_:=sourceStorageFixture(t);scope:=repository.scope;scope.Binding=request.Binding
 sourceID,err:=MetadataSourceIdentity(request.ParentRunID,request.Binding,"copyright",nil);if err!=nil{t.Fatal(err)}
 sourceRef:=repository.plan.Ref();sourceRef.ArtifactID=sourceID
 publication,err:=CompileMetadataSourcePublication(scope,sourceRef,repository.plan.SourceJCS(),"copyright",nil);if err!=nil{t.Fatal(err)}
 entry,err:=canonical.Marshal(MetadataInventoryEntry{Ordinal:request.Ordinal,Binding:request.Binding,Metadata:publication.Ref(),PublicationSHA256:publication.SHA256(),FrozenSHA256:digest(publication.FrozenJCS())});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"exact","snapshot","pin","entry","body","missing","foreign_source","unknown_entry_field"}{
  db:=&inventoryEntryReadDB{entry:string(entry),body:string(publication.JCS()),pin:publication.SHA256(),snapshot:request.SnapshotDigest}
  switch mode{case "snapshot":db.snapshot="changed";case "pin":db.pin="changed";case "entry":db.entry="{}";case "body":db.body="{}";case "missing":db.err=pgx.ErrNoRows
  case "unknown_entry_field":
   var value map[string]any;json.Unmarshal(entry,&value);value["untrusted"]=true;changed,_:=canonical.Marshal(value);db.entry=string(changed)
  case "foreign_source":
   foreignRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sourceRef.SHA256}
   foreign,compileErr:=CompileMetadataSourcePublication(scope,foreignRef,repository.plan.SourceJCS(),"copyright",nil);if compileErr!=nil{t.Fatal(compileErr)}
   changed,_:=canonical.Marshal(MetadataInventoryEntry{Ordinal:request.Ordinal,Binding:request.Binding,Metadata:foreign.Ref(),PublicationSHA256:foreign.SHA256(),FrozenSHA256:digest(foreign.FrozenJCS())})
   db.entry=string(changed);db.body=string(foreign.JCS());db.pin=foreign.SHA256()
  }
  body,pin,err:=(MetadataInventoryEntryStore{db}).CompileChild(context.Background(),scope.Tenant,scope.Project,uuid.New(),uuid.New(),uuid.New(),request,"copyright",nil)
  if mode=="exact"{if err!=nil||digest(body)!=pin{t.Fatal(err)}}else if err==nil{t.Fatal(mode)}
 }
}
