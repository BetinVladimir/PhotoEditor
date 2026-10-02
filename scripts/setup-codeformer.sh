#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON_BIN="${PYTHON_BIN:-python3.10}"
REPO="$ROOT/.models/CodeFormer"
VENV="$ROOT/.venv-codeformer"

if [ ! -d "$REPO/.git" ]; then
  git clone --depth 1 https://github.com/sczhou/CodeFormer.git "$REPO"
fi
if [ ! -x "$VENV/bin/python" ]; then
  "$PYTHON_BIN" -m venv "$VENV"
fi
"$VENV/bin/python" -m pip install --upgrade pip
"$VENV/bin/pip" install torch torchvision opencv-python scipy tqdm pyyaml lmdb yapf addict future requests scikit-image basicsr facexlib lpips

# BasicSR 1.4.2 imports a module removed by recent torchvision.
DEGRADATIONS="$VENV/lib/python3.10/site-packages/basicsr/data/degradations.py"
"$VENV/bin/python" -c "from pathlib import Path; p=Path('$DEGRADATIONS'); s=p.read_text(); s=s.replace('from torchvision.transforms.functional_tensor import rgb_to_grayscale','try:\n    from torchvision.transforms.functional_tensor import rgb_to_grayscale\nexcept ModuleNotFoundError:\n    from torchvision.transforms.functional import rgb_to_grayscale'); p.write_text(s)"

cd "$REPO"
# Generates vendored basicsr/version.py; modern setuptools may report a later
# editable-install warning, which is irrelevant to direct inference.
"$VENV/bin/python" basicsr/setup.py --version >/dev/null
"$VENV/bin/python" scripts/download_pretrained_models.py facelib
"$VENV/bin/python" scripts/download_pretrained_models.py CodeFormer
"$VENV/bin/python" -c "from facexlib.utils import load_file_from_url; load_file_from_url(url='https://github.com/xinntao/facexlib/releases/download/v0.1.0/recognition_arcface_ir_se50.pth', model_dir='weights/facelib', save_dir='$VENV/lib/python3.10/site-packages', progress=True, file_name=None)"
echo "CodeFormer + ArcFace runtime ready: $VENV"
