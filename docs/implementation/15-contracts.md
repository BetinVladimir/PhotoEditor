# 15. Типы, wire formats и правила генерации

## Примитивы

ID=UUID string lower canonical, Hash=lower hex64, Revision/Cursor/Bytes=decimal string в JSON API и int64 в Go/DB (не JSON number для потенциально больших значений); scalar image params IEEE754 finite. API version1, JSON task schema_version integer1, StyleV2 version string2.0.0. JSON dates RFC3339 UTC. Tuple shapes фиксированы, неизвестные поля запрещены. `ArtifactRef={artifact_id:ID,sha256:Hash}`; ref не содержит пользовательский путь или URL. Внутренний verified artifact metadata добавляет bucket,key,object_version,size,mime,dimensions/ICC только после DB lookup.

`Principal` discriminated union UserPrincipal{user_id,session_id} / GuestPrincipal{session_id,share_id,tenant_id,collection_id,policy_version} / ServicePrincipal{service_id,worker_id,scopes}. Клиент не присылает готовый Principal. `TenantTx` только серверный capability wrapper. `Lease={tenant_id,task_id,run_id,attempt>=1,fencing_token>=1,lease_until,worker_id}`; ни unsigned lease body, ни task UUID сами по себе не авторизуют worker.

`Canvas={width>=1,height>=1,orientation:1,space_id,source_sha256}`. `Rect={x,y,width,height}` integer pixels current canvas, within bounds. `NormalizedRect={x0,y0,x1,y1}`0..1, x1>x0,y1>y0. `TransformStep={kind:matrix|dense_warp|crop|resize,from_canvas,to_canvas,forward_matrix?:float[9],inverse_field?:ArtifactRef,invertibility,valid_region}`; ровно соответствующий payload. `TransformChain={steps:TransformStep[],hash}`.

`WorkingImage={pixels:ArtifactRef,canvas:Canvas,color:{profile_id,icc_sha256,encoding:gamma24|linear},dtype:uint16|float32,alpha:ArtifactRef|null,transform_chain:TransformChain}`. Masks PNG16 or float32 map, `MaskRef={artifact,canvas,transform_chain,object_id?,selector,confidence,model_lock_id,protected:boolean}`. Soft values0..1; PNG scales65535. RelativeDepth={artifact,float32_shape,invalid_mask,canvas,transform_chain,normalization:minmax_valid}; nonmetric.

`ImageMetadata={source_sha256,width,height,channels,bit_depth,format,orientation,icc_sha256,assumed_profile_id?,camera_id?,captured_at?,exposure_seconds?,warnings[]}`. `FaceBox={object_id,bbox_normalized,confidence}`; Face68={object_id,points:[{x,y,confidence};68],canvas,transform_chain}; Pose17 analogous. Coordinates reflect current transform but stored source-normalized override coordinates explicit. Embedding384={artifact,dims:384,dtype:float32,l2_norm,model_lock,preprocessing_id}; Stats20={feature_version:stats20_v1,values:float[20],coverage_bins:bool[512],mask_confidence,warnings[]}.

## Task/result и errors

Command exact fields: schema_version,event_id,tenant_id,run_id,task_id,attempt,operation,operation_version,resource_class,correlation_id,inputs:ArtifactRef[],params:ArtifactRef,model:null|{id,lock_id},timeout_seconds,traceparent. DB context содержит additional typed input manifest; список inputs упорядочен и роли определены InputT. Worker сверяет all IDs/hashes/operation/resource against task row. Byte payload≤64KiB, image/style не в Rabbit.

TaskResultEvent={schema_version:1,event_id,type:task.succeeded|task.failed|task.cancelled|task.progress,tenant_id,run_id,task_id,attempt,fencing_token,outputs:ArtifactRef[],metrics:TaskMetrics,error:null|TaskError}. TaskMetrics={duration_ms,peak_memory_bytes,peak_device_bytes?,done?,total?,bytes_read?,bytes_written?}; values finite nonnegative. TaskError={code,retryable,message_key,args,diagnostic_artifact_id?}. Progress uses same envelope but outputs=[],error=null; terminal output canonical DB still authority. `needs_review` lives in result metadata/output manifest, not extra task state.

