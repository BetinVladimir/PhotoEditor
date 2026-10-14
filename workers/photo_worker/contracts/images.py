"""Portable image/geometry contracts. Pixel arrays always live in artifacts."""
from __future__ import annotations
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, model_validator, model_serializer
from .base import ArtifactRef, Contract, SHA256, Unit, canonical_hash

PositiveInt = Annotated[int, Field(ge=1,le=2**31-1)]
Identifier = Annotated[str, Field(pattern=r'^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$')]
MatrixValue = Annotated[float,Field(ge=-1e12,le=1e12)]
Matrix3 = Annotated[list[MatrixValue], Field(min_length=9,max_length=9)]
Invertibility = Literal['invertible','partial','noninvertible']


class Canvas(Contract):
    width: PositiveInt
    height: PositiveInt
    orientation: Literal[1]
    space_id: Identifier
    source_sha256: SHA256

    @model_validator(mode='after')
    def pixel_count(self):
        if self.width*self.height > 2**53-1:
            raise ValueError('Canvas pixel count exceeds safe integer')
        return self


class Rect(Contract):
    x: Annotated[int,Field(ge=0)]
    y: Annotated[int,Field(ge=0)]
    width: PositiveInt
    height: PositiveInt

    def validate_canvas(self,canvas:Canvas):
        if self.x+self.width>canvas.width or self.y+self.height>canvas.height:
            raise ValueError('Rectangle exceeds canvas')
        return self


class NormalizedRect(Contract):
    x0: Unit
    y0: Unit
    x1: Unit
    y1: Unit

    @model_validator(mode='after')
    def nonempty(self):
        if self.x1<=self.x0 or self.y1<=self.y0:
            raise ValueError('Empty normalized rectangle')
        return self


class TransformBase(Contract):
    from_canvas: Canvas
    to_canvas: Canvas
    invertibility: Invertibility
    valid_region: Rect

    @model_validator(mode='after')
    def output_region(self):
        self.valid_region.validate_canvas(self.to_canvas)
        if self.from_canvas.source_sha256!=self.to_canvas.source_sha256:
            raise ValueError('Transform changes source identity')
        return self


class MatrixTransform(TransformBase):
    kind: Literal['matrix']
    forward_matrix: Matrix3

    @model_validator(mode='after')
    def nonsingular(self):
        a,b,c,d,e,f,g,h,i=self.forward_matrix
        determinant=a*(e*i-f*h)-b*(d*i-f*g)+c*(d*h-e*g)
        scale=max(abs(a),abs(b),abs(d),abs(e),abs(g)*self.from_canvas.width,abs(h)*self.from_canvas.height,abs(i))
        if scale==0 or abs(determinant)/(scale**3)<1e-12:
            raise ValueError('Singular or ill-conditioned transform matrix')
        # Homography denominator must retain its sign across pixel centres.
        den=[g*x+h*y+i for x in (0,self.from_canvas.width-1) for y in (0,self.from_canvas.height-1)]
        if min(den)<=0<=max(den):
            raise ValueError('Homography horizon crosses source canvas')
        return self


class DenseWarpTransform(TransformBase):
    kind: Literal['dense_warp']
    inverse_field: ArtifactRef
    # Absence preserves existing integer-center fields and their chain hashes.
    pixel_centers: Literal['half_pixel'] | None = None

    @model_validator(mode='before')
    @classmethod
    def explicit_centers(cls, value):
        if isinstance(value, dict) and 'pixel_centers' in value and value['pixel_centers'] is None:
            raise ValueError('Explicit null pixel centers')
        return value

    @classmethod
    def __get_pydantic_json_schema__(cls, core_schema, handler):
        schema = handler(core_schema)
        schema['properties']['pixel_centers'] = {'type': 'string', 'const': 'half_pixel'}
        return schema

    @model_serializer(mode='wrap')
    def serialize_centers(self, handler):
        value = handler(self)
        if self.pixel_centers is None:
            value.pop('pixel_centers', None)
        return value


class CropTransform(TransformBase):
    kind: Literal['crop']
    crop_rect: Rect

    @model_validator(mode='after')
    def matching_crop(self):
        self.crop_rect.validate_canvas(self.from_canvas)
        if (self.crop_rect.width,self.crop_rect.height)!=(self.to_canvas.width,self.to_canvas.height):
            raise ValueError('Crop dimensions disagree with output canvas')
        return self


class ResizeTransform(TransformBase):
    kind: Literal['resize']
    pixel_centers: Literal['half_pixel']


