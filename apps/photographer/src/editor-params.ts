export type DisabledModule={enabled:false};
export type BeautyModule={enabled:true;algorithm:"beautyfilter_v1";smoothing:number;tone_evenness:number;texture_keep:number;skin_mask:"face68_conservative_v1";protect_features:true};
export type DefectsModule={enabled:true;algorithm:"defectrepair_telea_v1";policy:"review"|"conservative_auto";strength:number;diameter_face_fraction:[number,number];preserve_permanent_marks:true};
export type GeometryModule={enabled:true;algorithm:"bodywarp_v1";body_strength:number;face_slim:number;waist_slim:number;hip_slim:number;torso_slim:number;leg_length:number;protect_background:true;max_displacement_image_fraction:number;on_unsafe:"skip_module"};
export type RestorationExtension={denoise:"off"|"scunet_color_real_psnr";denoise_strength:number;upscale:"off"|"2x"|"4x";sensor_dust:"off"|"review"|"conservative_auto"};
export type CompositionExtension={enabled:true;aspect:[number,number];headroom:number;rotate_limit_deg:number;outpaint_policy:"off"|"review";outpaint_prompt:string;outpaint_negative_prompt:string;outpaint_seed:number;outpaint_guidance_scale:number};
export type BackgroundExtension={mode:"keep"|"transparent"|"solid"|"blur";asset_id:null;solid_rgb:[number,number,number]|null;blur_strength:number;protect_subject:true};
export type EditorExtensions=Record<string,unknown>&{restoration?:RestorationExtension;composition?:DisabledModule|CompositionExtension;background?:BackgroundExtension};
export type EditorModules=Record<string,unknown>&{strength?:number;beauty?:DisabledModule|BeautyModule;defects?:DisabledModule|DefectsModule;geometry?:DisabledModule|GeometryModule;extensions?:EditorExtensions};
export type EditorParams={schema_version:1;modules:EditorModules};

function record(value:unknown):value is Record<string,unknown>{return typeof value==="object"&&value!==null&&!Array.isArray(value)}
export function parseEditorParams(value:Record<string,unknown>):EditorParams{
 if(value.schema_version!==1||!record(value.modules))throw new Error("EDITOR_PARAMS_INVALID");
 return value as EditorParams;
}
export function setEditorModule<K extends keyof EditorModules>(params:EditorParams,name:K,value:EditorModules[K]):EditorParams{return{...params,modules:{...params.modules,[name]:value}}}
export function setEditorExtension<K extends keyof EditorExtensions>(params:EditorParams,name:K,value:EditorExtensions[K]):EditorParams{
 const current=params.modules.extensions,extensions=record(current)?current:{};
 return setEditorModule(params,"extensions",{...extensions,[name]:value});
}
export function boundedNumber(raw:string,min:number,max:number){const value=Number(raw);if(raw.trim()===""||!Number.isFinite(value)||value<min||value>max)throw new Error("EDITOR_NUMBER_INVALID");return value}
export function updateBeauty(params:EditorParams,patch:Partial<BeautyModule>){const current=params.modules.beauty;if(!current?.enabled)throw new Error("EDITOR_MODULE_DISABLED");return setEditorModule(params,"beauty",{...current,...patch})}
export function updateDefects(params:EditorParams,patch:Partial<DefectsModule>){const current=params.modules.defects;if(!current?.enabled)throw new Error("EDITOR_MODULE_DISABLED");const next={...current,...patch};if(next.diameter_face_fraction[0]>next.diameter_face_fraction[1])throw new Error("EDITOR_DIAMETER_INVALID");return setEditorModule(params,"defects",next)}
export function updateGeometry(params:EditorParams,patch:Partial<GeometryModule>){const current=params.modules.geometry;if(!current?.enabled)throw new Error("EDITOR_MODULE_DISABLED");return setEditorModule(params,"geometry",{...current,...patch})}
export function updateRestoration(params:EditorParams,patch:Partial<RestorationExtension>){const current=params.modules.extensions?.restoration??{denoise:"off",denoise_strength:0,upscale:"off",sensor_dust:"off"};return setEditorExtension(params,"restoration",{...current,...patch})}
export function updateComposition(params:EditorParams,patch:Partial<CompositionExtension>){const current=params.modules.extensions?.composition;if(!current?.enabled)throw new Error("EDITOR_MODULE_DISABLED");const next={...current,...patch};if(next.aspect.length!==2||next.aspect.some(value=>!Number.isInteger(value)||value<1)||next.headroom<.01||next.headroom>.5||next.rotate_limit_deg<0||next.rotate_limit_deg>5||next.outpaint_seed<0||next.outpaint_seed>4294967295||!Number.isInteger(next.outpaint_seed)||next.outpaint_guidance_scale<1||next.outpaint_guidance_scale>20||next.outpaint_prompt.length>1000||next.outpaint_negative_prompt.length>1000||next.outpaint_policy==="review"&&!next.outpaint_prompt.trim()||next.outpaint_policy==="off"&&(next.outpaint_prompt!==""||next.outpaint_negative_prompt!==""))throw new Error("EDITOR_COMPOSITION_INVALID");return setEditorExtension(params,"composition",next)}
export function updateBackground(params:EditorParams,patch:Partial<BackgroundExtension>){const current=params.modules.extensions?.background??{mode:"keep",asset_id:null,solid_rgb:null,blur_strength:0,protect_subject:true};const next={...current,...patch};if(next.blur_strength<0||next.blur_strength>1||next.mode==="solid"&&(next.solid_rgb===null||next.solid_rgb.length!==3||next.solid_rgb.some(value=>value<0||value>1))||next.mode!=="solid"&&next.solid_rgb!==null||next.asset_id!==null)throw new Error("EDITOR_BACKGROUND_INVALID");return setEditorExtension(params,"background",next)}
