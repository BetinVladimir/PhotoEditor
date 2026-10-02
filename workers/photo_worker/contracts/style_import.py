from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256


class StyleImportParams(Contract):
    package:ArtifactRef
    target_style_id:UUID
    assets_manifest:ArtifactRef|None=None
    package_version:Literal['style_import_v1']='style_import_v1'


class PortableAsset(Contract):
    portable_asset_id:Annotated[str,Field(min_length=1,max_length=120,pattern=r'^[A-Za-z0-9][A-Za-z0-9._-]*$')]
    path:Annotated[str,Field(min_length=8,max_length=512)]
    sha256:SHA256
    media_type:Literal['image/png','image/jpeg','application/octet-stream']


class PortableManifest(Contract):
    schema_version:Literal['style-assets/v1']='style-assets/v1'
    assets:Annotated[list[PortableAsset],Field(max_length=128)]

    @model_validator(mode='after')
    def unique(self):
        ids=[x.portable_asset_id for x in self.assets];paths=[x.path for x in self.assets]
        if len(ids)!=len(set(ids)) or len(paths)!=len(set(paths)):raise ValueError('Duplicate portable asset')
        return self


class ImportedAsset(Contract):
    portable_asset_id:str
    artifact:ArtifactRef
    sha256:SHA256
    media_type:str


class StyleImportReport(Contract):
    schema_version:Literal['style-import-report/v1']='style-import-report/v1'
    source_sha256:SHA256
    config_hash:SHA256
    package_kind:Literal['json','zip']
    assets:list[ImportedAsset]
    warnings:list[str]
