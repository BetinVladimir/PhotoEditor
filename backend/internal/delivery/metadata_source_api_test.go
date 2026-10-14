package delivery

import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestMetadataSourceIdentityPinsVersionAndExplicitApproval(t *testing.T){
 run:=uuid.New();binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},CanvasHash:strings.Repeat("b",64)}
 first,err:=MetadataSourceIdentity(run,binding,"approved_exif",[]string{"author","gps"});if err!=nil{t.Fatal(err)}
 replay,err:=MetadataSourceIdentity(run,binding,"approved_exif",[]string{"gps","author"});if err!=nil||first!=replay{t.Fatal("approval order changes identity",err)}
 changed,err:=MetadataSourceIdentity(run,binding,"approved_exif",[]string{"author"});if err!=nil||changed==first{t.Fatal("approval not pinned")}
 foreign:=binding;foreign.CanvasHash=strings.Repeat("c",64)
 changed,err=MetadataSourceIdentity(run,foreign,"approved_exif",[]string{"author","gps"});if err!=nil||changed==first{t.Fatal("canvas not pinned")}
 if _,err=MetadataSourceIdentity(run,binding,"copyright",[]string{"gps"});err==nil{t.Fatal("implicit GPS")}
 if _,err=MetadataSourceIdentity(run,binding,"approved_exif",[]string{"gps","gps"});err==nil{t.Fatal("duplicate approval")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=(Service{}).PrepareAndPublishMetadataSource(ctx,auth.Identity{},uuid.Nil,uuid.Nil,uuid.Nil,binding,objectstore.Object{},"copyright",nil,MetadataSourceAPIDependencies{});err!=context.Canceled{t.Fatal(err)}
}
