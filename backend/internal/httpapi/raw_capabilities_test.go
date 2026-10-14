package httpapi

import (
 "context"
 "net/http/httptest"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/rawdevelopment"
 "photoeditor/backend/internal/upload"
)
type rawCapabilitiesStub struct{out upload.RAWCapabilities;cancel context.CancelFunc;calls int}
func(s *rawCapabilitiesStub)RAWCapabilities(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(upload.RAWCapabilities,error){s.calls++;if s.cancel!=nil{s.cancel()};return s.out,nil}
func TestRAWCapabilitiesHTTPEnvelopeAndCancellation(t *testing.T){
 for _,mode:=range []string{"valid","unavailable","foreign","missing view","hidden view","duplicate modes","decoder","bounds","cancel before","cancel after"}{t.Run(mode,func(t *testing.T){
  project:=uuid.New();view:=&rawdevelopment.CapabilityView{DecoderVersion:"rawpy-0.27.1/libraw-0.21.4",Demosaic:[]string{"ahd","dcb"},WBSource:[]string{"camera","daylight","auto"},HighlightPolicy:[]string{"clip","blend","reconstruct"},ExposureMin:-2,ExposureMax:3}
  stub:=&rawCapabilitiesStub{out:upload.RAWCapabilities{ProjectID:project,Available:true,Development:view}}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{case "unavailable":stub.out.Available=false;stub.out.Development=nil;case "foreign":stub.out.ProjectID=uuid.New();case "missing view":stub.out.Development=nil;case "hidden view":stub.out.Available=false;case "duplicate modes":view.Demosaic=[]string{"ahd","ahd"};case "decoder":view.DecoderVersion="latest";case "bounds":view.ExposureMax=4;case "cancel before":cancel();case "cancel after":stub.cancel=cancel}
  r:=httptest.NewRequest("GET","/",nil).WithContext(ctx);w:=httptest.NewRecorder()
  serveRAWCapabilities(w,r,auth.Identity{},uuid.New(),project,stub)
  if mode=="cancel before"||mode=="cancel after"{if w.Body.Len()!=0{t.Fatal("late response written")};if mode=="cancel before"&&stub.calls!=0{t.Fatal("cancelled request reached reader")};return}
  if mode=="valid"||mode=="unavailable"{if w.Code!=200||w.Header().Get("Cache-Control")!="private, no-store"{t.Fatal(w.Code,w.Body.String())}}else if w.Code!=503{t.Fatal("invalid provider envelope published",w.Code,w.Body.String())}
 })}
}