Codes: CONFIG_INVALID,CONFIG_INCOMPATIBLE,INPUT_INVALID,INPUT_PROFILE_REQUIRED,RAW_UNSUPPORTED,MODEL_UNAVAILABLE,MODEL_HASH_MISMATCH,ARTIFACT_MISSING,ARTIFACT_HASH_MISMATCH,QUOTA_EXCEEDED,DISK_QUOTA,OUTPUT_PIXELS_LIMIT,REVISION_CONFLICT,SNAPSHOT_STALE,IDEMPOTENCY_CONFLICT,OVERRIDE_SOURCE_MISMATCH,PAIR_ALIGNMENT_REQUIRED,PAIR_UNSUPPORTED,EMPTY_VALID_REGION,NO_VALID_PAIRS,LOW_CONFIDENCE,OOD_FALLBACK,UNSAFE_GEOMETRY,REVIEW_REQUIRED,LINK_UNAVAILABLE,PERMISSION_DENIED,LEASE_LOST,TASK_CANCELLED,DEPENDENCY_UNAVAILABLE,RESOURCE_OOM,TIMEOUT,INTERNAL_ERROR. retryable only dependency/transient timeout/OOM within limits; config/hash/security invalid never auto retry. Review reasons are codes, not exception messages.

## RunSnapshot / ImageState / Overrides

`RunSnapshotV1` required schema_version1,pipeline analyze/apply/cull/preview/export/ingest/download/bulk/maintenance,pipeline_version3,renderer_version,source_collection_id?,source_revision?,input_snapshot_id,style_version_id?,style_hash?,ordered_operations:[{operation,version}],resource_profile,locale,review_policy{generation:always,low_confidence:pause},overrides_version?,seed:uint32,model_lock_id,profile_lock_id,export_options:ExportPreset[],failure_policy:continue_items,scope:all|selected|all_change_selected. No paths. `input_snapshot_digest` included hash root; config contains all resolved defaults, not dynamic environment sliders.

`ImageStateV1={schema_version:1,input_sha256,source_versions:ID[],canvas,transform_chain,object_ids,prototype_weights:[{id,weight}],ood:{distance,threshold,uncovered_fraction,fallback},modules:[{operation,version,requested,applied,evidence,skip_reason?}],review_decisions:[{review_id,candidate_hash,decision}],backend,execution_profile,output_refs,quality}`. Evidence={state:fitted|inferred|user_set|unavailable,confidence0..1,supporting_pair_ids:ID[]}; no learned provenance on hardcoded defaults.

`OverrideV1={schema_version:1,input_sha256,object_id?,modules,accepted_candidate_ids,rejected_candidate_ids,protected_polygons,manual_landmarks?,mask_refs,revision}`; `mask_refs[]={binding_id,purpose,mask:MaskRef,confirmed,strength}` uses the closed purpose set `portrait_teeth|portrait_sclera|portrait_shine|portrait_under_eye|portrait_cloth|portrait_glasses|inpaint_target|generation_target|protected`. Purpose fixes the allowed selector/protection role; cloth, glasses, inpaint and generation require explicit confirmation; binding IDs are unique. The API accepts only the pixel artifact of a canonical succeeded `mask.rasterize|mask.prompt` interactive task whose source artifact is the same asset version, and checks exact SHA/output role/result provenance. Modules whitelist only semantic editable Params, cannot change model source/renderer/task limits/tenant. Input sha must match normalized source identity recorded run, not hash arbitrary preview. Explicit job overrides >style; per-image override>job; resolved requested/applied all preserved.

## Operation IO composition

Все входные типы registry определяются в `workers/photo_worker/contracts/operations.py`. Ниже замкнутые определения; shorthand `+` означает named fields, не positional untyped tuple. Все данные массивов на диске через ArtifactRef; малые structured metadata допускаются inline внутри JSON params artifact.

