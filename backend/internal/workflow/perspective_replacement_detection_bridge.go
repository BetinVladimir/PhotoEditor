package workflow

import (
 "bytes"
 "context"
 "crypto/rand"
 "crypto/subtle"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net"
 "net/http"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// A bridge is bound to one operator-owned replacement plan. It is served only
// on a caller-owned Unix listener, never mounted in the public HTTP API.
type ReplacementDetectionAdoptionBridge struct{source ReplacementDetectionDocuments;token string}
func NewReplacementDetectionAdoptionBridge(ctx context.Context,source ReplacementDetectionDocuments)(*ReplacementDetectionAdoptionBridge,string,error){
 scope,body,task,_,err:=source.Plan.PreparePublication(ctx);if err!=nil{return nil,"",err}
 if _,err=source.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return nil,"",err}
 var secret [32]byte;if _,err=rand.Read(secret[:]);err!=nil{return nil,"",err}
 token:=hex.EncodeToString(secret[:]);return &ReplacementDetectionAdoptionBridge{source:source,token:token},token,nil
}
func(b *ReplacementDetectionAdoptionBridge) Serve(ctx context.Context,listener *net.UnixListener)error{
 if b==nil||listener==nil||len(b.token)!=64{return v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return err}
 server:=&http.Server{BaseContext:func(net.Listener)context.Context{return ctx},Handler:http.HandlerFunc(b.serveHTTP),ReadHeaderTimeout:5*time.Second,ReadTimeout:20*time.Second,WriteTimeout:25*time.Second,IdleTimeout:10*time.Second,MaxHeaderBytes:4096}
 done:=make(chan struct{});defer close(done)
 go func(){select{case <-ctx.Done():_ = server.Close();case <-done:}}()
 err:=server.Serve(listener);if ctx.Err()!=nil{return ctx.Err()};if errors.Is(err,http.ErrServerClosed){return nil};return err
}

type replacementDetectionBridgeRequest struct{
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`;Run uuid.UUID `json:"run_id"`;Task uuid.UUID `json:"task_id"`
 Attempt int `json:"attempt"`;Fence int64 `json:"fence"`;Result uuid.UUID `json:"result_id"`
 Inventory json.RawMessage `json:"inventory,omitempty"`;Digest string `json:"digest,omitempty"`
 Evidence json.RawMessage `json:"evidence,omitempty"`;Root json.RawMessage `json:"root,omitempty"`
}
func(b *ReplacementDetectionAdoptionBridge) serveHTTP(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if b==nil||len(b.token)!=64||subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")),[]byte("Bearer "+b.token))!=1{http.Error(w,"unauthorized",http.StatusUnauthorized);return}
 if r.Method!=http.MethodPost||r.URL.RawQuery!=""||(r.URL.Path!="/evidence"&&r.URL.Path!="/commit"&&r.URL.Path!="/adopted"){http.Error(w,"unsupported request",http.StatusBadRequest);return}
 if r.Header.Get("Content-Type")!="application/json"{http.Error(w,"invalid document",http.StatusBadRequest);return}
 raw,err:=io.ReadAll(http.MaxBytesReader(w,r.Body,12<<20));if err!=nil{http.Error(w,"invalid document",http.StatusBadRequest);return}
 normal,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normal,raw){http.Error(w,"invalid document",http.StatusBadRequest);return}
 var packet replacementDetectionBridgeRequest
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&packet)!=nil{http.Error(w,"invalid document",http.StatusBadRequest);return}
 scope,_,task,_,err:=b.source.Plan.PreparePublication(r.Context())
 if err!=nil||packet.SchemaVersion!=1||packet.Tenant!=scope.Tenant||packet.Run!=scope.Run||packet.Task!=task.ID{http.Error(w,"foreign scope",http.StatusBadRequest);return}
 if r.URL.Path!="/commit"&&(len(packet.Inventory)>0||packet.Digest!=""||len(packet.Evidence)>0||len(packet.Root)>0){http.Error(w,"unexpected payload",http.StatusBadRequest);return}
 var response any
 switch r.URL.Path{
 case "/evidence":
  var evidence []byte;evidence,err=b.source.ReadAdoptionEvidence(r.Context(),packet.Attempt,packet.Fence,packet.Result)
  if err==nil{response=map[string]any{"schema_version":1,"evidence":json.RawMessage(evidence)}}
 case "/commit":
  err=b.source.CommitReplayedAdoption(r.Context(),packet.Attempt,packet.Fence,packet.Result,packet.Inventory,packet.Digest,packet.Evidence,packet.Root)
  response=map[string]any{"schema_version":1,"committed":true}
 case "/adopted":
  if packet.Attempt!=0||packet.Fence!=0||packet.Result!=uuid.Nil{http.Error(w,"unexpected attempt",http.StatusBadRequest);return}
  var adopted bool;adopted,err=b.source.WasAdopted(r.Context());response=map[string]any{"schema_version":1,"adopted":adopted}
 }
 if err!=nil{http.Error(w,"adoption unavailable",http.StatusConflict);return}
 encoded,err:=canonical.Marshal(response);if err!=nil{http.Error(w,"adoption unavailable",http.StatusInternalServerError);return}
 w.Header().Set("Content-Type","application/json");w.WriteHeader(http.StatusOK);_,_=w.Write(encoded)
}
