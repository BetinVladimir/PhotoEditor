from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract


Text=Annotated[str,Field(min_length=1,max_length=256)]


class GPSMetadata(Contract):
    latitude:Annotated[float,Field(ge=-90,le=90)]
    longitude:Annotated[float,Field(ge=-180,le=180)]
    altitude_m:Annotated[float,Field(ge=-1000,le=100000)]|None=None


class MetadataDocument(Contract):
    author:Text|None=None
    copyright:Text|None=None
    camera_make:Text|None=None
    camera_model:Text|None=None
    lens:Text|None=None
    datetime_original:Annotated[str,Field(pattern=r'^\d{4}:\d{2}:\d{2} \d{2}:\d{2}:\d{2}$')]|None=None
    gps:GPSMetadata|None=None


class MetadataParams(Contract):
    encoded:ArtifactRef
    document:ArtifactRef
    policy:Literal['copyright_only','safe_camera']
    copy_gps:bool=False
    tool_id:Literal['exiftool_v1']='exiftool_v1'

    @model_validator(mode='after')
    def gps_policy(self):
        if self.copy_gps and self.policy!='safe_camera':raise ValueError('GPS requires safe_camera policy')
        return self


class MetadataReport(Contract):
    encoded:ArtifactRef
    document:ArtifactRef
    policy:Literal['copyright_only','safe_camera']
    copied_fields:Annotated[list[Literal['author','copyright','camera_make','camera_model','lens','datetime_original','gps']],Field(max_length=7)]
    orientation:Literal[1]
    stale_previews_removed:Literal[True]
    icc_preserved:Literal[True]
