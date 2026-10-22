"""Result-scoped, independently read scene evidence and physical correspondence."""
from hashlib import sha256
from uuid import uuid4
import numpy as np
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.algorithms.semantic_scene import ScenePolicy
from photo_worker.algorithms.semantic_scene_projection import verify_full_scene_arrays
from photo_worker.operations.semantic.scene_documents import bind_scene_documents,require_scene_pin
from photo_worker.operations.semantic.resources import semantic_transfer_resources
from photo_worker.operations.semantic.scene_refusal import scientific_scene_outcome,SceneUnsupported,SceneDecision
from photo_worker.operations.optical.color import to_working_linear
from .adoption import AdoptionInvalid,ArtifactRef,MAX_JSON_BYTES
from .array_evidence import ArrayEvidenceSession
from .semantic_qualification_pin import SemanticQualificationPin


class SemanticSceneAdoption:
    def __init__(self,measurements,verify_runtime,scratch_root,scratch_bytes,ram_bytes):
        if (not callable(verify_runtime) or scratch_root is None or type(scratch_bytes) is not int
                or scratch_bytes<1 or type(ram_bytes) is not int or ram_bytes<1):
            raise ValueError('SEMANTIC_SCENE_ADOPTION_CONFIGURATION_INVALID')
        self.measurements=dict(measurements);self.verify_runtime=verify_runtime
        self.scratch_root=scratch_root;self.scratch_bytes=scratch_bytes;self.ram_bytes=ram_bytes

    def __call__(self,*args):
        raise AdoptionInvalid('SEMANTIC_SCENE_RESULT_SCOPE_REQUIRED')

    def for_result(self,connection,reader,tenant,task,context,check_active):
        scope=(tenant,task,context.attempt,context.fence,context.result_id)
        def scene():
            check_active()
            row=connection.execute('SELECT photo.v27_semantic_adoption_scene(%s,%s,%s,%s,%s)',scope).fetchone()
            check_active()
            if row is None or row[0] is None:raise AdoptionInvalid('SEMANTIC_SCENE_SCOPE_STALE')
            return row[0]
        def record():
            check_active()
            row=connection.execute('SELECT photo.v27_semantic_adoption_report_evidence(%s,%s,%s,%s,%s)',scope).fetchone()
            check_active()
            if row is None or not isinstance(row[0],dict):raise AdoptionInvalid('SEMANTIC_SCENE_POLICY_UNAVAILABLE')
            return row[0]
        def document(ref,model):
            ref=ArtifactRef(ref.artifact_id,ref.sha256)
            check_active();entry=reader.evidence(ref)
            if entry.ref!=ref or entry.source!='input' or entry.media_type!='application/json':
                raise AdoptionInvalid('SEMANTIC_SCENE_DOCUMENT_EVIDENCE_INVALID')
            raw=reader.read_json(ref,MAX_JSON_BYTES);check_active()
            if not isinstance(raw,bytes) or len(raw)!=entry.bytes or sha256(raw).hexdigest()!=ref.sha256:
                raise AdoptionInvalid('SEMANTIC_SCENE_DOCUMENT_BYTES_INVALID')
            return model.model_validate_json(canonical_bytes(parse_json(raw,MAX_JSON_BYTES)))
        def verify(params,intent,calibration,images,selected,guards):
            self.verify_runtime(check_active);frozen=scene();report=record()
            evidence,labels,depth=bind_scene_documents(frozen,params,intent,images,document,
                policy_evidence_shas=report['policy'].get('evidence_shas'))
            pin=SemanticQualificationPin.from_value(report['pin']);require_scene_pin(evidence,pin)
            if any(getattr(pin,key)!=value for key,value in self.measurements.items()):
                raise AdoptionInvalid('SEMANTIC_SCENE_INSTALLED_PIN_MISMATCH')
            shapes=tuple((image.canvas.height,image.canvas.width) for image in images)
            edge=min(evidence.policy.proxy_edge,*shapes[0],*shapes[1])
            conversions=sum(12*h*w+8192 for image,(h,w) in zip(images,shapes) if image.color.encoding=='gamma24')
            required=semantic_transfer_resources(*shapes)['ram_bytes']+conversions+512*1024**2+256*edge**2
            if self.ram_bytes<required:raise AdoptionInvalid('SEMANTIC_SCENE_RAM_EXCEEDED')
            # Unsupported result documents omit replay selection. Resolve the
            # same immutable intent mask; never substitute an empty domain.
            if selected is None:
                from photo_worker.contracts.images import MaskRef
                selected=document(intent.reference_mask,MaskRef)
            with ArrayEvidenceSession(reader,self.scratch_root,self.scratch_bytes,check_active) as session:
                def array(ref,shape,**kwargs):
                    return session.array(ArtifactRef(ref.artifact_id,ref.sha256),shape,source='input',legacy_input_npy=True,**kwargs)
                rgb=[]
                for image,shape in zip(images,shapes):
                    encoded=array(image.pixels,(*shape,3),unit=True)
                    if image.color.encoding=='linear':rgb.append(encoded);continue
                    size=12*shape[0]*shape[1]+8192
                    if size>session.limit-session.used:raise AdoptionInvalid('ADOPTION_SCRATCH_EXCEEDED')
                    session.used+=size
                    linear=np.lib.format.open_memmap(session.directory/(uuid4().hex+'.npy'),mode='w+',dtype='<f4',shape=(*shape,3))
                    session.stack.callback(linear._mmap.close)
                    for top in range(0,shape[0],256):
                        for left in range(0,shape[1],256):
                            check_active();part=np.s_[top:top+256,left:left+256]
                            linear[part]=to_working_linear(encoded[part],image.color)
                    linear.flush();linear.flags.writeable=False;rgb.append(linear)
                labs=tuple(array(value.labels,shape,labels=True) for value,shape in zip(labels,shapes))
                depths=tuple(array(value.artifact,shape,unit=True) for value,shape in zip(depth,shapes))
                for value,shape in zip(depth,shapes):
                    if value.invalid_mask is not None:
                        invalid=array(value.invalid_mask,shape,unit=True)
                        for top in range(0,shape[0],256):
                            for left in range(0,shape[1],256):
                                check_active()
                                if np.any(invalid[top:top+256,left:left+256]>0):raise AdoptionInvalid('SEMANTIC_SCENE_DEPTH_INVALID')
                mask=array(selected.artifact,shapes[0],unit=True)
                protected=tuple(array(value.artifact,shape,unit=True) for value,shape in zip(guards,shapes))
                policy=ScenePolicy(frozenset(evidence.policy.background_labels),evidence.policy.min_support,
                    evidence.policy.flow_error_px,evidence.policy.homography_error_px,evidence.policy.depth_plane_error)
                refusal=None
                try:
                    with scientific_scene_outcome():
                        verify_full_scene_arrays(*rgb,*labs,*depths,mask,*protected,policy=policy,
                            proxy_edge=evidence.policy.proxy_edge,match_error_px_max=calibration.match_error_px_max,
                            match_inlier_min=calibration.match_inlier_min,check_active=check_active)
                except SceneUnsupported as outcome:refusal=str(outcome)
            check_active();self.verify_runtime(check_active)
            if canonical_bytes(scene())!=canonical_bytes(frozen) or canonical_bytes(record())!=canonical_bytes(report):
                raise AdoptionInvalid('SEMANTIC_SCENE_SCOPE_CHANGED')
            return SceneDecision(refusal)
        return verify
