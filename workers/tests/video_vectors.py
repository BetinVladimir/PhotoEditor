"""Loader for shared video test vectors (docs/video/vectors)."""

import json
import re
from pathlib import Path

_DIR = Path(__file__).resolve().parents[2] / 'docs' / 'video' / 'vectors'


def load_vector(name: str) -> object:
    if not re.fullmatch(r'[a-z0-9_]+', name):
        raise ValueError(f'bad vector name: {name!r}')
    return json.loads((_DIR / f'{name}.json').read_text(encoding='utf-8'))
