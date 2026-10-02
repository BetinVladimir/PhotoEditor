package exportpreset

import "testing"

func validCreate() Create {
	long := 2400
	quality := 90
	return Create{Name: "Web", Config: Config{Format: "jpeg", ICCID: "srgb_v1", Size: &Size{LongEdge: &long, Fit: "contain"}, Quality: &quality, Subsampling: "444", Alpha: "matte", MatteRGB: []float64{1, 1, 1}, MetadataPolicy: "strip", NamingTemplate: "{name}-{version}"}}
}
func TestValidatePreset(t *testing.T) {
	if err := validate(validCreate()); err != nil {
		t.Fatalf("valid preset: %v", err)
	}
	cases := []func(*Create){
		func(v *Create) { v.Config.Format = "webp" },
		func(v *Create) { v.Config.Alpha = "preserve" },
		func(v *Create) { q := 101; v.Config.Quality = &q },
		func(v *Create) { v.Config.MatteRGB = []float64{1.1, 0, 0} },
		func(v *Create) { v.Config.MetadataPolicy = "safe_camera" },
		func(v *Create) { v.Config.Sharpen = &Sharpen{Radius: 5.1, Strength: .5} },
		func(v *Create) { v.Config.Size = &Size{Fit: "contain"} },
		func(v *Create) { width := 100; v.Config.Size = &Size{Width: &width, Fit: "contain"} },
		func(v *Create) { v.Config.Quality = nil },
	}
	for i, mutate := range cases {
		v := validCreate()
		mutate(&v)
		if validate(v) == nil {
			t.Fatalf("invalid case %d accepted", i)
		}
	}
}
