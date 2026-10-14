"""Multi-input exact typed closure checks; live SQL authority is admission's job."""
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import DenseWarpTransform


def verify_multi_source_closure(artifacts,params,images):
    if len(images)!=len(params.frames):raise ValueError('MULTI_SOURCE_MISMATCH')
    roots=artifacts.declared_contract_refs('WorkingImage',30)
    arrays=artifacts.declared_contract_refs('NpyArray',3900)
    declared={}
    for kind,refs in (('document',roots),('array',arrays)):
        for ref in refs:
            prior=declared.get(ref.artifact_id)
            if prior is not None and prior!=(ref.sha256,kind):
                raise ValueError('MULTI_SOURCE_CHILD_ALIAS')
            declared[ref.artifact_id]=(ref.sha256,kind)
    root_ids={binding.source.artifact_id for binding in params.frames}
    versions=set();source_roles={}
    for binding,image in zip(params.frames,images):
        if (image.dtype!='float32' or canonical_hash(image)!=binding.source.sha256
                or canonical_hash(image.canvas)!=binding.canvas_hash or binding.source not in roots
                or binding.asset_version_id in versions):raise ValueError('MULTI_SOURCE_MISMATCH')
        versions.add(binding.asset_version_id)
        required=[image.pixels]
        if image.alpha is not None:required.append(image.alpha)
        required.extend(step.inverse_field for step in image.transform_chain.steps if isinstance(step,DenseWarpTransform))
        if any(ref not in arrays for ref in required):raise ValueError('MULTI_SOURCE_CHILD_UNDECLARED')
        if image.alpha is not None and image.alpha.artifact_id==image.pixels.artifact_id:
            raise ValueError('MULTI_SOURCE_CHILD_ALIAS')
        if any(ref.artifact_id in root_ids for ref in required):
            raise ValueError('MULTI_SOURCE_CHILD_ALIAS')
        if any(declared.get(ref.artifact_id)!=(ref.sha256,'array') for ref in required):
            raise ValueError('MULTI_SOURCE_CHILD_ALIAS')
        for role,refs in (('pixels',[image.pixels]),('alpha',[] if image.alpha is None else [image.alpha]),
                          ('warp_field',[step.inverse_field for step in image.transform_chain.steps
                                         if isinstance(step,DenseWarpTransform)])):
            for ref in refs:
                prior=source_roles.get(ref.artifact_id)
                if prior is not None and prior!=(ref.sha256,role):
                    raise ValueError('MULTI_SOURCE_CHILD_ALIAS')
                source_roles[ref.artifact_id]=(ref.sha256,role)
