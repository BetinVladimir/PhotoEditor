from __future__ import annotations
from typing import Annotated,Literal
from pydantic import Field,field_validator,model_validator
from .base import Contract,ArtifactRef,SHA256

SOURCE_SUFFIXES=frozenset({'.jpg','.jpeg','.png','.tif','.tiff','.nef','.nrw','.crw','.cr2','.cr3','.arw','.srf','.sr2','.mrw','.dng','.rw2','.orf','.raf','.pef','.srw','.3fr','.fff','.iiq','.mos','.kdc','.mef','.x3f'})

class SourceParams(Contract):
    source:ArtifactRef
    expected_bytes:Annotated[int,Field(ge=1,le=536870912)]
    suffix:str

    @field_validator('suffix')
    @classmethod
    def source_suffix(cls,value):
        if value not in SOURCE_SUFFIXES:raise ValueError('Unsupported source suffix')
        return value

class VerifyParams(SourceParams):pass
class InspectParams(SourceParams):pass

class RAWDevelopment(Contract):
    schema_version:Literal[1]=1
    demosaic:Literal['ahd','dcb']
    wb_source:Literal['camera','daylight','auto']
    exposure_ev:Annotated[float,Field(ge=-2,le=3)]
    highlight_policy:Literal['clip','blend','reconstruct']
    decoder_version:Annotated[str,Field(max_length=128,pattern=r'^rawpy-[0-9]+\.[0-9]+\.[0-9]+/libraw-[0-9]+\.[0-9]+\.[0-9]+$')]

class DecodeParams(SourceParams):
    assume_profile:Literal['srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1']|None=None
    analyze:bool=False
    tile:Literal[256,512,1024]=512
    raw_development:RAWDevelopment|None=None

class SensorAlignment(Contract):
    camera_id:Annotated[str,Field(pattern=r'^raw-[0-9a-f]{64}$')]
    sensor_width:Annotated[int,Field(ge=8,le=200000)]
    sensor_height:Annotated[int,Field(ge=8,le=200000)]
    crop_x:Annotated[int,Field(ge=0)]
    crop_y:Annotated[int,Field(ge=0)]
    crop_width:Annotated[int,Field(ge=8)]
    crop_height:Annotated[int,Field(ge=8)]
    orientation:Literal[1]

    @model_validator(mode='after')
    def crop_inside_sensor(self):
        if self.crop_x+self.crop_width>self.sensor_width or self.crop_y+self.crop_height>self.sensor_height:raise ValueError('Sensor crop exceeds sensor')
        return self

class AssetMetadata(Contract):
    source:ArtifactRef
    width:Annotated[int,Field(ge=1,le=200000000)]
    height:Annotated[int,Field(ge=1,le=200000000)]
    bit_depth:Literal[8,16]
    has_alpha:bool
    orientation:Annotated[int,Field(ge=1,le=8)]
    icc_sha256:SHA256|None
    format:Literal['jpeg','png','tiff','raw']
    sensor_alignment:SensorAlignment|None=None

    @model_validator(mode='after')
    def pixels(self):
        if self.width*self.height>200000000:raise ValueError('Pixel limit exceeded')
        if self.sensor_alignment is not None and self.format!='raw':raise ValueError('Sensor alignment requires RAW')
        return self

class VerifiedAsset(Contract):
    source:ArtifactRef
    bytes:Annotated[int,Field(ge=1,le=536870912)]
    metadata:AssetMetadata

    @model_validator(mode='after')
    def same_source(self):
        if self.source!=self.metadata.source:raise ValueError('Metadata source mismatch')
        return self
