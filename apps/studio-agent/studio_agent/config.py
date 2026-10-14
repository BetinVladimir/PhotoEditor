"""Bounded configuration parsing before keychain or network access."""

import json
import uuid
from pathlib import Path

from .http import origin
from .raw_policy import canonical_raw_policy


_REQUIRED = frozenset({"key_id", "device_id", "tenant_id", "project_id", "session_id",
                       "collection_id", "api_origin", "storage_origins", "watch_root", "journal_path"})
_OPTIONAL = frozenset({"development", "server_origin", "ca_bundle", "max_queued_bytes",
                       "raw_development"})


def _object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate agent config field")
        result[key] = value
    return result


def _constant(value):
    raise ValueError("nonfinite agent config value")


def load_config(path: Path) -> dict:
    with path.open("rb") as file:
        raw = file.read(65537)
    if len(raw) > 65536:
        raise ValueError("agent config exceeds limit")
    try:
        config = json.loads(raw, object_pairs_hook=_object, parse_constant=_constant)
    except (ValueError, UnicodeError, RecursionError):
        raise ValueError("invalid agent config JSON") from None
    if (not isinstance(config, dict) or not _REQUIRED <= config.keys()
            or config.keys() - (_REQUIRED | _OPTIONAL)):
        raise ValueError("invalid agent config fields")
    for field in ("key_id", "device_id", "tenant_id", "project_id", "session_id", "collection_id"):
        value = config[field]
        if not isinstance(value, str):
            raise ValueError("invalid agent scope identity")
        try:
            parsed = uuid.UUID(value)
        except ValueError:
            raise ValueError("invalid agent scope identity") from None
        if parsed.int == 0:
            raise ValueError("invalid agent scope identity")
        config[field] = str(parsed)
    development = config.setdefault("development", False)
    if type(development) is not bool:
        raise ValueError("development must be boolean")
    for field in ("api_origin", "server_origin"):
        if field in config:
            if not isinstance(config[field], str):
                raise ValueError("invalid agent origin")
            config[field] = origin(config[field], development)
    storage = config["storage_origins"]
    if (not isinstance(storage, list) or not 1 <= len(storage) <= 32
            or any(not isinstance(value, str) for value in storage)):
        raise ValueError("invalid storage origin allowlist")
    config["storage_origins"] = [origin(value, development) for value in storage]
    for field in ("watch_root", "journal_path", "ca_bundle"):
        if field in config:
            value = config[field]
            if not isinstance(value, str) or not value.strip() or "\x00" in value:
                raise ValueError("invalid agent path")
    if not development and "ca_bundle" not in config:
        raise ValueError("production capture requires operator CA bundle")
    budget = config.setdefault("max_queued_bytes", 100 * 1024**3)
    if type(budget) is not int or not 1 <= budget <= 2**53 - 1:
        raise ValueError("invalid queue byte budget")
    if "raw_development" in config:
        config["raw_development"] = canonical_raw_policy(config["raw_development"])
    return config
