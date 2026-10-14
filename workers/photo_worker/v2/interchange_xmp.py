"""Bounded namespace-exact XMP subset; no DTD/entities or external IO."""
from dataclasses import dataclass
import math
import re
from xml.parsers import expat

XMP = "http://ns.adobe.com/xap/1.0/"
CRS = "http://ns.adobe.com/camera-raw-settings/1.0/"
TIFF = "http://ns.adobe.com/tiff/1.0/"
PHOTO = "https://photoeditor.invalid/ns/1.0/"
RDF = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
XML = "http://www.w3.org/XML/1998/namespace"


@dataclass(frozen=True)
class XMPMapping:
    rating: int | None
    pick: str | None
    orientation: int | None
    crop: tuple[float, float, float, float] | None
    unsupported: tuple[str, ...]
    label: str | None = None


XMP_LABELS = frozenset(("Red", "Yellow", "Green", "Blue", "Purple"))


def _name(uri, local):
    return uri + "}" + local


def _fields(raw: bytes) -> dict[str, str]:
    if not isinstance(raw, bytes) or len(raw) > 1 << 20:
        raise ValueError("XMP byte budget exceeded")
    parser = expat.ParserCreate(namespace_separator="}")
    stack = []
    fields = {}
    count = 0
    descriptions = 0
    rdf_roots = 0

    def add(key, value):
        if key in fields:
            raise ValueError("duplicate XMP property")
        fields[key] = value.strip()

    def forbidden(*_args):
        raise ValueError("XMP DTD/entities forbidden")

    def start(name, attrs):
        nonlocal count, descriptions, rdf_roots
        count += 1
        if count > 30000 or len(stack) >= 32:
            raise ValueError("XMP structural budget exceeded")
        if _name(XML, "base") in attrs:
            raise ValueError("XMP base URI forbidden")
        parent = stack[-1][0] if stack else None
        if parent is None:
            if name not in (_name("adobe:ns:meta/", "xmpmeta"), _name(RDF, "RDF")):
                raise ValueError("invalid XMP root")
        elif len(stack) == 1 and stack[0][0] == _name("adobe:ns:meta/", "xmpmeta"):
            if name != _name(RDF, "RDF") or rdf_roots:
                raise ValueError("invalid XMP RDF container")
        elif parent == _name(RDF, "RDF"):
            if name != _name(RDF, "Description"):
                raise ValueError("invalid XMP RDF entry")
        elif name in (_name(RDF, "RDF"), _name(RDF, "Description")):
            raise ValueError("nested XMP RDF structure")
        if name == _name(RDF, "RDF"):
            rdf_roots += 1
        if name == _name(RDF, "Description") and parent == _name(RDF, "RDF"):
            # Only the sidecar's own image is in scope. A nonempty rdf:about
            # names another resource, so its edits cannot be applied here.
            if attrs.get(_name(RDF, "about"), "") != "":
                raise ValueError("foreign XMP subject")
            if any(key.startswith(RDF + "}") and key != _name(RDF, "about")
                   for key in attrs):
                raise ValueError("unsupported XMP subject identity")
            descriptions += 1
        if stack:
            stack[-1][2] = True
        stack.append([name, [], False, parent])
        for key, value in attrs.items():
            if name == _name(RDF, "Description") and parent == _name(RDF, "RDF") and key not in (_name(RDF, "about"), _name(XML, "lang")):
                add(key, value)

    def end(name):
        node = stack.pop()
        if node[3] == _name(RDF, "Description"):
            add(name, "" if node[2] else "".join(node[1]))

    def characters(text):
        if stack:
            if (stack[-1][0] in (_name("adobe:ns:meta/", "xmpmeta"),
                                  _name(RDF, "RDF"), _name(RDF, "Description"))
                    and text.strip()):
                raise ValueError("XMP text outside property")
            stack[-1][1].append(text)

    parser.StartElementHandler = start
    parser.EndElementHandler = end
    parser.CharacterDataHandler = characters
    parser.StartDoctypeDeclHandler = forbidden
    parser.EntityDeclHandler = forbidden
    parser.ExternalEntityRefHandler = forbidden
    parser.SkippedEntityHandler = forbidden
    try:
        parser.Parse(raw, True)
    except expat.ExpatError as exc:
        raise ValueError("invalid XMP XML") from exc
    if rdf_roots != 1 or descriptions < 1:
        raise ValueError("XMP description missing")
    return fields


def inverse_orientation_crop(crop, orientation):
    """Map oriented normalized rectangle corners into original canonical axes."""
    left, top, right, bottom = crop
    if orientation not in range(1, 9) or not all(math.isfinite(x) and 0 <= x <= 1 for x in crop) or left >= right or top >= bottom:
        raise ValueError("invalid oriented crop")
    transforms = {
        1: lambda x, y: (x, y), 2: lambda x, y: (1-x, y),
        3: lambda x, y: (1-x, 1-y), 4: lambda x, y: (x, 1-y),
        5: lambda x, y: (y, x), 6: lambda x, y: (y, 1-x),
        7: lambda x, y: (1-y, 1-x), 8: lambda x, y: (1-y, x),
    }
    points = [transforms[orientation](x, y) for x in (left, right) for y in (top, bottom)]
    return min(x for x, _ in points), min(y for _, y in points), max(x for x, _ in points), max(y for _, y in points)


