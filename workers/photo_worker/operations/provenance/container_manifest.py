"""Mandatory exact container-store binding for supported export formats."""
from .png_manifest import verify_png_manifest
from .jpeg_manifest import verify_jpeg_manifest


def verify_container_manifest(stream, mime, manifest, *, check_active=lambda: None):
    verifier={'image/png':verify_png_manifest,'image/jpeg':verify_jpeg_manifest}.get(mime)
    if verifier is None:
        raise ValueError('SIGNER_CONTAINER_MIME')
    verifier(stream,manifest,check_active=check_active)
