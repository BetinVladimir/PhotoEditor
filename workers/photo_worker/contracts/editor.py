from typing import Literal

from pydantic import model_validator

from .base import ArtifactRef, Contract
from .override import OverrideModules


class EditorPreviewParams(Contract):
    image: ArtifactRef
    style: ArtifactRef | None = None
    modules: OverrideModules
    scale: Literal["proxy", "full"]
    roi: tuple[int, int, int, int] | None = None

    @model_validator(mode="after")
    def valid_roi(self):
        if self.roi is not None:
            x, y, width, height = self.roi
            if min(x, y) < 0 or min(width, height) < 1:
                raise ValueError("Invalid preview ROI")
        return self
