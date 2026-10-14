# Параметры операций v2.2

Нормативные типы и границы — [schemas.json](contracts/schemas.json), роли и лимиты — [operations.json](contracts/operations.json). Artifact closure и lifecycle — [15](15-execution-contracts.md); формулы image processing — [16](16-imaging-algorithms.md); wire protocol — [19](19-runtime-protocol.md). Все поля required; nullable явно задаётся схемой. Таблица генерируется build-indexes.py.

| Операция | Params schema | Поля кроме schema_version | Output | Пакет |
|---|---|---|---|---|
| layer.compose | LayerComposeParams | base, source_result, proposal_result, mask, protected_mask, opacity, blend | ImageResult | V25 |
| session.describe | SessionDescribeParams | snapshot, ordinals, model_lock_sha, calibration, consents | DescriptorShard | V22 |
| session.plan | SessionPlanParams | shards, policy, context | ScenePlan | V22 |
| learning.fit | LearningFitParams | manifest, base_profile, policy | CandidateProfile | V23 |
| learning.evaluate | LearningEvaluateParams | candidate, manifest, baseline, calibration, model_lock_sha, icc_lock_sha, renderer_version | Evaluation | V23 |
| capture.assess | CaptureAssessParams | binding, proxy, faces, policy, model_lock_sha, context | QAReport | V24 |
| identity.compare | IdentityCompareParams | before, after, effective_mask, protected, model_lock_sha, calibration, consents | IdentityReport | V23 |
| semantic.transfer | SemanticTransferParams | intent, reference, target, model_lock_sha, calibration | TransferCandidate | V27 |
| portrait.open_eyes | PortraitOpen_EyesParams | base, strength, donor, source_face_id, donor_face_id, eyes_mask, protected, confirmation, source_faces, donor_faces | ImageResult | V28 |
| portrait.glare | PortraitGlareParams | base, strength, donor, glasses_mask, protected, mode, confirmation, model_lock_sha, calibration, source_faces, donor_faces | ImageResult | V28 |
| image.reflection | ImageReflectionParams | base, strength, mask, protected, model_lock_sha, calibration | ImageResult | V28 |
| portrait.hair | PortraitHairParams | base, strength, mask, hue_delta_degrees, saturation_factor, detail, protected | ImageResult | V28 |
| portrait.clothing | PortraitClothingParams | base, strength, mask, protected, hue_delta_degrees, saturation_factor, wrinkle_strength | ImageResult | V28 |
| image.relight | ImageRelightParams | base, strength, depth, subject, protected, lights, calibration | ImageResult | V28 |
| optical.lens | OpticalLensParams | base, strength, profile, focal_mm, aperture | ImageResult | V25 |
| optical.perspective | OpticalPerspectiveParams | base, strength, mode, corners, lines, max_crop_fraction | ImageResult | V29 |
| optical.negative | OpticalNegativeParams | base, strength, film_base_rgb, calibration, exposure_ev | ImageResult | V29 |
| optical.panorama | OpticalPanoramaParams | frames, reference_ordinal, projection, max_pixels, calibration | ImageResult | V29 |
| optical.focus_stack | OpticalFocus_StackParams | frames, reference_ordinal, motion_policy, max_pixels, calibration | ImageResult | V29 |
| provenance.sign | ProvenanceSignParams | export, lineage, signing_policy_id, policy | SignedResult | V31 |
| v2.delivery.assemble | V2DeliveryAssembleParams | assembly | DeliveryManifest | V30 |
| v2.interchange.import | V2InterchangeImportParams | parsed, plan, max_report_bytes | InterchangeResult | V30 |
| v2.interchange.export | V2InterchangeExportParams | plan, max_report_bytes | InterchangeResult | V30 |
| v2.semantic.assemble | V2SemanticAssembleParams | intent, candidates, model_lock_sha, calibration | SemanticPlan | V27 |
| v2.interchange.parse | V2InterchangeParseParams | request, max_expanded_bytes, max_files | ParsedInterchange | V30 |
| v2.delivery.prepare | V2DeliveryPrepareParams | input | DeliveryPreparation | V30 |
| region.preview | RegionPreviewParams | tenant_id, project_id, asset_id, request, source, recipe, frozen_recipe, geometry, render_order, inputs | RegionDerivativeManifest | V26 |
| render.compose | FrozenRenderRecipe | recipe, recipe_sha256, scalars, learning, background_asset | ImageResult | V26 |
| semantic.local_exposure | ApprovedSemanticEdit | target_id, source, intent, candidate, mask, transform, kind, strength, ev, prompt, preserve_persons | ImageResult | V27 |

DTO добавлять в existing contract generator. Go strict decoding, TS generated types, Pydantic forbid extras; semantic validation15–19 обязательна на сервере и перед исполнением worker. Cross-language tests: unknown fields, null, finite numeric bounds, duplicate IDs, order, byte caps и revision overflow.
