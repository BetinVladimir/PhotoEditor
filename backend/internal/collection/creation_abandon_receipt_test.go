package collection

import (
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

// Authored only. The accepted abandon outcome must prove the frozen policy,
// including expiry intent, rather than trusting a resource ID alone.
func TestValidateFrozenShareReceiptBindsOriginalPolicy(t *testing.T){
 collection:=uuid.New();request:=shareIntentRequest()
 intent,err:=freezeShareIntent(collection,request);if err!=nil{t.Fatal(err)}
 expires:=time.Date(2026,10,1,0,0,0,0,time.UTC)
 permissions,_:=canonical.Marshal(intent.Permissions);limits,_:=canonical.Marshal(intent.Limits);presets,_:=canonical.Marshal(intent.Presets)
 receipt:=ShareCreated{Share:Share{ID:uuid.New(),CollectionID:collection,Label:intent.Label,Permissions:permissions,PolicyVersion:"1",ExpiresAt:&expires,Limits:limits,Moderation:intent.Moderation,AllowedPresets:presets,CreatedAt:expires.Add(-time.Hour)}}
 if err=validateFrozenShareReceipt(receipt,collection,intent);err!=nil{t.Fatalf("historical accepted default must not be freshly time-validated: %v",err)}
 for name,change:=range map[string]func(*ShareCreated){
  "different parent":func(v *ShareCreated){v.CollectionID=uuid.New()},
  "different permissions":func(v *ShareCreated){v.Permissions=[]byte(`{"view":false,"download":true,"upload":false}`)},
  "different limits":func(v *ShareCreated){v.Limits=[]byte(`{"max_upload_files":2,"max_upload_bytes_total":1024,"max_file_bytes":1024,"allowed_formats":["jpg"]}`)},
  "different preset":func(v *ShareCreated){v.AllowedPresets=[]byte(`["`+uuid.NewString()+`"]`)},
  "lost effective default":func(v *ShareCreated){v.ExpiresAt=nil},
  "secret":func(v *ShareCreated){v.Secret="must-not-leak"},
  "updated policy":func(v *ShareCreated){v.PolicyVersion="2"},
 }{t.Run(name,func(t *testing.T){changed:=receipt;change(&changed);if validateFrozenShareReceipt(changed,collection,intent)==nil{t.Fatal("mismatched immutable receipt accepted")}})}
 nullRequest:=request;nullRequest.ExpiresAt=OptionalTime{Set:true};nullIntent,err:=freezeShareIntent(collection,nullRequest);if err!=nil{t.Fatal(err)}
 if validateFrozenShareReceipt(receipt,collection,nullIntent)==nil{t.Fatal("effective default mistaken for explicit null")}
 nullReceipt:=receipt;nullReceipt.ExpiresAt=nil;if err=validateFrozenShareReceipt(nullReceipt,collection,nullIntent);err!=nil{t.Fatal(err)}
 explicitRequest:=request;explicitRequest.ExpiresAt=OptionalTime{Set:true,Value:&expires};explicitIntent,err:=freezeShareIntent(collection,explicitRequest);if err!=nil{t.Fatal(err)}
 if err=validateFrozenShareReceipt(receipt,collection,explicitIntent);err!=nil{t.Fatal(err)}
 changed:=receipt;later:=expires.Add(time.Second);changed.ExpiresAt=&later;if validateFrozenShareReceipt(changed,collection,explicitIntent)==nil{t.Fatal("different explicit expiry accepted")}
}
