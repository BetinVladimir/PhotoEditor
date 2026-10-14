package v2render
import (
 "testing"
 "strings"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)
func TestReplacementReviewPageBoundsAndCursor(t *testing.T) {
 tenant,review,project:=uuid.New(),uuid.New(),uuid.New();hash:=strings.Repeat("d",64)
 a:=uuid.MustParse("00000000-0000-4000-8000-000000000001")
 value:=PerspectiveReplacementReviewPage{Review:review,DecisionHash:hash,Items:[]PerspectiveReplacementReviewItem{{OriginalTask:a,Project:project}}}
 raw,_:=canonical.Marshal(value)
 got,err:=decodePerspectiveReplacementReviewPage(raw,tenant,review,hash,uuid.Nil);if err!=nil||len(got.Items)!=1||got.Items[0].Scope!=nil { t.Fatal(got,err) }
 if _,err=decodePerspectiveReplacementReviewPage(raw,tenant,review,hash,a);err==nil { t.Fatal("cursor replay accepted") }
 value.Next=&a;raw,_=canonical.Marshal(value);if _,err=decodePerspectiveReplacementReviewPage(raw,tenant,review,hash,uuid.Nil);err==nil { t.Fatal("short continuation accepted") }
 value.Next=nil;value.Items=append(value.Items,value.Items[0]);raw,_=canonical.Marshal(value);if _,err=decodePerspectiveReplacementReviewPage(raw,tenant,review,hash,uuid.Nil);err==nil { t.Fatal("duplicate accepted") }
}
