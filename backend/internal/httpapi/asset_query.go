package httpapi

import (
 "net/url"
 "photoeditor/backend/internal/domain"
)

// upload_state is the documented filter. Keep the previously shipped status
// spelling as an alias, but never choose silently between conflicting values.
func assetUploadStateQuery(query url.Values)(string,error){
 for _,key:=range []string{"upload_state","status"}{if len(query[key])>1{return "",domain.NewError("VALIDATION_FAILED",422)}}
 state,legacy:=query.Get("upload_state"),query.Get("status")
 if state!=""&&legacy!=""&&state!=legacy{return "",domain.NewError("VALIDATION_FAILED",422)}
 if state==""{state=legacy};return state,nil
}
