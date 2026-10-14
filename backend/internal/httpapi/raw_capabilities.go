package httpapi

import (
 "context"
 "net/http"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/rawdevelopment"
 "photoeditor/backend/internal/upload"
)
type rawCapabilitiesReader interface{RAWCapabilities(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(upload.RAWCapabilities,error)}
func validRAWModes(values,expected []string)bool{
 if len(values)!=len(expected){return false};seen:=map[string]bool{}
 for _,v:=range values{if seen[v]{return false};seen[v]=true}
 for _,v:=range expected{if !seen[v]{return false}};return true
}
func validRAWCapabilities(out upload.RAWCapabilities,project uuid.UUID)bool{
 if project==uuid.Nil||out.ProjectID!=project{return false}
 if !out.Available{return out.Development==nil}
 d:=out.Development;if d==nil{return false}
 return rawdevelopment.Valid(&rawdevelopment.Policy{SchemaVersion:1,Demosaic:"ahd",WBSource:"camera",ExposureEV:0,HighlightPolicy:"clip",DecoderVersion:d.DecoderVersion})&&
 d.ExposureMin== -2&&d.ExposureMax==3&&validRAWModes(d.Demosaic,[]string{"ahd","dcb"})&&validRAWModes(d.WBSource,[]string{"camera","daylight","auto"})&&validRAWModes(d.HighlightPolicy,[]string{"clip","blend","reconstruct"})
}
func serveRAWCapabilities(w http.ResponseWriter,r *http.Request,who auth.Identity,tenant,project uuid.UUID,reader rawCapabilitiesReader){
 if r.Context().Err()!=nil{return}
 if reader==nil{writeError(w,r,domain.NewError("DEPENDENCY_UNAVAILABLE",503));return}
 out,err:=reader.RAWCapabilities(r.Context(),who,tenant,project)
 if r.Context().Err()!=nil{return}
 if err!=nil{writeError(w,r,err);return}
 if !validRAWCapabilities(out,project){writeError(w,r,domain.NewError("QUALIFICATION_UNAVAILABLE",503));return}
 w.Header().Set("Cache-Control","private, no-store")
 writeJSON(w,http.StatusOK,out)
}
