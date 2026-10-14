"""Independent installed-catalog proof for server-frozen Region execution."""
from dataclasses import dataclass
from hashlib import sha256
import os
from pathlib import Path
import stat
from typing import Annotated, Literal

from pydantic import Field, model_validator
from photo_worker.contracts.base import Contract, SHA256, canonical_bytes, canonical_hash, parse_json
from photo_worker.runtime.executor import OperationFailure
from .resources import region_source_resources

Safe = Annotated[int, Field(ge=0, le=2**53-1)]
Positive = Annotated[int, Field(ge=1, le=2**53-1)]


class Qualification(Contract):
    runtime_lock_sha: SHA256
    model_lock_sha: SHA256
    icc_lock_sha: SHA256
    device_profile_sha: SHA256
    dataset_sha: SHA256
    policy_sha: SHA256
    backend: Literal['cuda']


class Peak(Contract):
    ram_bytes: Safe
    scratch_bytes: Safe
    vram_bytes: Positive
    output_bytes: Safe


class Sample(Contract):
    width: Positive
    height: Positive
    operator_peak: Peak

    @model_validator(mode='after')
    def dimensions(self):
        if self.width*self.height > 200_000_000:
            raise ValueError('REGION_CALIBRATION_DIMENSIONS_INVALID')
        return self


class Calibration(Contract):
    qualification: Qualification
    workload_sha: SHA256
    evidence_sha: SHA256
    samples: Annotated[list[Sample], Field(min_length=1, max_length=256)]

    @model_validator(mode='after')
    def unique(self):
        if len({(s.width,s.height) for s in self.samples}) != len(self.samples):
            raise ValueError('REGION_CALIBRATION_DUPLICATE_SAMPLE')
        return self


class Manifest(Contract):
    schema_version: Literal[1]
    profiles: Annotated[list[Calibration], Field(min_length=1, max_length=128)]

    @model_validator(mode='before')
    @classmethod
    def exact_version(cls,value):
        if not isinstance(value,dict) or type(value.get('schema_version')) is not int:
            raise ValueError('REGION_CALIBRATION_VERSION_INVALID')
        return value

    @model_validator(mode='after')
    def unique(self):
        if len({(canonical_hash(p.qualification),p.workload_sha) for p in self.profiles}) != len(self.profiles):
            raise ValueError('REGION_CALIBRATION_DUPLICATE_PROFILE')
        return self


class CalibrationPin(Contract):
    qualification: Qualification
    catalog_sha: SHA256
    calibration_sha: SHA256
    workload_sha: SHA256
    evidence_sha: SHA256
    sample: Sample


def region_workload_sha(recipe):
    return canonical_hash(dict(base_style=recipe.base_style.model_dump(mode='json'),
        effective_delta=recipe.effective_delta.model_dump(mode='json'),
        renderer_version=recipe.renderer_version, model_lock_sha=recipe.model_lock_sha,
        icc_lock_sha=recipe.icc_lock_sha))


def _read(path):
    if not isinstance(path,Path) or not path.is_absolute():
        raise ValueError('REGION_CALIBRATION_PATH_INVALID')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(fd,'rb') as stream:
        before=os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_nlink!=1 or not 2<=before.st_size<=1_048_576:
            raise ValueError('REGION_CALIBRATION_FILE_INVALID')
        raw=stream.read(1_048_577)
        after=os.fstat(stream.fileno())
        if len(raw)!=before.st_size or (after.st_size,after.st_mtime_ns)!=(before.st_size,before.st_mtime_ns):
            raise ValueError('REGION_CALIBRATION_FILE_CHANGED')
        return raw


