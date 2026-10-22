package v2draft

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

// Shape rejection must happen before reservation/object IO.
func TestEditorExecutedStyleRejectsPartialBeforePublication(t *testing.T){
 for _,raw:=range []string{`null`,`[]`,`{"strength":1}`,`{"strength":-1}`} {
  _,err:=(Publisher{}).PublishEditorExecutedStyle(context.Background(),auth.Identity{},uuid.New(),uuid.New(),uuid.New(),[]byte(raw))
  if err==nil{t.Fatal("partial executable style accepted",raw)}
 }
}
