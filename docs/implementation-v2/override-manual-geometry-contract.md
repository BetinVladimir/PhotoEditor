# OverrideV1 manual geometry

`protected_polygons` is an ordered list of at most 128 `manual_mask.Polygon` documents. Each has `kind="polygon"`, `mode="protect"`, a unique integer `sequence` in `[0, 2^31-1]`, and 3–4096 normalized `{x,y}` points. The total point count is at most 10,000. Additional keys, non-finite or out-of-range coordinates, and duplicate sequences are invalid. This is metadata in OverrideV1; physical mask coverage and consent require separate qualified mask evidence.

`manual_landmarks` is either null or one complete `images.Face68`: nonzero `object_id`, exactly 68 normalized `{x,y,confidence}` points, a valid canvas whose `source_sha256` equals `OverrideV1.input_sha256`, and a canonical, continuous `transform_chain` ending at that canvas. It remains an authored correction, not evidence that a face detector ran or that a model was approved. The API admission and native worker contract apply the same shape; the import Override path repeats both checks against the target source before publication.

Any future manual geometry variant requires a new versioned contract and explicit migration. It must not be interpreted as arbitrary dictionary content by import, export, or render.
