"""Exact bytes for controlled relational learning fixtures (no inference)."""
import io
from hashlib import sha256
from uuid import UUID, uuid4
from pathlib import Path
from photo_worker.contracts.style import StyleV2
import numpy as np
from photo_worker.contracts.base import canonical_bytes, ArtifactRef, canonical_hash
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain,MaskRef
from photo_worker.operations.session.contracts import FaceObservations,DescriptorShard

class MaterialCatalog:
    def __init__(self,db,tenant):
        self.db,self.tenant,self.bodies,self.images,self.frozen = db,tenant,{},{},{}
        self.style=StyleV2.model_validate_json((Path(__file__).resolve().parents[2]/"backend/internal/run/identity_style.json").read_bytes())
        self.style_ref=self.publish(self.style)

    def publish(self,body,role='document',media='application/json',artifact_id=None):
        raw=body if isinstance(body,bytes) else canonical_bytes(body)
        aid=artifact_id or uuid4();digest=sha256(raw).hexdigest()
        self.db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,verified_at) VALUES(%s,%s,'fixture',%s,'v1',%s,%s,%s,'ready',%s,clock_timestamp())",(aid,self.tenant,str(aid),digest,len(raw),media,role))
        self.bodies[str(aid)]=raw
        return {'artifact_id':str(aid),'sha256':digest}

    def array(self,values,role='source'):
        stream=io.BytesIO();np.save(stream,values,allow_pickle=False)
        return self.publish(stream.getvalue(),role,'application/x-npy')

    def source(self,index):
        pixels=self.array(np.full((2,2,3),(index+1)/100,dtype=np.float32))
        canvas=Canvas(width=2,height=2,orientation=1,space_id='fixture:detail',source_sha256=pixels['sha256'])
        chain=TransformChain.create([])
        image=WorkingImage(pixels=ArtifactRef.model_validate_json(canonical_bytes(pixels)),canvas=canvas,
             color=ColorSpace(profile_id='linear_srgb_v1',icc_sha256='f'*64,encoding='linear'),
             dtype='float32',alpha=None,transform_chain=chain)
        ref=self.publish(image,'source')
        protection_pixels=self.array(np.zeros((2,2),dtype=np.float32))
        protection=MaskRef(artifact=ArtifactRef.model_validate_json(canonical_bytes(protection_pixels)),
             canvas=canvas,transform_chain=chain,selector='manual',confidence=1.,model_lock_id=None,protected=True)
        protected=self.publish(protection)
        self.images[ref['artifact_id']]={'image':image,'pixels':pixels,'protected':protected,
            'protected_pixels':protection_pixels,'canvas_hash':canonical_hash(canvas)}
        return ref

    def case(self,version,source,index):
        binding={'asset_version_id':str(version),'source':source,'canvas_hash':self.images[source['artifact_id']]['canvas_hash']}
        faces=FaceObservations.model_validate_json(canonical_bytes(dict(schema_version=1,binding=binding,faces=[])))
        faces_ref=self.publish(faces)
        descriptor=dict(schema_version=1,binding=binding,ordinal=index,captured_at=None,camera_hash=None,
            scene_embedding=[0.]*384,luma_quantiles=[.1,.2,.3,.4,.5],neutral_rgb=None,
            sharpness=1.,clipping_fraction=0.,person_fraction=0.,faces=faces_ref)
        shard=DescriptorShard.model_validate_json(canonical_bytes(dict(schema_version=1,snapshot_digest=canonical_hash({'schema_version':1,'bindings':[binding]}),frames=[descriptor])))
        return binding,faces_ref,self.publish(shard)
