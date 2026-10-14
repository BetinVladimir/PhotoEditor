package delivery

import (
 "encoding/json"
 "fmt"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)

// MetadataChildConfig is the closed frozen non-strip child admission config.
type MetadataChildConfig struct{
 QueueClass string `json:"queue_class"`
 ProjectID uuid.UUID `json:"project_id"`
 Request FrozenExportRequest `json:"v30_delivery_export"`
 ExportID uuid.UUID `json:"export_id"`
 Metadata v2input.Ref `json:"v30_metadata_source"`
 Approved []string `json:"v30_metadata_approved_fields"`
 FrozenSHA string `json:"v30_metadata_frozen_sha256"`
 PublicationSHA string `json:"v30_metadata_publication_sha256"`

}

// CompileMetadataChildConfig freezes references only. SQL child admission must
// independently bind the request to its current parent slot and source publication.
// Existing strip ChildConfig and public admission gates are unchanged.
func CompileMetadataChildConfig(project uuid.UUID,request FrozenExportRequest,exportID uuid.UUID,
 publication MetadataSourcePublication,policy string,approved []string)([]byte,string,error){
 invalid:=func()([]byte,string,error){return nil,"",v2input.ErrInvalid}
 if project==uuid.Nil||exportID==uuid.Nil||request.SchemaVersion!=1||request.ParentRunID==uuid.Nil||
 request.SlotIndex<0||request.SlotIndex>=80000||request.Ordinal<0||request.Ordinal>=5000||
 request.ChildRunID!=uuid.NewSHA1(request.ParentRunID,[]byte(fmt.Sprintf("child:export:%d",request.SlotIndex)))||
 request.Recipe.ArtifactID==uuid.Nil||!shaPattern.MatchString(request.Recipe.SHA256)||!shaPattern.MatchString(request.SnapshotDigest)||
 request.Preset.PresetID==uuid.Nil||request.Preset.Config.ArtifactID==uuid.Nil||request.Preset.Config.SHA256!=request.Preset.PresetSHA||
 policy!="copyright"&&policy!="approved_exif"{return invalid()}
 parsed,err:=exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS),request.Preset.PresetSHA)
 if err!=nil||parsed.MetadataPolicy!=policy{return invalid()}
 var envelope metadataSourcePublicationEnvelope
 if json.Unmarshal(publication.JCS(),&envelope)!=nil||envelope.Scope.Project!=project||
 envelope.Scope.Binding!=request.Binding{return invalid()}
 owned,err:=RestoreMetadataSourcePublication(publication.JCS(),publication.SHA256(),envelope.Scope,
 publication.Ref(),publication.SourceJCS(),policy,approved)
 if err!=nil{return nil,"",err}
 // Canonical freeze sorts and validates the external explicit allowlist.
 var frozen FrozenMetadata
 if json.Unmarshal(owned.FrozenJCS(),&frozen)!=nil{return invalid()}
 raw,err:=canonical.Marshal(MetadataChildConfig{"batch",project,request,exportID,owned.Ref(),frozen.ApprovedFields,digest(owned.FrozenJCS()),owned.SHA256()})
 if err!=nil||len(raw)>2<<20{return invalid()}
 return raw,digest(raw),nil
}
