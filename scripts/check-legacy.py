#!/usr/bin/env python3
"""Guard established image quality code/configs against unintended changes."""
from pathlib import Path
import hashlib,json
root=Path(__file__).resolve().parents[1]
manifest=root/'resources/legacy-quality-baseline.json'
for rel,digest in json.loads(manifest.read_text()).items():
    path=root/rel
    if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest()!=digest:
        raise SystemExit(f'Legacy quality baseline changed: {rel}; explicit regression review required')
print('Legacy quality source and presets unchanged')
