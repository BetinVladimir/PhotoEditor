package styleplan

import ("encoding/json";"errors")

// Stage is the durable, renderer-versioned order of one Apply graph.
// Cardinality is either item (one node per snapshot item), aggregate (one node
// over canonical outputs of the preceding item stage), or review (a persisted
// human decision barrier with no worker operation).
type Stage struct {
	Phase       string `json:"phase"`
	Operation   string `json:"operation"`
	OutputRole  string `json:"output_role"`
	Cardinality string `json:"cardinality"`
}

type regionRule struct { ID string `json:"region_id"`; Selector string `json:"selector"` }

type style struct {
 Culling struct { Enabled bool `json:"enabled"` } `json:"culling"`
	Mode  string `json:"mode"`
	Local struct {
		Enabled bool `json:"enabled"`
	} `json:"local"`
	Beauty struct {
		Enabled bool `json:"enabled"`
	} `json:"beauty"`
	Defects struct {
		Enabled bool   `json:"enabled"`
		Policy  string `json:"policy"`
	} `json:"defects"`
	Geometry struct {
		Enabled bool `json:"enabled"`
	} `json:"geometry"`
	Extensions struct {
        Culling struct { Enabled bool `json:"enabled"` } `json:"culling"`
		RegionAdjustments []regionRule `json:"region_adjustments"`
		Series *struct {
			Enabled bool `json:"enabled"`
		} `json:"series"`
		Composition *struct {
			Enabled bool `json:"enabled"`
		} `json:"composition"`
		PortraitDetail *struct {
			Enabled bool `json:"enabled"`
		} `json:"portrait_detail"`
		Restoration *struct {
			Denoise    string `json:"denoise"`
			Upscale    string `json:"upscale"`
			SensorDust string `json:"sensor_dust"`
		} `json:"restoration"`
		Background *struct {
			Mode string `json:"mode"`
		} `json:"background"`
		Inpainting *struct {
			Enabled bool `json:"enabled"`
		} `json:"inpainting"`
		Generation *struct {
			Enabled bool `json:"enabled"`
		} `json:"generation"`
	} `json:"extensions"`
}

func itemStage(phase, operation, role string) Stage {
	return Stage{phase, operation, role, "item"}
}

