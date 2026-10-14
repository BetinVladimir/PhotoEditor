import pytest
from photo_worker.v2.interchange_xmp import parse_xmp, inverse_orientation_crop


def xmp(attributes="", children=""):
    return (f'<x:xmpmeta xmlns:x="adobe:ns:meta/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" '
            f'xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" '
            f'xmlns:tiff="http://ns.adobe.com/tiff/1.0/" xmlns:pe="https://photoeditor.invalid/ns/1.0/">'
            f'<rdf:RDF><rdf:Description {attributes}>{children}</rdf:Description></rdf:RDF></x:xmpmeta>').encode()


def test_rating_pick_and_unknown_label():
    result = parse_xmp(xmp('xmp:Rating="5" pe:Pick="picked" xmp:Label="Custom Blue"'))
    assert result.rating == 5 and result.pick == "picked"
    assert result.unsupported == ("http://ns.adobe.com/xap/1.0/}Label",)


@pytest.mark.parametrize('label', ['Red', 'Yellow', 'Green', 'Blue', 'Purple'])
def test_standard_color_label_roundtrip_without_pick_conversion(label):
    mapping = parse_xmp(xmp(f'xmp:Label="{label}"'))
    assert mapping.label == label and mapping.pick is None and mapping.unsupported == ()
    assert parse_xmp(export_xmp(mapping)) == mapping


@pytest.mark.parametrize("orientation,expected", [
    (1, (.1, .2, .4, .6)), (2, (.6, .2, .9, .6)),
    (3, (.6, .4, .9, .8)), (4, (.1, .4, .4, .8)),
    (5, (.2, .1, .6, .4)), (6, (.2, .6, .6, .9)),
    (7, (.4, .6, .8, .9)), (8, (.4, .1, .8, .4)),
])
def test_inverse_exif_crop(orientation, expected):
    assert inverse_orientation_crop((.1, .2, .4, .6), orientation) == pytest.approx(expected)


def test_crop_mapping_and_arbitrary_rotation():
    attrs = 'tiff:Orientation="6" crs:CropLeft=".1" crs:CropTop=".2" crs:CropRight=".4" crs:CropBottom=".6"'
    assert parse_xmp(xmp(attrs)).crop == pytest.approx((.2, .6, .6, .9))
    zero = parse_xmp(xmp(attrs + ' crs:CropAngle="0"'))
    assert zero.crop == pytest.approx((.2, .6, .6, .9)) and zero.unsupported == ()
    rotated = parse_xmp(xmp(attrs + ' crs:CropAngle="5"'))
    assert rotated.crop is None and any(key.endswith('CropLeft') for key in rotated.unsupported)
    angle_only = parse_xmp(xmp('crs:CropAngle="0"'))
    assert angle_only.crop is None and any(key.endswith('CropAngle') for key in angle_only.unsupported)


def test_namespace_identity_and_duplicate_property():
    value = xmp('xmp:Rating="5"').replace(b'http://ns.adobe.com/xap/1.0/', b'https://foreign.invalid/')
    result = parse_xmp(value)
    assert result.rating is None and result.unsupported
    with pytest.raises(ValueError, match="duplicate"):
        parse_xmp(xmp('xmp:Rating="5"', '<xmp:Rating>4</xmp:Rating>'))


@pytest.mark.parametrize("raw", [
    b'<!DOCTYPE x [<!ENTITY a "boom">]><x>&a;</x>',
    b'<!DOCTYPE x SYSTEM "https://example.invalid/x"><x/>',
    b'<x>' * 33 + b'</x>' * 33,
    b'x' * ((1 << 20) + 1),
])
def test_forbidden_xml_and_budgets(raw):
    with pytest.raises(ValueError):
        parse_xmp(raw)


@pytest.mark.parametrize("raw", [
    b'<root><metadata/></root>',
    b'<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/>',
    b'<x:xmpmeta xmlns:x="adobe:ns:meta/"/>',
    b'<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Bag/></rdf:RDF>',
])
def test_unrelated_or_empty_xml_cannot_be_reported_as_supported_xmp(raw):
    with pytest.raises(ValueError):
        parse_xmp(raw)


def test_nested_description_cannot_inject_supported_properties():
    raw = xmp(children='<xmp:Label><rdf:Description xmp:Rating="5"/></xmp:Label>')
    with pytest.raises(ValueError, match="nested XMP RDF"):
        parse_xmp(raw)


def test_foreign_rdf_subject_cannot_apply_edits_to_current_image():
    for about in ('other.jpg', 'https://example.invalid/other.jpg'):
        with pytest.raises(ValueError, match="foreign XMP subject"):
            parse_xmp(xmp(f'rdf:about="{about}" xmp:Rating="5"'))
    assert parse_xmp(xmp('rdf:about="" xmp:Rating="5"')).rating == 5
    with pytest.raises(ValueError, match="unsupported XMP subject identity"):
        parse_xmp(xmp('rdf:nodeID="other" xmp:Rating="5"'))
    with pytest.raises(ValueError, match="XMP base URI forbidden"):
        parse_xmp(xmp('rdf:about="" xml:base="https://example.invalid/other/" xmp:Rating="5"'))
    with pytest.raises(ValueError, match="XMP base URI forbidden"):
        parse_xmp(xmp('xmp:Rating="5"').replace(b'<x:xmpmeta ', b'<x:xmpmeta xml:base="https://example.invalid/" '))


def test_rdf_container_refuses_non_property_text():
    for raw in (xmp('xmp:Rating="5"').replace(b'<rdf:RDF>', b'<rdf:RDF>injected'),
                xmp('xmp:Rating="5"', 'injected')):
        with pytest.raises(ValueError, match="XMP text outside property"):
            parse_xmp(raw)
    assert parse_xmp(xmp('xmp:Rating="5"', '\n  ')).rating == 5


def test_invalid_values_report_without_guessing():
    result = parse_xmp(xmp('xmp:Rating="NaN" tiff:Orientation="9" pe:Pick="yes" crs:CropLeft="0"'))
    assert result.rating is None and result.pick is None and result.orientation is None and result.crop is None
    assert len(result.unsupported) == 4

from photo_worker.v2.interchange_xmp import XMPMapping, export_xmp


@pytest.mark.parametrize("orientation", range(1, 9))
def test_export_import_roundtrip_all_orientations(orientation):
    value = XMPMapping(3, "rejected", orientation, (.1, .2, .4, .6), ())
    raw = export_xmp(value)
    assert raw == export_xmp(value)
    decoded = parse_xmp(raw)
    assert (decoded.rating, decoded.pick, decoded.orientation, decoded.unsupported) == (3, "rejected", orientation, ())
    assert decoded.crop == pytest.approx(value.crop)


@pytest.mark.parametrize("value", [
    XMPMapping(True, "picked", 1, None, ()),
    XMPMapping(6, "picked", 1, None, ()),
    XMPMapping(0, "foreign", 1, None, ()),
    XMPMapping(0, "picked", None, None, ()),
    XMPMapping(0, "picked", 1, (0, 0, float('nan'), 1), ()),
    XMPMapping(0, "picked", 1, None, ("unmapped-label",)),
    XMPMapping(0, "picked", 1, None, (), "Custom Blue"),
    XMPMapping(0, "picked", 1, None, (), ["Blue"]),
])
def test_export_refuses_invalid_or_unsupported_mapping(value):
    with pytest.raises(ValueError):
        export_xmp(value)


def test_absent_fields_are_not_invented_by_export():
    result = parse_xmp(export_xmp(XMPMapping(None, None, 1, None, ())))
    assert result == XMPMapping(None, None, 1, None, ())
