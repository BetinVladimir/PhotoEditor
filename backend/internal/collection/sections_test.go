package collection

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

// Invalid input must fail before a database is needed. Integration scenarios
// exercise the existing project/tenant authority below the shared authorize.
func TestListSectionsRejectsUnboundedPageBeforeDatabase(t *testing.T){
 for _,n:=range []int{-1,0,101}{if _,err:=(Service{}).ListSections(context.Background(),auth.Identity{},uuid.New(),uuid.New(),n,"");err==nil{t.Fatalf("limit %d admitted",n)}}
}

func TestWritableCollectionStates(t *testing.T){for _,s:=range []string{"draft","published"}{if !writableCollectionState(s){t.Fatal(s)}};for _,s:=range []string{"archived","deleting","","unknown"}{if writableCollectionState(s){t.Fatal(s)}}}