def parse_xmp(raw: bytes) -> XMPMapping:
    fields = _fields(raw)
    rating_name, pick_name, orientation_name = _name(XMP, "Rating"), _name(PHOTO, "Pick"), _name(TIFF, "Orientation")
    label_name = _name(XMP, "Label")
    crop_names = [_name(CRS, key) for key in ("CropLeft", "CropTop", "CropRight", "CropBottom")]
    known = {rating_name, pick_name, orientation_name, label_name, *crop_names}
    unsupported = set(fields) - known
    rating = pick = crop = label = None
    orientation = 1
    if rating_name in fields:
        value = fields[rating_name]
        if re.fullmatch(r"-1|[0-5]", value):
            rating = int(value)
        else:
            unsupported.add(rating_name)
    if pick_name in fields:
        if fields[pick_name] in ("picked", "rejected", "unmarked"):
            pick = fields[pick_name]
        else:
            unsupported.add(pick_name)
    if label_name in fields:
        if fields[label_name] in XMP_LABELS:
            label = fields[label_name]
        else:
            unsupported.add(label_name)
    if orientation_name in fields:
        if re.fullmatch(r"[1-8]", fields[orientation_name]):
            orientation = int(fields[orientation_name])
        else:
            orientation = None
            unsupported.add(orientation_name)
    if any(key in fields for key in crop_names):
        try:
            if orientation is None or not all(key in fields for key in crop_names):
                raise ValueError("incomplete crop")
            angle = float(fields.get(_name(CRS, "CropAngle"), "0"))
            if not math.isfinite(angle) or angle != 0:
                raise ValueError("rotated crop unsupported")
            crop = inverse_orientation_crop(tuple(float(fields[key]) for key in crop_names), orientation)
            unsupported.discard(_name(CRS, "CropAngle"))
        except (ValueError, OverflowError):
            unsupported.update(key for key in crop_names if key in fields)
    return XMPMapping(rating, pick, orientation, crop, tuple(sorted(unsupported)), label)


def forward_orientation_crop(crop, orientation):
    if type(orientation) is not int or orientation not in range(1, 9):
        raise ValueError("invalid EXIF orientation")
    inverse = {1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 8, 7: 7, 8: 6}
    return inverse_orientation_crop(crop, inverse[orientation])


def export_xmp(mapping: XMPMapping) -> bytes:
    """Deterministic supported subset. Never silently drop unsupported mappings.

    crop is in canonical original axes; write oriented normalized coordinates.
    No WB/curves/RAW settings, foreign pick or unmapped labels are fabricated.
    """
    if not isinstance(mapping, XMPMapping) or mapping.unsupported:
        raise ValueError("unsupported XMP export mapping")
    if type(mapping.orientation) is not int or mapping.orientation not in range(1, 9):
        raise ValueError("invalid EXIF orientation")
    attrs = []
    if mapping.rating is not None:
        if type(mapping.rating) is not int or not -1 <= mapping.rating <= 5:
            raise ValueError("invalid XMP rating")
        attrs.append(("xmp:Rating", str(mapping.rating)))
    if mapping.pick is not None:
        if mapping.pick not in ("picked", "rejected", "unmarked"):
            raise ValueError("invalid XMP pick")
        attrs.append(("pe:Pick", mapping.pick))
    if mapping.label is not None:
        if type(mapping.label) is not str or mapping.label not in XMP_LABELS:
            raise ValueError("invalid XMP color label")
        attrs.append(("xmp:Label", mapping.label))
    attrs.append(("tiff:Orientation", str(mapping.orientation)))
    if mapping.crop is not None:
        crop = forward_orientation_crop(mapping.crop, mapping.orientation)
        attrs.extend(("crs:" + name, format(value, ".17g")) for name, value in
                     zip(("CropLeft", "CropTop", "CropRight", "CropBottom"), crop))
    # Every attribute value above is a validated enum, integer or finite float.
    attributes = " ".join(f'{name}="{value}"' for name, value in attrs)
    return (f'<?xml version="1.0" encoding="UTF-8"?>\n'
            f'<x:xmpmeta xmlns:x="adobe:ns:meta/" xmlns:rdf="{RDF}" '
            f'xmlns:xmp="{XMP}" xmlns:crs="{CRS}" xmlns:tiff="{TIFF}" xmlns:pe="{PHOTO}">'
            f'<rdf:RDF><rdf:Description rdf:about="" {attributes}/></rdf:RDF>'
            f'</x:xmpmeta>\n').encode("utf-8")
