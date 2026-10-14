"""Exact V29 multi-source geometry contracts; no pixel or scope authority."""
from typing import Annotated, Literal
from pydantic import Field, model_validator
import numpy as np
from photo_worker.contracts.base import Contract, ArtifactRef, SHA256, canonical_hash
from photo_worker.contracts.images import Canvas, Rect, MatrixTransform, DenseWarpTransform, TransformChain, WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.algorithms.multi_registration import _matrix


class RegisteredTransform(Contract):
    schema_version: Literal[1]
    source: VersionBinding
    to_canvas_hash: SHA256
    homography: Annotated[list[Annotated[float, Field(ge=-1e8, le=1e8)]], Field(min_length=9, max_length=9)]
    inverse_field: ArtifactRef | None

    @model_validator(mode='after')
    def nonsingular(self):
        matrix = np.asarray(self.homography, np.float64).reshape(3, 3)
        if abs(matrix[2, 2]) <= 1e-10 or abs(np.linalg.det(matrix)) <= 1e-10 or np.linalg.cond(matrix) > 1e8:
            raise ValueError('UNSAFE_REGISTRATION')
        return self


def compile_multi_lineage(sources, bindings, matrices, pixels, alpha, *, operation,
                          reference, output_shape, inverse_fields=None):
    """Return output WorkingImage and ordered registration documents.

    Source bytes, staged NPY shapes/digests, encoded output color and object
    provenance must independently be checked by the publication/adoption path.
    Every nonlinear projection field must be retained as a report/root child.
    """
    if (operation not in ('optical.panorama', 'optical.focus_stack')
            or not isinstance(sources,(list,tuple)) or not 2<=len(sources)<=30
            or operation=='optical.panorama' and len(sources)>20
            or not isinstance(bindings,(list,tuple)) or len(bindings)!=len(sources)
            or not isinstance(matrices,(list,tuple)) or len(matrices)!=len(sources)
            or type(reference) is not int or not 0<=reference<len(sources)
            or not isinstance(output_shape,tuple) or len(output_shape)!=2
            or any(type(v) is not int or v<1 for v in output_shape) or output_shape[0]*output_shape[1]>200000000):
        raise ValueError('MULTI_LINEAGE_INVALID')
    owned_sources=[WorkingImage.model_validate_json(s.model_dump_json()) for s in sources]
    owned_bindings=[VersionBinding.model_validate_json(b.model_dump_json()) for b in bindings]
    pixels=ArtifactRef.model_validate_json(pixels.model_dump_json());alpha=ArtifactRef.model_validate_json(alpha.model_dump_json())
    if inverse_fields is None:inverse_fields=[None]*len(sources)
    if not isinstance(inverse_fields,(list,tuple)) or len(inverse_fields)!=len(sources):
        raise ValueError('MULTI_LINEAGE_INVALID')
    fields=[None if f is None else ArtifactRef.model_validate_json(f.model_dump_json()) for f in inverse_fields]
    if any(f is not None for f in fields) and (operation!='optical.panorama' or any(f is None for f in fields)):
        raise ValueError('MULTI_LINEAGE_INVALID')
    seen=set();transforms=[]
    for source,binding,matrix in zip(owned_sources,owned_bindings,matrices):
        if (source.dtype!='float32' or source.color!=owned_sources[reference].color
                or binding.canvas_hash!=canonical_hash(source.canvas) or binding.source.sha256!=canonical_hash(source)
                or binding.asset_version_id in seen):
            raise ValueError('MULTI_SOURCE_MISMATCH')
        seen.add(binding.asset_version_id)
        transforms.append(_matrix(matrix,(source.canvas.height,source.canvas.width)))
    if pixels.artifact_id==alpha.artifact_id or any(pixels.artifact_id in (s.pixels.artifact_id,b.source.artifact_id) or alpha.artifact_id in (s.pixels.artifact_id,b.source.artifact_id) for s,b in zip(owned_sources,owned_bindings)):
        raise ValueError('MULTI_OUTPUT_ALIAS')
    roots={b.source.artifact_id for b in owned_bindings}
    roots.update(s.pixels.artifact_id for s in owned_sources)
    roots.update(s.alpha.artifact_id for s in owned_sources if s.alpha is not None)
    roots.update(step.inverse_field.artifact_id for s in owned_sources
                 for step in s.transform_chain.steps if isinstance(step, DenseWarpTransform))
    if pixels.artifact_id in roots or alpha.artifact_id in roots:
        raise ValueError('MULTI_OUTPUT_ALIAS')
    field_ids=[f.artifact_id for f in fields if f is not None]
    if len(set(field_ids))!=len(field_ids) or any(i in roots or i in (pixels.artifact_id,alpha.artifact_id) for i in field_ids):
        raise ValueError('MULTI_OUTPUT_ALIAS')
    ref=owned_sources[reference]
    seed={'operation':operation,'reference':reference,'sources':[b.model_dump(mode='json') for b in owned_bindings],
          'matrices':[h.reshape(-1).tolist() for h in transforms],
          'inverse_fields':[None if f is None else f.model_dump(mode='json') for f in fields],
          'height':output_shape[0],'width':output_shape[1]}
    canvas=Canvas(width=output_shape[1],height=output_shape[0],orientation=1,
                  space_id='multi:'+canonical_hash(seed)[:32],source_sha256=ref.canvas.source_sha256)
    steps=list(ref.transform_chain.steps);from_canvas=ref.canvas
    if fields[reference] is not None:
        projection=Canvas(width=ref.canvas.width,height=ref.canvas.height,orientation=1,
            space_id='cylindrical:'+canonical_hash({'canvas':ref.canvas.model_dump(mode='json'),'field':fields[reference].model_dump(mode='json')})[:32],
            source_sha256=ref.canvas.source_sha256)
        steps.append(DenseWarpTransform(kind='dense_warp',from_canvas=ref.canvas,to_canvas=projection,
            invertibility='partial',valid_region=Rect(x=0,y=0,width=projection.width,height=projection.height),
            inverse_field=fields[reference],pixel_centers='half_pixel'))
        from_canvas=projection
    steps.append(MatrixTransform(kind='matrix',from_canvas=from_canvas,to_canvas=canvas,
        invertibility='partial',valid_region=Rect(x=0,y=0,width=canvas.width,height=canvas.height),
        forward_matrix=transforms[reference].reshape(-1).tolist()))
    image=WorkingImage(pixels=pixels,alpha=alpha,canvas=canvas,color=ref.color,dtype='float32',
                       transform_chain=TransformChain.create(steps))
    registrations=tuple(RegisteredTransform(schema_version=1,source=b,to_canvas_hash=canonical_hash(canvas),
        homography=h.reshape(-1).tolist(),inverse_field=f) for b,h,f in zip(owned_bindings,transforms,fields))
    return image,registrations