func Compile(raw json.RawMessage, itemCount int) ([]Stage, error) {
	var s style
	if itemCount < 1 || itemCount > 5000 || json.Unmarshal(raw, &s) != nil || s.Mode != "fixed" && s.Mode != "adaptive" {
		return nil, errors.New("invalid apply v4 style")
	}
	s.Culling.Enabled = s.Culling.Enabled || s.Extensions.Culling.Enabled
	series := s.Extensions.Series != nil && s.Extensions.Series.Enabled && itemCount >= 2
	if s.Extensions.Restoration != nil && s.Extensions.Restoration.SensorDust != "" && s.Extensions.Restoration.SensorDust != "off" && s.Extensions.Restoration.SensorDust != "review" && s.Extensions.Restoration.SensorDust != "conservative_auto" {
		return nil, errors.New("invalid sensor dust policy")
	}
	backgroundNeedsPerson := s.Extensions.Background != nil && s.Extensions.Background.Mode != "" && s.Extensions.Background.Mode != "keep"
	needsRegionPerson := false
	for _, rule := range s.Extensions.RegionAdjustments { needsRegionPerson = needsRegionPerson || rule.Selector == "person" || rule.Selector == "background" }
	autoDust := s.Extensions.Restoration != nil && s.Extensions.Restoration.SensorDust == "conservative_auto"
	needsFace := s.Beauty.Enabled || s.Defects.Enabled
	needsPerson := s.Culling.Enabled || autoDust || needsRegionPerson || s.Mode == "adaptive" || s.Local.Enabled || series || s.Geometry.Enabled || s.Beauty.Enabled || s.Defects.Enabled || backgroundNeedsPerson || s.Extensions.Composition != nil && s.Extensions.Composition.Enabled
	stages := make([]Stage, 0, 32)
	if needsPerson {
		stages = append(stages, itemStage("mask", "mask.person", "person_mask_ref"))
	}
	if s.Mode == "adaptive" || s.Culling.Enabled {
		stages = append(stages, itemStage("stats", "color.stats", "stats"))
	}
	stages = append(stages, itemStage("resolve", "style.resolve", "resolved_style"))
 if s.Culling.Enabled { stages=append(stages,itemStage("cull_embedding","features.scene","scene_embedding"),itemStage("cull_features","collection.features","scene_features"),Stage{"cull_pack","collection.features_pack","scene_feature_pack","group"},Stage{"cull_group","collection.group","scene_groups","aggregate"},Stage{"cull_rank","collection.cull","cull_recommendations","aggregate"},Stage{"cull_review","","","review"}) }
	if s.Extensions.Restoration != nil && s.Extensions.Restoration.Denoise != "" && s.Extensions.Restoration.Denoise != "off" {
		stages = append(stages, itemStage("denoise", "image.denoise", "working_image"))
	}
	if s.Geometry.Enabled {
		stages = append(stages, itemStage("pose", "pose.detect", "pose_set"), itemStage("geometry", "geometry.body", "working_image"), itemStage("mask_transform", "mask.transform", "mask_transform_result"))
	}
	stages = append(stages, itemStage("color", "image.color", "working_image"))
	if series {
		stages = append(stages, itemStage("series_measure", "series.measure", "series_stats"), Stage{"series_pack","series.pack","series_stats_pack","group"}, Stage{"series_balance", "series.balance", "series_corrections", "aggregate"})
	}
	if s.Extensions.Restoration != nil && (s.Extensions.Restoration.SensorDust == "review" || autoDust) {
        stages = append(stages, Stage{"dust_detect", "sensor.dust", "dust_candidates", "group"})
        if autoDust { stages=append(stages,itemStage("dust_repair","sensor.dust_auto_repair","working_image")) } else { stages=append(stages,Stage{"dust_review","","","review"},itemStage("dust_repair","sensor.dust_repair","working_image")) }
    }
	stages = append(stages, itemStage("local", "image.local", "working_image"))
	if needsFace {
		stages = append(stages, itemStage("face_detect", "face.detect", "face_boxes"), itemStage("face_landmarks", "face.landmarks", "face_landmarks"), itemStage("face_parse", "face.parse", "face_labels_ref"), itemStage("face_skin", "mask.face_skin", "skin_mask_ref"))
	}
	if s.Beauty.Enabled {
		stages = append(stages, itemStage("beauty", "portrait.beauty", "working_image"))
	}
	if s.Extensions.PortraitDetail != nil && s.Extensions.PortraitDetail.Enabled {
		stages = append(stages, itemStage("portrait_detail", "portrait.detail", "working_image"))
	}
	if s.Defects.Enabled {
		stages = append(stages, itemStage("defects_propose", "defects.propose", "defect_candidates"))
		if s.Defects.Policy == "review" {
			stages = append(stages, Stage{"defects_review", "", "", "review"})
		}
		if s.Defects.Policy == "conservative_auto" { stages=append(stages,itemStage("defects_auto_repair","defects.auto_repair","working_image")) }
		if s.Defects.Policy != "review" && s.Defects.Policy != "conservative_auto" {
			return nil, errors.New("invalid defects policy")
		}
	}
	if b := s.Extensions.Background; b != nil && b.Mode != "" && b.Mode != "keep" {
		op := map[string]string{"transparent": "background.compose", "solid": "background.compose", "asset": "background.compose", "blur": "background.blur", "generate": "image.generate"}[b.Mode]
		if op == "" {
			return nil, errors.New("invalid background mode")
		}
		if b.Mode == "blur" {
			stages = append(stages, itemStage("depth", "depth.predict", "relative_depth_ref"))
		}
		if b.Mode == "asset" { stages = append(stages, itemStage("background_inspect", "asset.inspect", "metadata"), itemStage("background_decode", "asset.decode", "working_image")) }
		stages = append(stages, itemStage("background", op, "working_image"))
		if b.Mode == "generate" {
			stages = append(stages, Stage{"background_review", "", "", "review"})
		}
	}
	if s.Extensions.Inpainting != nil && s.Extensions.Inpainting.Enabled {
		stages = append(stages, itemStage("inpaint", "image.inpaint", "working_image"), Stage{"inpaint_review", "", "", "review"})
	}
	if s.Extensions.Generation != nil && s.Extensions.Generation.Enabled {
		stages = append(stages, itemStage("generate", "image.generate", "working_image"), Stage{"generation_review", "", "", "review"})
	}
	if s.Extensions.Composition != nil && s.Extensions.Composition.Enabled {
		stages = append(stages, itemStage("composition_structure", "composition.structure", "image_structure"), itemStage("composition", "composition.plan", "composition_plan"), itemStage("composition_render", "geometry.crop", "working_image"), Stage{"composition_review", "", "", "review"})
	}
	if r := s.Extensions.Restoration; r != nil && r.Upscale != "" && r.Upscale != "off" {
		stages = append(stages, itemStage("upscale", "image.upscale", "working_image"))
	}
	stages = append(stages, itemStage("quality", "quality.check", "quality_report"))
	return stages, nil
}