| Типы registry | Структура |
|---|---|
| StagingRef / SourceRef | ArtifactRef+upload_session_id/expected_bytes либо asset_version_id+source_sha+purpose |
| VerifiedAsset | source ArtifactRef+ImageMetadata+verification_state+warnings |
| SRGBPreview | pixels ArtifactRef+Canvas+profile sRGB+preprocessing/transform |
| PairImages | pair_id,before/after WorkingImage,ignore_mask?,session_id |
| PairAlignment | pair_id,matrix3x3,valid_mask,overlap,bg_error,confidence,mode |
| ImageAndMask | WorkingImage,person MaskRef?,valid MaskRef? |
| ImageFaceBoxes / ImageFaceMasks | image,faces FaceBox[] / image,landmarks Face68[],masks MaskRef[] |
| FaceBoxes / FaceLabels19 / SceneLabels / ProbabilityMask / AlphaMask / TransformedMask | detected list / label artifact+class map+Canvas / MaskRef corresponding selector |
| ImagePrompts / ManualMask | image ref,source_sha,prompt vectors / source canvas,stroke/polygon vector artifact |
| MaskAndTransform | MaskRef,TransformChain,target_canvas |
| ImageAlpha / ImageAlphaBackground / ImageAlphaDepth | image,alpha / plus background ArtifactRef or solid / plus RelativeDepth |
| RegisteredPairFeatures | PairImages,PairAlignment,face/pose/person before/after refs |
| BodyFit | pair_id,object fits,coefficients,confidence,requested/applied,metrics/evidence |
| BodyParams | image, poses, person, exactly one of pair-bound fit or portable style coefficients, optional faces, strength and safety bounds |
| AlignedPairPixels | pair_id,aligned before/after,valid/ignore/person masks,split ref,session_id |
| ColorFit | pair_id,GlobalColor,LocalColor,stats20,coverage,fit_metrics,holdout_metrics,loss_history artifact,compiled_lut?,evidence |
| ColorAlignedPair / ResidualPair | PairImages+color reconstructed before+valid masks+object refs / plus beauty reconstruction |
| BeautyFit / DefectEvidence | pair_id,params,confidence,evidence,diagnostic refs,metrics |
| PairFits | ordered pair-fit refs +pair snapshot,session split,digest |
| StyleCandidate / StyleAndReport / StyleAndHoldout | full StyleV2 ref+report / candidate+QualityReport / candidate+heldout snapshot |
| StylePackage | style ArtifactRef,config_hash,assets_manifest?,report/contact_sheet refs |
| CollectionFeatures / GroupedFeatures | ordered snapshot +per-item features refs / plus SceneGroups |
| SceneGroups / CullRecommendations | groups[{id,item_ids,reference?}],burst/duplicate groups / picks,rank reasons,shortage,review_required |
| StyleAndStats / ImageResolvedStyle | style ref+Stats20+mask confidence / image+ResolvedStyle |
| ResolvedStyle | mode,GlobalColor|AbsoluteLUT,LocalColor,prototype_weights,OOD,effective module params,hash |
| ColoredImageMasks / SeriesStatsCollection | global-colored image,comparable masks / ordered SeriesStats refs |
| SeriesStats / SeriesCorrections | item/region statistics/confidence / scene/reference+item raw/applied EV/gains/reasons |
| ImageBodyFeatures / ImageAndWarp | image,pose/person/face/masks / image,field ref,transform chain,quality |
| ImageMasksCorrections | image,aligned base,mask refs,ResolvedStyle,SeriesCorrections?,region adjustments |
| DefectCandidates / DustCandidates | source hash,candidates[{id,mask,bbox,score,decision:pending}],label artifact,confidence |
| ImageAcceptedMasks | image,candidate ref,review decision id/hash,protected masks |
| SensorAlignedSeries | ordered image refs,camera metadata,sensor transform refs |
| ImageStructure / CompositionPlan / ImageComposition | image+lines/landmarks/person / crop rect,homography,protected checks,review / image+plan |
| ImageConfirmedMask / ImageExtendedCanvas | image,mask,protected,decision ref / plus original rect,extended Canvas |
| ImageCandidate / GenerationCandidate | image,base ref,changed mask,review reason / plus generation metadata prompt/seed/model/dtype/backend |
| ImageSkyMaskAsset | image,sky MaskRef,sky asset ref,protection |
| BracketImages / BracketTransforms / RegisteredBracket | groupID,input versions/images,times,reference / matrices+motion mask+confidence / images+transforms |
| ImageDarkExposureMask | merged image,dark image,alignment,window mask,exposure ratio |
| ImageAndBaseline / QualityReport | image,baseline?,threshold policy,protected masks / stage,technical status,metrics nullable,review reasons,diagnostic refs |
| VersionMaster / VersionOrCandidate / DerivativeManifest | version ID+master WorkingImage / version or candidate discriminated / variant refs+profile/source hash/dimensions/status |
| ImageWatermarkAsset / EncodedImage | image,approved watermark asset / artifact,format,dimensions,ICC,alpha,metadata_policy,sha |
| DownloadSnapshot / ArchiveManifest | snapshot ref+ordered allowed exports/volume / artifact,entries manifest,checksum,bytes,count,volume |
| BulkChunk / BulkChunkResult | bulk_id,ordinals,expected_revision,action params / item outcomes,new_revision,counts |
| CleanupCandidates / CleanupReport | artifact IDs+cutoff/lease evidence / deleted/skipped/failed IDs+reasons |
| PreviewList / ContactSheet | refs+labels+locale / paginated image/pdf-free image refs,manifest |

Input/output JSON schemas generated from these named Pydantic classes; constraints include allowed missing baseline, empty masks and dimensions. `none` model registry means algorithm, not отсутствующая реализация.

## Style defaults и cross-field validation

