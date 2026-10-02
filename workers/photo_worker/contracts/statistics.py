from pydantic import model_validator
from .base import Contract,ArtifactRef
from .images import MaskRef

class StatsParams(Contract):
    image:ArtifactRef
    person:MaskRef

    @model_validator(mode='after')
    def person_mask(self):
        if self.person.selector!='person':raise ValueError('Stats20 requires person selector')
        return self
