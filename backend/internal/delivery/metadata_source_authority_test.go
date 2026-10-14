package delivery

import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type metadataSourceAuthorityDB struct {calls int;allowed bool;cancel context.CancelFunc}
func(d *metadataSourceAuthorityDB)QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 if !strings.Contains(query,"photo.v30_metadata_source_authorized(")||len(args)!=11{panic("wrong metadata source authority")}
 d.calls++;return metadataSourceAuthorityRow{d}
}
type metadataSourceAuthorityRow struct{db *metadataSourceAuthorityDB}
func(r metadataSourceAuthorityRow)Scan(dest ...any)error{
 *dest[0].(*bool)=r.db.allowed
 if r.db.cancel!=nil{r.db.cancel()}
 return nil
}
func TestMetadataSourceAuthorityScopeAndCancellation(t *testing.T){
 raw,err:=canonical.Marshal(MetadataDocument{});if err!=nil{t.Fatal(err)}
 scope:=MetadataSourcePublicationScope{Tenant:uuid.New(),Project:uuid.New(),Actor:uuid.New(),Session:uuid.New(),
 Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)},CanvasHash:strings.Repeat("c",64)},
 Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("d",64)},OriginalBytes:100,OriginalMIME:"image/jpeg"}
 plan,err:=CompileMetadataSourcePublication(scope,v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)},raw,"copyright",nil)
 if err!=nil{t.Fatal(err)}
 db:=&metadataSourceAuthorityDB{allowed:true}
 if err=AuthorizeMetadataSourcePublication(context.Background(),db,scope,plan,"copyright",nil);err!=nil{t.Fatal(err)}
 foreign:=scope;foreign.Actor=uuid.New()
 if err=AuthorizeMetadataSourcePublication(context.Background(),db,foreign,plan,"copyright",nil);err==nil||db.calls!=1{t.Fatal("foreign principal reached SQL")}
 db.allowed=false
 if err=AuthorizeMetadataSourcePublication(context.Background(),db,scope,plan,"copyright",nil);err!=v2input.ErrExpired{t.Fatal("revocation",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 calls:=db.calls
 if err=AuthorizeMetadataSourcePublication(ctx,db,scope,plan,"copyright",nil);err!=context.Canceled||db.calls!=calls{t.Fatal("cancelled authority",err)}
 ctx,cancel=context.WithCancel(context.Background());defer cancel();db.allowed=true;db.cancel=cancel
 if err=AuthorizeMetadataSourcePublication(ctx,db,scope,plan,"copyright",nil);err!=context.Canceled{t.Fatal("post SQL cancellation",err)}
}
