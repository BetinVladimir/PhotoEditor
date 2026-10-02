from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256


class PreviewEntry(Contract):
    preview:ArtifactRef
    media_type:Literal['image/jpeg','image/png','image/webp']
    label:Annotated[str,Field(min_length=1,max_length=160)]


class ContactSheetParams(Contract):
    entries:Annotated[list[PreviewEntry],Field(min_length=1,max_length=10000)]
    locale:Literal['ru','bg','en']
    font:ArtifactRef
    font_lock_id:SHA256
    columns:Annotated[int,Field(ge=1,le=8)]=4
    rows:Annotated[int,Field(ge=1,le=10)]=5
    cell_width:Literal[320]=320
    cell_height:Literal[260]=260
    page_background:Literal['white']='white'

    @model_validator(mode='after')
    def unique_previews(self):
        ids=[(e.preview.artifact_id,e.preview.sha256) for e in self.entries]
        if len(ids)!=len(set(ids)):raise ValueError('Duplicate contact preview')
        if self.font.sha256!=self.font_lock_id:raise ValueError('Font lock mismatch')
        if len(self.entries)>self.columns*self.rows*128:raise ValueError('Contact sheet page limit exceeded')
        return self


class ContactPage(Contract):
    artifact:ArtifactRef
    page:Annotated[int,Field(ge=1,le=128)]
    first_index:Annotated[int,Field(ge=0)]
    item_count:Annotated[int,Field(ge=1,le=80)]
    width:Annotated[int,Field(ge=1,le=2560)]
    height:Annotated[int,Field(ge=1,le=2600)]


class ContactSheet(Contract):
    locale:Literal['ru','bg','en']
    font_lock_id:SHA256
    total_items:Annotated[int,Field(ge=1,le=10000)]
    pages:Annotated[list[ContactPage],Field(min_length=1,max_length=128)]
    format:Literal['png_pages']='png_pages'
    labels_embedded:Literal[True]=True
