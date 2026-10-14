"""Strict, owned RAW development choice for device upload replay."""

import json
import math
import re
from pathlib import Path


RAW_SUFFIXES = frozenset({".dng", ".cr2", ".nef", ".arw", ".raf", ".orf", ".rw2"})
_FIELDS = frozenset({"schema_version", "demosaic", "wb_source", "exposure_ev",
                     "highlight_policy", "decoder_version"})
_VERSION = re.compile(r"rawpy-[0-9]+\.[0-9]+\.[0-9]+/libraw-[0-9]+\.[0-9]+\.[0-9]+\Z")


def canonical_raw_policy(value: object) -> str:
    if not isinstance(value, dict) or value.keys() != _FIELDS:
        raise ValueError("invalid RAW development fields")
    exposure = value["exposure_ev"]
    if (type(value["schema_version"]) is not int or value["schema_version"] != 1
            or value["demosaic"] not in ("ahd", "dcb")
            or value["wb_source"] not in ("camera", "daylight", "auto")
            or value["highlight_policy"] not in ("clip", "blend", "reconstruct")
            or type(exposure) not in (int, float) or not math.isfinite(exposure)
            or not -2 <= exposure <= 3
            or not isinstance(value["decoder_version"], str)
            or len(value["decoder_version"]) > 128
            or _VERSION.fullmatch(value["decoder_version"]) is None):
        raise ValueError("invalid RAW development choice")
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False)


def frozen_raw_policy(encoded: str | None, filename: str) -> dict | None:
    if encoded is None:
        return None
    if Path(filename).suffix.lower() not in RAW_SUFFIXES or not isinstance(encoded, str):
        raise ValueError("RAW policy is bound to a RAW source")
    try:
        value = json.loads(encoded)
    except (ValueError, TypeError, RecursionError):
        raise ValueError("invalid frozen RAW policy") from None
    if canonical_raw_policy(value) != encoded:
        raise ValueError("frozen RAW policy changed")
    return value
