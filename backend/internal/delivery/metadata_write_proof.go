package delivery

import (
 "bytes"
 "encoding/json"
 "sort"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// MetadataWriteProof is a bounded immutable recompilation record. A proof does
// not approve its own fields: RestoreMetadataWriteProof requires independently
// pinned binding, metadata ref, policy and approval from current admission.
type MetadataWriteProof struct {
 SchemaVersion int `json:"schema_version"`
 Task uuid.UUID `json:"task_id"`
 Encoded v2input.Ref `json:"encoded"`
 EncodedJCS string `json:"encoded_jcs"`
 FrozenJCS string `json:"frozen_jcs"`
 FrozenSHA string `json:"frozen_sha256"`
 Binding v2input.SessionVersionBinding `json:"binding"`
 Metadata v2input.Ref `json:"metadata_source"`
 MetadataJCS string `json:"metadata_jcs"`
 Policy string `json:"policy"`
 ApprovedFields []string `json:"approved_fields"`
 Params v2input.Ref `json:"params"`
 ParamsJCS string `json:"params_jcs"`
 Projection v2input.Ref `json:"projection"`
 ProjectionJCS string `json:"projection_jcs"`
}

func CompileMetadataWriteProof(task uuid.UUID,encoded v2input.Ref,encodedRaw,frozenRaw []byte,pin string,
 binding v2input.SessionVersionBinding,metadata v2input.Ref,metadataRaw []byte,policy string,approved []string)(MetadataWritePlan,[]byte,string,error){
 plan,err:=CompileMetadataWrite(task,encoded,encodedRaw,frozenRaw,pin,binding,metadata,metadataRaw,policy,approved)
 if err!=nil{return MetadataWritePlan{},nil,"",err}
 fields:=append([]string{},approved...);sort.Strings(fields)
 proof:=MetadataWriteProof{SchemaVersion:1,Task:task,Encoded:encoded,EncodedJCS:string(encodedRaw),FrozenJCS:string(frozenRaw),FrozenSHA:pin,
 Binding:binding,Metadata:metadata,MetadataJCS:string(metadataRaw),Policy:policy,ApprovedFields:fields,Params:plan.Params(),ParamsJCS:string(plan.ParamsJCS()),
 Projection:plan.Projection(),ProjectionJCS:string(plan.ProjectionJCS())}
 body,err:=canonical.Marshal(proof)
 if err!=nil||len(body)>8<<20{return MetadataWritePlan{},nil,"",v2input.ErrInvalid}
 return plan,body,digest(body),nil
}

func RestoreMetadataWriteProof(raw []byte,pin string,task uuid.UUID,encoded v2input.Ref,
 binding v2input.SessionVersionBinding,metadata v2input.Ref,policy string,approved []string)(MetadataWritePlan,error){
 invalid:=func()(MetadataWritePlan,error){return MetadataWritePlan{},v2input.ErrInvalid}
 if len(raw)<2||len(raw)>8<<20||!shaPattern.MatchString(pin)||digest(raw)!=pin{return invalid()}
 var proof MetadataWriteProof
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&proof)!=nil||proof.SchemaVersion!=1||proof.Task!=task||proof.Encoded!=encoded||proof.Binding!=binding||proof.Metadata!=metadata||proof.Policy!=policy{return invalid()}
 plan,expected,expectedPin,err:=CompileMetadataWriteProof(task,encoded,[]byte(proof.EncodedJCS),[]byte(proof.FrozenJCS),proof.FrozenSHA,
 binding,metadata,[]byte(proof.MetadataJCS),policy,approved)
 if err!=nil{return MetadataWritePlan{},err}
 if pin!=expectedPin||!bytes.Equal(raw,expected){return invalid()}
 return plan,nil
}
