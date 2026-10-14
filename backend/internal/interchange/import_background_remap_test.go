package interchange

import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type importedBackgroundPublisherStub struct{calls int;wrong bool;source ImportBackgroundSource;ids []string}
func(p *importedBackgroundPublisherStub)PublishImportedBackground(_ context.Context,source ImportBackgroundSource,_ v2input.SessionVersionBinding,id string)(string,error){
 p.calls++;p.source=source;p.ids=append(p.ids,id);if p.wrong{return "wrong",nil};return id,nil
}
func TestIndexedBackgroundRemapStableScopedAndReceiptBound(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 style:=ref();source:=ImportBackgroundSource{Original:ref(),Staged:ref()}
 source.Style=style;source.Portable="bg"
 index:=&ImportBackgroundIndex{sources:map[v2input.Ref]map[string]ImportBackgroundSource{style:{"bg":source}}}
 publisher:=&importedBackgroundPublisherStub{}
 remapper:=IndexedImportBackgroundRemapper{Workflow:uuid.New(),Index:index,Publisher:publisher}
 target:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:strings.Repeat("b",64)}
 first,err:=remapper.RemapImportedStyleBackground(context.Background(),style,"bg",target);if err!=nil{t.Fatal(err)}
 second,err:=remapper.RemapImportedStyleBackground(context.Background(),style,"bg",target)
 if err!=nil||first!=second||publisher.source!=source{t.Fatal("unstable or substituted source",err)}
 target.AssetVersionID=uuid.New()
 third,err:=remapper.RemapImportedStyleBackground(context.Background(),style,"bg",target);if err!=nil||third==first{t.Fatal("target scope collision",err)}
 publisher.wrong=true
 if _,err:=remapper.RemapImportedStyleBackground(context.Background(),style,"bg",target);err==nil{t.Fatal("accepted wrong publication receipt")}
 before:=publisher.calls
 if _,err:=remapper.RemapImportedStyleBackground(context.Background(),style,"missing",target);err==nil||publisher.calls!=before{t.Fatal("published missing mapping")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=remapper.RemapImportedStyleBackground(ctx,style,"bg",target);err==nil||publisher.calls!=before{t.Fatal("published after cancellation")}
}
