from pathlib import Path
from uuid import uuid4
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import GPSMetadata,MetadataDocument,MetadataParams
from photo_worker.operations.metadata.write import build_args,metadata_operations


def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)


def values(policy='safe_camera',copy_gps=False):
    encoded=EncodedImage(artifact=ref(),format='jpeg',width=1200,height=800,icc_id='srgb_v1',icc_sha256='b'*64,alpha=False,metadata_policy='strip')
    document=MetadataDocument(author='Photographer',copyright='Copyright',camera_make='Camera Co',camera_model='Model 1',lens='50mm',datetime_original='2026:01:02 03:04:05',gps=GPSMetadata(latitude=42.1,longitude=23.2,altitude_m=500))
    params=MetadataParams(encoded=ref(),document=ref(),policy=policy,copy_gps=copy_gps)
    return encoded,document,params


def test_exiftool_argv_is_allowlisted_and_shell_independent():
    encoded,document,params=values(copy_gps=True);args,copied=build_args('/locked/exiftool',Path('/attempt/output.jpg'),encoded,document,params)
    assert args[0]=='/locked/exiftool' and args[-2:] == ['--','/attempt/output.jpg']
    assert '-EXIF:all=' in args and '-Orientation#=1' in args and '-ExifImageWidth#=1200' in args
    assert any(a.startswith('-GPSLatitude#=') for a in args) and copied[-1]=='gps'
    assert all('\x00' not in a for a in args)


def test_copyright_policy_never_copies_camera_or_gps():
    encoded,document,params=values('copyright_only',False);args,copied=build_args('exiftool',Path('/x.jpg'),encoded,document,params)
    assert copied==['author','copyright']
    assert not any(a.startswith(('-Make=','-Model=','-LensModel=','-GPS')) for a in args)


def test_runtime_does_not_advertise_metadata_without_exiftool(tmp_path):
    assert metadata_operations(object(),str(tmp_path/'missing-exiftool'))==[]
    tool=tmp_path/'exiftool';tool.write_text('#!/bin/sh\nexit 0\n');tool.chmod(0o700)
    assert len(metadata_operations(object(),str(tool)))==1
