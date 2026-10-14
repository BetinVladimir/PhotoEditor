"""V29 measured film-base conversion intent; calibration is an input pin."""
from typing import Annotated, Literal
from pydantic import Field
from photo_worker.contracts.base import ArtifactRef, Contract, Unit
from photo_worker.operations.session.contracts import VersionBinding

FilmBaseChannel = Annotated[float, Field(ge=1e-6, le=1)]


class OpticalNegativeParams(Contract):
    schema_version: Literal[1]
    base: VersionBinding
    strength: Unit
    film_base_rgb: tuple[FilmBaseChannel, FilmBaseChannel, FilmBaseChannel]
    calibration: ArtifactRef
    exposure_ev: Annotated[float, Field(ge=-3, le=3)]


from photo_worker.contracts.base import SHA256
DensityCoefficient = Annotated[float, Field(ge=-10, le=10)]
DensityRow = tuple[DensityCoefficient, DensityCoefficient, DensityCoefficient]
DensityRange = Annotated[float, Field(ge=0.001, le=10)]


class NegativeCalibration(Contract):
    schema_version: Literal[1]
    input_icc_sha: SHA256
    density_matrix: tuple[DensityRow, DensityRow, DensityRow]
    density_offset: DensityRow
    density_range: tuple[DensityRange, DensityRange, DensityRange]
    qualification: ArtifactRef
