package delivery

import (
 "sort"
 "photoeditor/backend/internal/v2input"
)
// normalizeMetadataApproval rejects implicit consent and owns the sorted list.
// An empty approved_exif list permits no EXIF fields, including GPS.
func normalizeMetadataApproval(policy string,fields []string)([]string,error){
 switch policy{case "strip","copyright":if len(fields)!=0{return nil,v2input.ErrInvalid};return nil,nil
 case "approved_exif":
 default:return nil,v2input.ErrInvalid}
 if _,err:=ProjectDeliveryMetadata(MetadataDocument{},policy,fields);err!=nil{return nil,err}
 if len(fields)==0{return nil,nil}
 owned:=append([]string(nil),fields...);sort.Strings(owned);return owned,nil
}
