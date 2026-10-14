package exportpreset

import ("testing";"encoding/json";"bytes";"photoeditor/backend/internal/canonical")

func TestMetadataPolicyChangeValidation(t *testing.T){
 for _,mode:=range []string{"strip","copyright","approved_exif"}{if _,err:=validateMetadataPolicyChange(MetadataPolicyChange{"1",mode});err!=nil{t.Fatal(mode,err)}}
 for _,revision:=range []string{"","0","-1","01","+1","1.0","9007199254740991"}{if _,err:=validateMetadataPolicyChange(MetadataPolicyChange{revision,"strip"});err==nil{t.Fatal(revision)}}
 if _,err:=validateMetadataPolicyChange(MetadataPolicyChange{"1","all_exif"});err==nil{t.Fatal("unknown mode")}
}

func TestMetadataPolicyConfigPreservesProcessingParameters(t *testing.T){
 raw:=[]byte(`{"format":"jpeg","icc_id":"srgb_v1","quality":92,"subsampling":"444","alpha":"matte","matte_rgb":[1,1,1],"metadata_policy":"strip","naming_template":"{original_name}_edited","size":{"long_edge":2048,"fit":"contain","allow_upscale":false},"sharpen":{"radius":1,"strength":0.2,"threshold":0.01}}`)
 body,hash,config,err:=compileMetadataPolicyConfig(raw,"approved_exif")
 if err!=nil||config.MetadataPolicy!="approved_exif"{t.Fatal(err)}
 var before,after map[string]json.RawMessage
 json.Unmarshal(raw,&before);json.Unmarshal(body,&after)
 delete(before,"metadata_policy");delete(after,"metadata_policy")
 a,_:=canonical.Marshal(before);b,_:=canonical.Marshal(after)
 if !bytes.Equal(a,b){t.Fatal("processing parameters changed")}
 actual,_:=canonical.Hash(json.RawMessage(body));if actual!=hash{t.Fatal("hash does not match persisted config")}
 for _,invalid:=range []string{"null","[]","{}"}{if _,_,_,err=compileMetadataPolicyConfig([]byte(invalid),"copyright");err==nil{t.Fatal(invalid)}}
}
