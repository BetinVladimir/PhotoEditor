"""Official, versioned MediaPipe assets. Download is setup-only, never during render."""
import os
from pathlib import Path
import urllib.request
import uuid
from .io import digest

MODELS = {
    'face_landmarker.task': {
        'url': 'https://storage.googleapis.com/mediapipe-models/face_landmarker/face_landmarker/float16/1/face_landmarker.task',
        'sha256': '64184e229b263107bc2b804c6625db1341ff2bb731874b0bcc2fe6544e0bc9ff'},
    'selfie_multiclass.tflite': {
        'url': 'https://storage.googleapis.com/mediapipe-models/image_segmenter/selfie_multiclass_256x256/float32/1/selfie_multiclass_256x256.tflite',
        'sha256': 'c6748b1253a99067ef71f7e26ca71096cd449baefa8f101900ea23016507e0e0'},
}


def model_dir():
    return Path(os.environ.get('PHOTO_MODELS_DIR', '.models')).expanduser().resolve()


def ensure_models(download=False):
    root = model_dir()
    for name, item in MODELS.items():
        path = root / name
        if path.is_file() and digest(path) == item['sha256']:
            continue
        if not download:
            raise ValueError(f'Missing/corrupt model {path}; run photoeditor setup-models')
        root.mkdir(parents=True, exist_ok=True)
        tmp = path.with_name(name + '.' + uuid.uuid4().hex + '.tmp')
        try:
            import ssl
            import certifi
            with urllib.request.urlopen(item['url'], timeout=90, context=ssl.create_default_context(cafile=certifi.where())) as src, open(tmp, 'wb') as dest:
                while block := src.read(1024*1024):
                    dest.write(block)
            if digest(tmp) != item['sha256']:
                raise ValueError(f'Model checksum mismatch: {name}')
            os.replace(tmp, path)
        finally:
            tmp.unlink(missing_ok=True)
    return {name: item['sha256'] for name, item in MODELS.items()}
