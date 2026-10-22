package httpapi
import (
 "context"
 "net/http"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/portal"
)
type downloadIntentRedeemer interface{RedeemDownloadIntent(context.Context,portal.DownloadRedeemRequest)(portal.DownloadLink,error)}
func serveDownloadRedemption(w http.ResponseWriter,r *http.Request,service downloadIntentRedeemer){
 // A one-use bearer and its signed URL must not be retained by caches.
 w.Header().Set("Cache-Control","private, no-store")
 w.Header().Set("Pragma","no-cache")
 if r.Context().Err()!=nil{return}
 var request portal.DownloadRedeemRequest
 if err:=decodeJSON(r,&request);err!=nil{writeError(w,r,err);return}
 if service==nil{writeError(w,r,domain.NewError("DOWNLOAD_UNAVAILABLE",503));return}
 out,err:=service.RedeemDownloadIntent(r.Context(),request)
 if r.Context().Err()!=nil{return}
 if err!=nil{writeError(w,r,err);return}
 writeJSON(w,http.StatusOK,out)
}
