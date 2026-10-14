"""Source-bound automatic perspective evidence, separate from correction."""
from typing import Literal
from pydantic import model_validator
from .base import Contract
from .optical_perspective import LineSegments
from photo_worker.operations.session.contracts import VersionBinding

class PerspectiveLineDetectionParams(Contract):
    schema_version: Literal[1]
    base: VersionBinding

class PerspectiveLineDetectionResult(Contract):
    schema_version: Literal[1]
    base: VersionBinding
    status: Literal['available','unsupported']
    lines: LineSegments | None
    @model_validator(mode='after')
    def availability(self):
        if (self.status=='available')!=(self.lines is not None):
            raise ValueError('PERSPECTIVE_LINE_RESULT_INVALID')
        return self
