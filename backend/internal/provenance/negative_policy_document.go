package provenance

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "os"
 "path/filepath"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2qualification"
)

type SigningNegativePolicyDocument struct{
 Schema string `json:"schema"`
 Policies []SigningNegativePolicyDocumentEntry `json:"policies"`
}
type SigningNegativePolicyDocumentEntry struct{
 PolicyID uuid.UUID `json:"policy_id"`
 PolicySHA string `json:"policy_sha"`
 Locks v2qualification.DeploymentLocks `json:"locks"`
}

// expectedSHA is authenticated process configuration, never derived from this
// document or supplied by a signing request. Only exact canonical bytes qualify.
func DecodeSigningNegativePolicyDocument(ctx context.Context,raw []byte,expectedSHA string)(*SigningNegativePolicyRegistry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !digest(expectedSHA)||len(raw)<2||len(raw)>8<<20||bodyDigest(raw)!=expectedSHA{return nil,ErrInvalid}
 owned:=append([]byte(nil),raw...)
 normalized,err:=canonical.Bytes(owned);if err!=nil||!bytes.Equal(normalized,owned){return nil,ErrInvalid}
 var doc SigningNegativePolicyDocument
 decoder:=json.NewDecoder(bytes.NewReader(owned));decoder.DisallowUnknownFields()
 if decoder.Decode(&doc)!=nil||doc.Schema!="signing-negative-policies/v1"||len(doc.Policies)<1||len(doc.Policies)>256{return nil,ErrInvalid}
 encoded,err:=canonical.Marshal(doc);if err!=nil||!bytes.Equal(encoded,owned){return nil,ErrInvalid}
 entries:=make([]InstalledSigningNegativePolicy,len(doc.Policies))
 for i,entry:=range doc.Policies{entries[i]=InstalledSigningNegativePolicy{PolicyID:entry.PolicyID,PolicySHA:entry.PolicySHA,Locks:entry.Locks}}
 registry,err:=NewSigningNegativePolicyRegistry(entries);if err!=nil{return nil,err}
 return registry,ctx.Err()
}

// Load only a pinned regular deployment file; reject final symlinks and writable
// group/public files. The digest pin, not file ownership, authenticates content.
func LoadSigningNegativePolicyDocument(ctx context.Context,path,expectedSHA string)(*SigningNegativePolicyRegistry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !filepath.IsAbs(path)||!digest(expectedSHA){return nil,ErrInvalid}
 initial,err:=os.Lstat(path);if err!=nil{return nil,err}
 acceptable:=func(info os.FileInfo)bool{return info.Mode().IsRegular()&&info.Mode().Perm()&0022==0&&info.Size()>=2&&info.Size()<=8<<20}
 if !acceptable(initial){return nil,ErrInvalid}
 file,err:=os.Open(path);if err!=nil{return nil,err};defer file.Close()
 opened,err:=file.Stat();if err!=nil{return nil,err}
 if !acceptable(opened)||!os.SameFile(initial,opened){return nil,ErrInvalid}
 raw,err:=io.ReadAll(io.LimitReader(file,(8<<20)+1));if err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err}
 final,err:=file.Stat();if err!=nil{return nil,err}
 current,err:=os.Lstat(path);if err!=nil{return nil,err}
 if !acceptable(final)||!acceptable(current)||!os.SameFile(opened,final)||!os.SameFile(opened,current)||opened.Size()!=final.Size()||opened.Size()!=int64(len(raw))||!opened.ModTime().Equal(final.ModTime())||!opened.ModTime().Equal(current.ModTime()){return nil,ErrInvalid}
 return DecodeSigningNegativePolicyDocument(ctx,raw,expectedSHA)
}
