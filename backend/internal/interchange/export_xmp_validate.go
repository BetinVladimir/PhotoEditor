package interchange

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"math"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
)

const exportXMPRDF = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"

// Validate the generated subset only. Returned orientation still needs to match
// independently qualified encoded image metadata before final adoption.
func validateExportXMP(ctx context.Context, raw []byte, entry importPlanEntry) (int, error) {
	if len(raw) < 1 || len(raw) > 1<<20 {
		return 0, v2input.ErrInvalid
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	depth, starts := 0, 0
	fields := map[xml.Name]string{}
	names := []xml.Name{{Space: "adobe:ns:meta/", Local: "xmpmeta"}, {Space: exportXMPRDF, Local: "RDF"}, {Space: exportXMPRDF, Local: "Description"}}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		switch node := token.(type) {
		case xml.StartElement:
			if starts >= 3 || depth != starts || node.Name != names[starts] {
				return 0, v2input.ErrInvalid
			}
			starts++
			depth++
			for _, attr := range node.Attr {
				if depth == 1 && (attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns") {
					continue
				}
				if depth != 3 {
					return 0, v2input.ErrInvalid
				}
				if _, ok := fields[attr.Name]; ok {
					return 0, v2input.ErrInvalid
				}
				fields[attr.Name] = attr.Value
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if strings.TrimSpace(string(node)) != "" {
				return 0, v2input.ErrInvalid
			}
		case xml.ProcInst:
			if node.Target != "xml" || starts != 0 {
				return 0, v2input.ErrInvalid
			}
		default:
			return 0, v2input.ErrInvalid
		}
	}
	if starts != 3 || depth != 0 {
		return 0, v2input.ErrInvalid
	}
	take := func(space, name string) (string, bool) {
		key := xml.Name{Space: space, Local: name}
		value, ok := fields[key]
		delete(fields, key)
		return value, ok
	}
	about, ok := take(exportXMPRDF, "about")
	if !ok || about != "" {
		return 0, v2input.ErrInvalid
	}
	rating, ok := take("http://ns.adobe.com/xap/1.0/", "Rating")
	if !ok || rating != strconv.Itoa(entry.Rating) {
		return 0, v2input.ErrInvalid
	}
	pick, ok := take("https://photoeditor.invalid/ns/1.0/", "Pick")
	if !ok || pick != entry.Pick {
		return 0, v2input.ErrInvalid
	}
	label, hasLabel := take("http://ns.adobe.com/xap/1.0/", "Label")
	if !validImportColorLabel(entry.Label) || (entry.Label == nil && hasLabel) || (entry.Label != nil && (!hasLabel || label != *entry.Label)) {
		return 0, v2input.ErrInvalid
	}
	orientationRaw, ok := take("http://ns.adobe.com/tiff/1.0/", "Orientation")
	if !ok || len(orientationRaw) != 1 || orientationRaw[0] < '1' || orientationRaw[0] > '8' {
		return 0, v2input.ErrInvalid
	}
	orientation := int(orientationRaw[0] - '0')
	crop := [4]float64{}
	present := 0
	for i, name := range []string{"CropLeft", "CropTop", "CropRight", "CropBottom"} {
		value, ok := take("http://ns.adobe.com/camera-raw-settings/1.0/", name)
		if ok {
			present++
			number, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 1 {
				return 0, v2input.ErrInvalid
			}
			crop[i] = number
		}
	}
	if len(fields) != 0 || (entry.Crop == nil && present != 0) || (entry.Crop != nil && present != 4) {
		return 0, v2input.ErrInvalid
	}
	if entry.Crop != nil {
		if crop[0] >= crop[2] || crop[1] >= crop[3] {
			return 0, v2input.ErrInvalid
		}
		minX, minY, maxX, maxY := 1., 1., 0., 0.
		for _, x := range []float64{crop[0], crop[2]} {
			for _, y := range []float64{crop[1], crop[3]} {
				a, b := x, y
				switch orientation {
				case 2:
					a = 1 - x
				case 3:
					a, b = 1-x, 1-y
				case 4:
					b = 1 - y
				case 5:
					a, b = y, x
				case 6:
					a, b = y, 1-x
				case 7:
					a, b = 1-y, 1-x
				case 8:
					a, b = 1-y, x
				}
				minX = math.Min(minX, a)
				minY = math.Min(minY, b)
				maxX = math.Max(maxX, a)
				maxY = math.Max(maxY, b)
			}
		}
		expected := [4]float64{entry.Crop.X0, entry.Crop.Y0, entry.Crop.X1, entry.Crop.Y1}
		for i, value := range [4]float64{minX, minY, maxX, maxY} {
			if math.Abs(value-expected[i]) > 1e-12 {
				return 0, v2input.ErrInvalid
			}
		}
	}
	return orientation, nil
}
