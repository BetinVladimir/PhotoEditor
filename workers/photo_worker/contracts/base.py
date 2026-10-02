from __future__ import annotations

import hashlib
import json
from typing import Annotated, Any
from uuid import UUID

import rfc8785
from pydantic import BaseModel, ConfigDict, Field

Unit = Annotated[float, Field(ge=0, le=1)]
SHA256 = Annotated[str, Field(pattern=r"^[0-9a-f]{64}$")]
UInt32 = Annotated[int, Field(ge=0, le=2**32-1)]


class Contract(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True, allow_inf_nan=False, validate_assignment=True)


class ArtifactRef(Contract):
    artifact_id: UUID
    sha256: SHA256


def _unique_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"Duplicate JSON key: {key}")
        result[key] = value
    return result


def _invalid_constant(value: str) -> None:
    raise ValueError(f"Non-finite JSON number: {value}")


def parse_json(raw: bytes | str, max_bytes: int = 1_048_576) -> Any:
    data = raw.encode("utf-8") if isinstance(raw, str) else raw
    if len(data) > max_bytes:
        raise ValueError("JSON document exceeds byte limit")
    try:
        value = json.loads(data.decode("utf-8"), object_pairs_hook=_unique_pairs,
                           parse_constant=_invalid_constant)
        # JCS rejects non-finite numbers, unsafe integers and invalid Unicode.
        rfc8785.dumps(value)
        return value
    except (RecursionError, UnicodeError) as exc:
        raise ValueError("Invalid or excessively nested JSON") from exc


def canonical_bytes(value: Any) -> bytes:
    if isinstance(value, BaseModel):
        value = value.model_dump(mode="json", by_alias=True)
    return rfc8785.dumps(value)


def canonical_hash(value: Any) -> str:
    return hashlib.sha256(canonical_bytes(value)).hexdigest()