@dataclass(frozen=True, init=False)
class PinnedRegionCalibrationCatalog:
    _raw: bytes
    _sha: str
    _path: Path | None

    def __init__(self,raw:bytes,expected_sha:str,*,path:Path|None=None):
        if type(raw) is not bytes or not 2<=len(raw)<=1_048_576 or sha256(raw).hexdigest()!=expected_sha:
            raise ValueError('REGION_CALIBRATION_PIN_MISMATCH')
        value=parse_json(raw)
        if canonical_bytes(value)!=raw:
            raise ValueError('REGION_CALIBRATION_NONCANONICAL')
        Manifest.model_validate(value)
        object.__setattr__(self,'_raw',raw)
        object.__setattr__(self,'_sha',expected_sha)
        object.__setattr__(self,'_path',path)
        if path is not None:
            self.verify_installed()

    @classmethod
    def load(cls,path,expected_sha):
        return cls(_read(path),expected_sha,path=path)

    def verify_installed(self):
        if self._path is not None and _read(self._path)!=self._raw:
            raise OperationFailure('REGION_CALIBRATION_PIN_MISMATCH')

    def admit(self,profile,params,source=None):
        """Before pixels prove membership; with source prove exact selection/budget."""
        try:
            self.verify_installed()
            pin=CalibrationPin.model_validate(profile['region_qualification'])
            q=pin.qualification
            recipe=params.frozen_recipe
            if (pin.catalog_sha!=self._sha or pin.workload_sha!=region_workload_sha(recipe)
                    or q.model_lock_sha!=recipe.model_lock_sha or q.icc_lock_sha!=recipe.icc_lock_sha
                    or any(profile.get(k)!=v for k,v in dict(backend=q.backend,
                        renderer_version='v2.2',region_runtime_sha256=q.runtime_lock_sha,
                        model_lock_sha=q.model_lock_sha,icc_lock_sha=q.icc_lock_sha,
                        alpha_mode='normalize_uint16').items())):
                raise ValueError('REGION_CALIBRATION_PIN_MISMATCH')
            manifest=Manifest.model_validate(parse_json(self._raw))
            candidates=[p for p in manifest.profiles if p.qualification==q and p.workload_sha==pin.workload_sha]
            if len(candidates)!=1:
                raise ValueError('REGION_CALIBRATION_UNAVAILABLE')
            calibration=candidates[0]
            if (canonical_hash(calibration)!=pin.calibration_sha or calibration.evidence_sha!=pin.evidence_sha
                    or pin.sample not in calibration.samples):
                raise ValueError('REGION_CALIBRATION_PIN_MISMATCH')
            estimate=profile.get('resource_estimate')
            if (not isinstance(estimate,dict) or set(estimate)!=set(('pixel_count','ram_bytes','scratch_bytes','vram_bytes','output_bytes'))
                    or any(type(v) is not int or not 0<=v<=2**53-1 for v in estimate.values())
                    or estimate['pixel_count']<1 or estimate['ram_bytes']<3*1024**3
                    or estimate['vram_bytes']!=pin.sample.operator_peak.vram_bytes):
                raise ValueError('REGION_CALIBRATION_BUDGET_INVALID')
            if source is not None:
                width,height=source.canvas.width,source.canvas.height
                covered=[s for s in calibration.samples if s.width>=width and s.height>=height]
                selected=min(covered,key=lambda s:(s.width*s.height,s.width)) if covered else None
                if selected!=pin.sample:
                    raise ValueError('REGION_CALIBRATION_SAMPLE_MISMATCH')
                src=region_source_resources(width,height,has_alpha=source.alpha is not None,alpha_mode='normalize_uint16')
                pixels=width*height
                roi=params.geometry.output_width*params.geometry.output_height
                peak=pin.sample.operator_peak
                expected=dict(pixel_count=pixels,ram_bytes=max(3*1024**3,src['ram_bytes']+16*pixels+64*roi)+peak.ram_bytes,
                    scratch_bytes=src['scratch_bytes']+16*pixels+5*roi+26*1024**2+peak.scratch_bytes,
                    vram_bytes=peak.vram_bytes,output_bytes=24*1024**2+peak.output_bytes)
                if estimate!=expected:
                    raise ValueError('REGION_CALIBRATION_BUDGET_INVALID')
        except OperationFailure:
            raise
        except (ValueError,KeyError,TypeError,AttributeError,OSError) as error:
            raise OperationFailure('REGION_CALIBRATION_UNAVAILABLE') from error
