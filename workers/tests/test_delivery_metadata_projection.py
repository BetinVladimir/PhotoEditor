import pytest
from photo_worker.contracts.metadata import MetadataDocument,GPSMetadata
from photo_worker.v2.delivery_metadata_projection import project_delivery_metadata,verify_delivery_metadata_projection


def source():
    return MetadataDocument(author='Author',copyright='Copyright',camera_make='Make',camera_model='Model',
        lens='Lens',datetime_original='2026:10:12 10:00:00',gps=GPSMetadata(latitude=42,longitude=23))


def test_delivery_metadata_policy_projection_and_owned_gps():
    document=source()
    assert project_delivery_metadata(document,'strip')==MetadataDocument()
    assert project_delivery_metadata(document,'copyright')==MetadataDocument(author='Author',copyright='Copyright')
    assert project_delivery_metadata(document,'approved_exif')==MetadataDocument()
    projected=project_delivery_metadata(document,'approved_exif',approved_fields=('gps','camera_model'))
    assert projected==MetadataDocument(gps=document.gps,camera_model='Model')
    projected.gps.latitude=0
    assert document.gps.latitude==42


@pytest.mark.parametrize('fields',[['gps'],('gps','gps'),('serial_number',),(None,),('gps',)*8])
def test_delivery_metadata_refuses_invalid_approval(fields):
    with pytest.raises(ValueError):project_delivery_metadata(source(),'approved_exif',approved_fields=fields)


def test_delivery_metadata_verifier_refuses_extra_or_changed_fields():
    document=source();fields=('author','camera_model')
    expected=project_delivery_metadata(document,'approved_exif',approved_fields=fields)
    assert verify_delivery_metadata_projection(expected,document,'approved_exif',approved_fields=fields) is True
    expected.gps=document.gps
    with pytest.raises(ValueError):verify_delivery_metadata_projection(expected,document,'approved_exif',approved_fields=fields)
    expected.gps=None;expected.author='substitution'
    with pytest.raises(ValueError):verify_delivery_metadata_projection(expected,document,'approved_exif',approved_fields=fields)
    with pytest.raises(ValueError):project_delivery_metadata(document,'unknown')