TransformStep=Annotated[MatrixTransform|DenseWarpTransform|CropTransform|ResizeTransform,Field(discriminator='kind')]


class TransformChain(Contract):
    steps: Annotated[list[TransformStep],Field(max_length=128)]
    hash: SHA256

    @model_validator(mode='after')
    def verify_chain(self):
        for left,right in zip(self.steps,self.steps[1:]):
            if left.to_canvas!=right.from_canvas:
                raise ValueError('Discontinuous transform chain')
        expected=canonical_hash({'steps':[s.model_dump(mode='json') for s in self.steps]})
        if self.hash!=expected:
            raise ValueError('Transform chain hash mismatch')
        return self

    @classmethod
    def create(cls,steps:list[TransformStep]):
        digest=canonical_hash({'steps':[s.model_dump(mode='json') for s in steps]})
        return cls(steps=steps,hash=digest)

    def validate_target(self,canvas:Canvas):
        self.verify_chain()
        if self.steps and self.steps[-1].to_canvas!=canvas:
            raise ValueError('Transform chain does not end at target canvas')
        return self


class ColorSpace(Contract):
    profile_id: Identifier
    icc_sha256: SHA256
    encoding: Literal['gamma24','linear']

    @model_validator(mode='after')
    def known_profile_encoding(self):
        expected={'work_rec2020_gamma24_v1':'gamma24','linear_srgb_v1':'linear'}
        if self.profile_id not in expected or self.encoding!=expected[self.profile_id]:
            raise ValueError('Unsupported working profile or encoding')
        return self


class WorkingImage(Contract):
    pixels: ArtifactRef
    canvas: Canvas
    color: ColorSpace
    dtype: Literal['uint16','float32']
    alpha: ArtifactRef|None
    transform_chain: TransformChain

    @model_validator(mode='after')
    def coherent_image(self):
        self.transform_chain.validate_target(self.canvas)
        return self


class MaskRef(Contract):
    artifact: ArtifactRef
    canvas: Canvas
    transform_chain: TransformChain
    object_id: UUID|None=None
    selector: Identifier
    confidence: Unit
    model_lock_id: SHA256|None
    protected: bool

    @model_validator(mode='after')
    def coherent_mask(self):
        self.transform_chain.validate_target(self.canvas)
        return self


class RelativeDepth(Contract):
    artifact: ArtifactRef
    float32_shape: Annotated[list[PositiveInt],Field(min_length=2,max_length=2)]
    invalid_mask: ArtifactRef|None
    canvas: Canvas
    transform_chain: TransformChain
    normalization: Literal['minmax_valid']

    @model_validator(mode='after')
    def shape_matches(self):
        if self.float32_shape!=[self.canvas.height,self.canvas.width]:
            raise ValueError('Depth dimensions disagree with canvas')
        self.transform_chain.validate_target(self.canvas)
        return self


class FaceBox(Contract):
    object_id: UUID
    bbox_normalized: NormalizedRect
    confidence: Unit


class Landmark(Contract):
    x: Unit
    y: Unit
    confidence: Unit


class Face68(Contract):
    object_id: UUID
    points: Annotated[list[Landmark],Field(min_length=68,max_length=68)]
    canvas: Canvas
    transform_chain: TransformChain

    @model_validator(mode='after')
    def chain_matches(self):
        self.transform_chain.validate_target(self.canvas)
        return self


class Pose17(Contract):
    object_id: UUID
    points: Annotated[list[Landmark],Field(min_length=17,max_length=17)]
    canvas: Canvas
    transform_chain: TransformChain

    @model_validator(mode='after')
    def chain_matches(self):
        self.transform_chain.validate_target(self.canvas)
        return self


class Stats20(Contract):
    source_image: ArtifactRef|None=None
    feature_version: Literal['stats20_v1']
    values: Annotated[list[float],Field(min_length=20,max_length=20)]
    coverage_bins: Annotated[list[bool],Field(min_length=512,max_length=512)]
    mask_confidence: Unit
    warnings: Annotated[list[Identifier],Field(max_length=64)]


class ResolvedTransform(Contract):
    """V2 source/target canvas-bound planar geometry document."""
    schema_version: Literal[1]
    from_canvas_hash: SHA256
    to_canvas_hash: SHA256
    matrix: Annotated[list[Annotated[float,Field(ge=-1e8,le=1e8,allow_inf_nan=False)]],Field(min_length=9,max_length=9)]
    kind: Literal['homography','similarity']
