from io import BytesIO
from hashlib import sha256
from dataclasses import replace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import MaskRef,ResolvedTransform
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.semantic_replay import verify_semantic_mask_replay,semantic_mask_replay_resources
from test_image_denoise import Artifacts
from test_v2_adoption import Reader


@pytest.mark.parametrize('variant',['valid','translated','changed','source_protected','target_protected','binding','budget','ram'])
def test_semantic_warp_replay_bound_protection_and_cleanup(tmp_path,variant):
    a=Artifacts(tmp_path);reader=Reader();bodies={}
    selected=np.zeros((20,24),np.float32);selected[5:10,6:11]=.5
    guard=np.zeros_like(selected);target_guard=guard.copy();source_guard=guard.copy()
    if variant=='source_protected':source_guard[7,8]=.001
    if variant=='target_protected':target_guard[7,8]=.001
    published=selected.copy()
    if variant=='translated':
        published[:]=0;published[8:13,8:13]=.5
    if variant=='changed':published[0,0]=.001
    def mask(data,protected,source):
        stream=BytesIO();np.save(stream,data,allow_pickle=False);body=stream.getvalue()
        value=ArtifactRef(uuid4(),sha256(body).hexdigest());bodies[value]=body
        reader.add(value,source=source,role='mask_pixels' if source=='output' else None,media='application/x-npy')
        reader.rows[value]=replace(reader.rows[value],bytes=len(body))
        return MaskRef(artifact={'artifact_id':value.artifact_id,'sha256':value.sha256},canvas=a.image.canvas,
            transform_chain=a.image.transform_chain,selector='manual',confidence=1,model_lock_id=None,protected=protected)
    source=mask(selected,False,'input');target=mask(published,False,'output')
    source_protection=mask(source_guard,True,'input');target_protection=mask(target_guard,True,'input')
    reads=[]
    def blocks(ref,max_bytes):
        reads.append(ref);return iter([bodies[ref]])
    reader.iter_bytes=blocks
    transform=ResolvedTransform(schema_version=1,from_canvas_hash=canonical_hash(a.image.canvas),
        to_canvas_hash=canonical_hash(a.image.canvas),matrix=[1,0,0,0,1,0,0,0,1],kind='homography')
    if variant=='translated':transform.matrix[2]=2;transform.matrix[5]=3
    if variant=='binding':target_protection.protected=False
    scratch=tmp_path/'scratch';scratch.mkdir()
    def run():return verify_semantic_mask_replay(reader,source,target,source_protection,target_protection,transform,
        scratch_root=scratch,max_bytes=1 if variant=='budget' else 65536,
        max_ram_bytes=semantic_mask_replay_resources((20,24),(20,24))['ram_bytes']-(1 if variant=='ram' else 0))
    if variant in ('valid','translated'):assert run()==25
    else:
        with pytest.raises(AdoptionInvalid):run()
    assert not list(scratch.iterdir())
    if variant in ('ram','budget'):assert reads==[]


def test_semantic_replay_costs_cover_all_retained_mappings():
    costs=semantic_mask_replay_resources((4000,6000),(6000,8000))
    retained=8*24000000+12*48000000
    assert costs==dict(ram_bytes=retained+64*1024*1024,scratch_bytes=retained+5*8192)
    with pytest.raises(AdoptionInvalid):semantic_mask_replay_resources((8,8),(10001,20000))
