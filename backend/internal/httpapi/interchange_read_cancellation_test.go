package httpapi

import (
 "context"
 "encoding/json"
 "net/http/httptest"
 "testing"

 "github.com/go-chi/chi/v5"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

type lateInterchangeReader struct { cancel context.CancelFunc }
func(s lateInterchangeReader)Get(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(json.RawMessage,error){s.cancel();return json.RawMessage(`{"late":"job"}`),nil}
func(s lateInterchangeReader)Inspect(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(json.RawMessage,error){s.cancel();return json.RawMessage(`{"late":"inspection"}`),nil}

func TestInterchangeReadDoesNotPublishNoncooperativeLateResult(t *testing.T){
 for _,inspection:=range []bool{false,true}{
  ctx,cancel:=context.WithCancel(context.Background())
  route:=chi.NewRouteContext();route.URLParams.Add("id",uuid.NewString())
  ctx=context.WithValue(ctx,chi.RouteCtxKey,route)
  request:=httptest.NewRequest("GET","/",nil).WithContext(ctx)
  response:=httptest.NewRecorder();reader:=lateInterchangeReader{cancel:cancel}
  if inspection { serveInterchangeInspection(response,request,auth.Identity{},uuid.New(),reader) } else { serveInterchangeJob(response,request,auth.Identity{},uuid.New(),reader) }
  if response.Body.Len()!=0 { t.Fatalf("cancelled read published late DTO: %s",response.Body.String()) }
  cancel()
 }
}
