"""Ordinary leased semantic installation using real frozen scene evidence."""
from contextlib import contextmanager,ExitStack
from copy import deepcopy
from dataclasses import replace
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.executor import OperationFailure
from photo_worker.algorithms.semantic_scene import ScenePolicy
from photo_worker.algorithms.semantic_scene_projection import verify_full_scene_arrays
from .adapter import SemanticTransferInstallation,sql_semantic_artifact_factory
from .scene_documents import bind_scene_documents,require_scene_pin
from .resources import semantic_transfer_resources
from .scene_refusal import scientific_scene_outcome,SceneUnsupported


def semantic_scene_worker_factory(*,database,objects,bucket,measurements,verify_color,verify_installed_runtime):
    if not callable(getattr(database,'semantic_scene',None)) or not callable(verify_installed_runtime):
        raise ValueError('SEMANTIC_SCENE_INSTALLATION_INVALID')
    def install(ctx,artifacts):
        active=ctx.check_active;owned=deepcopy(ctx.lease);arrays=None;runtime=None
        def scene(params,intent,calibration):
            active();verify_installed_runtime(active);active()
            frozen=owned.execution_profile.get('semantic_scene')
            current=database.semantic_scene(deepcopy(owned));active()
            if frozen is None or current is None or canonical_bytes(current)!=canonical_bytes(frozen):
                raise OperationFailure('CONFIG_INVALID')
            if arrays is None or runtime is None:raise ValueError('SEMANTIC_SCENE_ARRAY_LIFETIME_INVALID')
            from photo_worker.contracts.images import WorkingImage
            images=tuple(artifacts.contract(binding.source,WorkingImage) for binding in (params.reference,params.target))
            record=database.semantic_report_evidence(deepcopy(owned));active()
            if not isinstance(record,dict) or 'policy' not in record:raise ValueError('SEMANTIC_SCENE_POLICY_UNAVAILABLE')
            evidence,labels,depth=bind_scene_documents(frozen,params,intent,images,artifacts.contract,
                policy_evidence_shas=record['policy'].get('evidence_shas'))
            require_scene_pin(evidence,runtime)
            edge=min(evidence.policy.proxy_edge,*arrays.reference.shape[:2],*arrays.target.shape[:2])
            required=semantic_transfer_resources(arrays.reference.shape[:2],arrays.target.shape[:2])
            estimate=owned.execution_profile.get('resource_estimate',{})
            if type(estimate.get('ram_bytes')) is not int or estimate['ram_bytes']<required['ram_bytes']+512*1024**2+256*edge**2:
                raise OperationFailure('RAM_ADMISSION_EXCEEDED')
            refusal=None
            with ExitStack() as stack:
                def mapped(ref,shape,dtype):
                    local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
                    return stack.enter_context(read_array(local.path,shape,dtype,active,unit_interval=dtype=='float32'))
                shapes=tuple((image.canvas.height,image.canvas.width) for image in images)
                labs=tuple(mapped(label.labels,shape,'uint16') for label,shape in zip(labels,shapes))
                depths=tuple(mapped(value.artifact,shape,'float32') for value,shape in zip(depth,shapes))
                # Depth invalidity never becomes an implicit planar sample. The
                # conservative initial policy refuses any invalid target/source.
                for value,shape in zip(depth,shapes):
                    if value.invalid_mask is not None:
                        invalid=mapped(value.invalid_mask,shape,'float32')
                        for top in range(0,shape[0],256):
                            for left in range(0,shape[1],256):
                                active()
                                if np.any(invalid[top:top+256,left:left+256]>0):
                                    raise ValueError('SEMANTIC_SCENE_DEPTH_INVALID')
                policy=ScenePolicy(frozenset(evidence.policy.background_labels),evidence.policy.min_support,
                    evidence.policy.flow_error_px,evidence.policy.homography_error_px,evidence.policy.depth_plane_error)
                try:
                    with scientific_scene_outcome():
                        verify_full_scene_arrays(arrays.reference,arrays.target,*labs,*depths,arrays.mask,
                            arrays.reference_protected,arrays.target_protected,policy=policy,proxy_edge=evidence.policy.proxy_edge,
                            match_error_px_max=calibration.match_error_px_max,match_inlier_min=calibration.match_inlier_min,check_active=active)
                except SceneUnsupported as outcome:refusal=outcome
            active()
            if canonical_bytes(database.semantic_scene(deepcopy(owned)))!=canonical_bytes(frozen):
                raise ValueError('SEMANTIC_SCENE_SCOPE_CHANGED')
            base.verify_runtime();active()
            if refusal is not None:raise refusal
        factory=sql_semantic_artifact_factory(database=database,objects=objects,bucket=bucket,
            verify_color=verify_color,verify_scene=scene,**measurements)
        base=factory(ctx,artifacts)
        @contextmanager
        def load(*args,**kwargs):
            nonlocal arrays,runtime
            active();verify_installed_runtime(active);runtime=base.verify_runtime()
            with base.load_arrays(*args,**kwargs) as opened:
                arrays=opened
                try:yield opened
                finally:arrays=None;runtime=None
        return SemanticTransferInstallation(load,base.verify_runtime,scene)
    return install
