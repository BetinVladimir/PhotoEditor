package v2render
import (
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)
func TestMultiReplacementReviewPageUnavailableAndOrdering(t *testing.T){
 tenant,review,project:=uuid.New(),uuid.New(),uuid.New();hash:=strings.Repeat("a",64)
 first:=uuid.MustParse("00000000-0000-4000-8000-000000000010");second:=uuid.MustParse("00000000-0000-4000-8000-000000000011")
 page:=MultiReplacementReviewPage{Review:review,DecisionHash:hash,Items:[]MultiReplacementReviewItem{{OriginalTask:first,Project:project,Operation:"optical.panorama"},{OriginalTask:second,Project:project,Operation:"optical.focus_stack"}}}
 raw,_:=canonical.Marshal(page)
 if got,err:=decodeMultiReplacementReviewPage(raw,tenant,review,hash,uuid.Nil);err!=nil||len(got.Items)!=2{t.Fatal(got,err)}
 for _,alter:=range []func(*MultiReplacementReviewPage){
 func(p *MultiReplacementReviewPage){p.Items[1].OriginalTask=first},
 func(p *MultiReplacementReviewPage){p.Items[0].Operation="optical.perspective"},
 func(p *MultiReplacementReviewPage){p.Next=&second},
 func(p *MultiReplacementReviewPage){p.DecisionHash=strings.Repeat("b",64)},
 }{bad:=page;bad.Items=append([]MultiReplacementReviewItem(nil),page.Items...);alter(&bad);body,_:=canonical.Marshal(bad);if _,err:=decodeMultiReplacementReviewPage(body,tenant,review,hash,uuid.Nil);err==nil{t.Fatal("invalid page accepted")}}
}

func TestMultiReplacementReviewPageRejectsEmptyForeignRequestScope(t *testing.T){
 review:=uuid.New();hash:=strings.Repeat("a",64)
 raw,_:=canonical.Marshal(MultiReplacementReviewPage{Review:review,DecisionHash:hash,Items:[]MultiReplacementReviewItem{}})
 if _,err:=decodeMultiReplacementReviewPage(raw,uuid.Nil,review,hash,uuid.Nil);err==nil{t.Fatal("zero tenant accepted")}
 if _,err:=decodeMultiReplacementReviewPage(raw,uuid.New(),review,"bad",uuid.Nil);err==nil{t.Fatal("invalid decision accepted")}
}