Global ranges и LUT 07; beauty/defects/geometry08. Active local requires person_ev/background_ev[-1,1],person_saturation/background_saturation[.7,1.3],mask_policy_id=person_guided_v1; disabled only enabled false. Beauty algorithm beautyfilter_v1,smoothing/tone_evenness/texture_keep[0,1],skin_mask face68_conservative_v1,protect_features=true. Defects algorithm defectrepair_telea_v1,policy review/conservative_auto,strength[0,1],diameter tuple .002/.012 bounds,preserve_permanent_marks=true. Geometry algorithm bodywarp_v1,body_strength,face_slim,waist_slim,hip_slim,torso_slim,leg_length,protect_background=true,max_displacement_image_fraction<=.02,on_unsafe=skip_module.

Extensions culling/series/composition/portrait_detail/inpainting/generation use enabled union. Masking provider IDs; restoration denoise off/scunet_color_real_psnr,denoise_strength,upscale off/2x/4x,sensor_dust off/review/conservative_auto. Background keep/transparent/solid/asset/blur/generate,asset_id nullable,solid_rgb?,blur_strength,protect_subject=true. Region_adjustments explicit array fields selector/region_id/EV±1/sat.7..1.3/hue±15/strength/priority. Optional export policy lives job, не вводить embedded tenant-specific MinIO URLs в style. HDR bracket groups live job, not learned style.

Cross checks: adaptive requires adaptation+LR-ASPP,features20,k<=4,temperature1,scale>=.05,coverage512,prototypes max32@33 or8@65,all same profile/size. Fixed adaptation=null. Every enabled model effect listed models exact weight IDs and installed lock; disabled effect no weights required. Target count/fraction mutually exclusive. Background asset mode requires packaged asset. Alpha JPEG requires explicit matte. Geometry strength0 bypass. Semver unknown major rejected; unsupported renderer rejected even if schema valid.

Job operational extension `brackets:[{group_id,asset_version_ids[3..9],exposure_seconds[3..9],reference_index,mode,window_mask?}]` lengths equal, IDs disjoint among brackets, explicit order, scale positive. `scene_edits`,`manual_picks`,`group_fixed` job values not style learning.

## API response variants

`RunStatus={id,kind,status,revision,counts{total,succeeded,failed,skipped,review},output_collection_id?,stage,progress,errors[],next_cursor?}`; `BulkStatus` adds per-item outcomes pagination. `DownloadStatus={id,status,files_done,files_total,bytes_written,volumes[{number,status,bytes,delivery_kind}],expires_at?,error?}` no signed URLs until intent. `GuestCollection={name,description,revision,cover?,capabilities{view,download,upload},allowed_presets,sections,empty}`; download-only sections=[]/cover=null. `GuestItem={id,width,height,title,section_id,thumbnail_status,media_variants}` excludes camera/GPS/internal reviews. `MediaLinks={items[{item_id,variant,url,expires_at,width,height,media_type}]}`.

`PublicConfig={locales:[ru,bg,en],themes:[light,dark,system],upload_policy,registration_enabled,api_version}`. `Capabilities` operation names plus available/reason/backend profile, no system secrets. `ArtifactDownload={url,expires_at,filename,bytes,sha256}`. `DownloadIntent={intent_token,expires_at}` token only current caller. `TokenPair={access_token,expires_in,user,session_id,csrf_token,refresh_token?}` refresh_token only native, web cookie. Status DTO никогда не содержит source path, signing key или task credential.

## Уточнение геометрических wire-контрактов

`Canvas.space_id` — идентификатор координатного пространства, не ICC/profile ID. Цветовой профиль задаётся только `WorkingImage.color`. Изменение color encoding само по себе не меняет геометрию. `valid_region` задаётся в пикселях `to_canvas`; canvas/source_sha256 сохраняет исходную идентичность в пределах цепочки.

TransformStep — строгий discriminated union: matrix содержит row-major forward_matrix[9]; dense_warp — inverse_field ArtifactRef; crop — crop_rect в from_canvas (размеры равны to_canvas); resize — pixel_centers=half_pixel с коэффициентами из размеров canvas. Лишние payload fields запрещены. `invertibility` принимает invertible/partial/noninvertible и относится к геометрической области, не гарантирует обратимость квантования пикселей. Матрица проверяется на вырожденность и пересечение projective horizon исходного canvas. `TransformChain.hash=SHA256(JCS({steps:[...]}))`; соседние canvases должны точно совпадать.

Рабочие ColorSpace сейчас поддерживают work_rec2020_gamma24_v1/gamma24 и linear_srgb_v1/linear; sRGB display preview имеет отдельный IO тип, не объявляется linear WorkingImage. Контракты находятся в `workers/photo_worker/contracts/images.py`; декодирование содержимого pixel/mask artifact дополнительно проверяет фактические shape/dtype/ICC.
