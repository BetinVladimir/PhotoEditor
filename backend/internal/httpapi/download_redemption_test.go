package httpapi
import (
 "context"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "photoeditor/backend/internal/portal"
)
type redemptionFunc func(context.Context,portal.DownloadRedeemRequest)(portal.DownloadLink,error)
func(f redemptionFunc)RedeemDownloadIntent(ctx context.Context,r portal.DownloadRedeemRequest)(portal.DownloadLink,error){return f(ctx,r)}
func TestDownloadRedemptionNeverWritesLateBearerResponse(t *testing.T){
 for _,mode:=range []string{"before","after"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  if mode=="before"{cancel()}
  request:=httptest.NewRequest(http.MethodPost,"/api/v1/downloads/redeem",strings.NewReader(`{"token":"opaque"}`)).WithContext(ctx)
  request.Header.Set("Content-Type","application/json")
  response:=httptest.NewRecorder();calls:=0
  service:=redemptionFunc(func(context.Context,portal.DownloadRedeemRequest)(portal.DownloadLink,error){calls++;cancel();return portal.DownloadLink{URL:"https://storage.test/private?versionId=v1"},nil})
  serveDownloadRedemption(response,request,service)
  if response.Body.Len()!=0{t.Fatal("cancelled request exposed URL")}
  if mode=="before"&&calls!=0||mode=="after"&&calls!=1{t.Fatal("unexpected service calls",calls)}
  if response.Header().Get("Cache-Control")!="private, no-store"||response.Header().Get("Pragma")!="no-cache"{t.Fatal("bearer response cacheable")}
 })}
}
func TestDownloadRedemptionRejectsMalformedBodyBeforeService(t *testing.T){
 response:=httptest.NewRecorder();request:=httptest.NewRequest(http.MethodPost,"/api/v1/downloads/redeem",strings.NewReader(`{"token":"x","extra":true}`));request.Header.Set("Content-Type","application/json")
 calls:=0;service:=redemptionFunc(func(context.Context,portal.DownloadRedeemRequest)(portal.DownloadLink,error){calls++;return portal.DownloadLink{},nil})
 serveDownloadRedemption(response,request,service)
 if calls!=0||response.Code<400{t.Fatal("malformed redemption reached service")}
}
