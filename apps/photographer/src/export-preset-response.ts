import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {ExportPreset,ExportPresetConfig} from './catalog-types';
const fail=():never=>{throw new Error('PRESET_METADATA_RECEIPT_INVALID')};
const uuid=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
function object(v:unknown,required:string[],optional:string[]=[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&required.every(k=>Object.hasOwn(v,k))&&Object.keys(v).every(k=>required.includes(k)||optional.includes(k));}
const range=(v:unknown,min:number,max:number,integer=false)=>typeof v==='number'&&Number.isFinite(v)&&v>=min&&v<=max&&(!integer||Number.isSafeInteger(v));
export function exportPresetConfigHash(config:ExportPresetConfig){return bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(config))));}
export function readExportPresetResponse(value:unknown):ExportPreset{
 if(!object(value,['id','name','revision','config','config_hash','created_at'])||!uuid(value.id)||typeof value.name!=='string'||!value.name.trim()||[...value.name].length>120||typeof value.revision!=='string'||!/^[1-9][0-9]{0,15}$/.test(value.revision)||BigInt(value.revision)>9007199254740991n||typeof value.created_at!=='string'||!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(value.created_at)||!Number.isFinite(Date.parse(value.created_at))||typeof value.config_hash!=='string'||!/^[0-9a-f]{64}$/.test(value.config_hash))return fail();
 readExportPresetConfig(value.config);
 const owned=structuredClone(value) as unknown as ExportPreset;if(exportPresetConfigHash(owned.config)!==owned.config_hash)return fail();return owned;
}
export function readExportPresetConfig(value:unknown):ExportPresetConfig{
 const c=value;
 if(!object(c,['format','icc_id','subsampling','alpha','metadata_policy','naming_template'],['size','quality','matte_rgb','watermark','sharpen'])||!['tiff','png','jpeg'].includes(c.format as string)||!['srgb_v1','work_rec2020_gamma24_v1'].includes(c.icc_id as string)||c.subsampling!=='444'||!['preserve','matte'].includes(c.alpha as string)||!['strip','copyright','approved_exif'].includes(c.metadata_policy as string)||typeof c.naming_template!=='string'||!c.naming_template.trim()||new TextEncoder().encode(c.naming_template).length>200||/[\\/\p{Cc}]/u.test(c.naming_template)||/[{}]/.test(c.naming_template.replace(/\{(?:original_name|name|sha8|ordinal)\}/g,'')))return fail();
 if(Object.hasOwn(c,'size')){
  const size=c.size;if(!object(size,['fit','allow_upscale'],['long_edge','width','height'])||size.fit!=='contain'||typeof size.allow_upscale!=='boolean')return fail();
  const dims=['long_edge','width','height'].filter(k=>Object.hasOwn(size,k));
  if(!(dims.length===1&&dims[0]==='long_edge'||dims.length===2&&dims.includes('width')&&dims.includes('height'))||dims.some(k=>!range(size[k],1,100000,true)))return fail();
 }
 if(c.format==='jpeg'){if(c.alpha!=='matte'||!range(c.quality,1,100,true))return fail();}else if(Object.hasOwn(c,'quality'))return fail();
 if(c.alpha==='matte'){if(!Array.isArray(c.matte_rgb)||c.matte_rgb.length!==3||c.matte_rgb.some(v=>!range(v,0,1)))return fail();}else if(Object.hasOwn(c,'matte_rgb'))return fail();
 if(Object.hasOwn(c,'watermark')){const w=c.watermark;if(!object(w,['asset_id','opacity','anchor','margin_x','margin_y'])||!uuid(w.asset_id)||!range(w.opacity,0,1)||!['top_left','top_right','bottom_left','bottom_right','center'].includes(w.anchor as string)||!range(w.margin_x,0,Number.MAX_SAFE_INTEGER,true)||!range(w.margin_y,0,Number.MAX_SAFE_INTEGER,true))return fail();}
 if(Object.hasOwn(c,'sharpen')){const s=c.sharpen;if(!object(s,['radius','strength','threshold'])||!range(s.radius,.1,5)||!range(s.strength,0,1)||!range(s.threshold,0,.1))return fail();}
 return structuredClone(c) as unknown as ExportPresetConfig;
}
export function freezeExportPresetCreate(name:string,config:ExportPresetConfig):{name:string;config:ExportPresetConfig}{
 if(typeof name!=='string'||!name.trim()||[...name].length>120)return fail();
 // Freeze the actual JSON request, omitting the form's optional undefined fields.
 const wire=JSON.parse(JSON.stringify(config)) as unknown;
 return {name,config:readExportPresetConfig(wire)};
}
export function readCreatedExportPreset(value:unknown,request:{name:string;config:ExportPresetConfig}):ExportPreset{
 const preset=readExportPresetResponse(value);
 if(preset.name!==request.name.trim()||preset.revision!=='1'||canonicalJSON(preset.config)!==canonicalJSON(request.config))return fail();
 return preset;
}
export function readExportPresetPage(value:unknown):{items:ExportPreset[];next_cursor:string|null}{
 if(!object(value,['items','next_cursor'])||!Array.isArray(value.items)||value.items.length>100||value.next_cursor!==null&&(typeof value.next_cursor!=='string'||!value.next_cursor||value.next_cursor.length>4096))return fail();
 const items=value.items.map(readExportPresetResponse);
 if(new Set(items.map(item=>item.id)).size!==items.length)return fail();
 return {items,next_cursor:value.next_cursor as string|null};
}
/** Legacy journals may lack an original baseline. Their exact historical receipt
 * remains recoverable even if the current catalog has subsequently advanced. */
export function readPresetMetadataReceipt(value:unknown,expected:{id:string;revision:string;mode:ExportPresetConfig['metadata_policy']},baseline?:ExportPreset|null):ExportPreset{
 const after=readExportPresetResponse(value);
 if(!uuid(expected.id)||!/^[1-9][0-9]{0,15}$/.test(expected.revision)||BigInt(expected.revision)>=9007199254740991n||!['strip','copyright','approved_exif'].includes(expected.mode)||after.id!==expected.id||after.revision!==(BigInt(expected.revision)+1n).toString()||after.config.metadata_policy!==expected.mode)return fail();
 if(baseline){const before=readExportPresetResponse(baseline);if(before.id!==expected.id||before.revision!==expected.revision||after.name!==before.name||after.created_at!==before.created_at||canonicalJSON(after.config)!==canonicalJSON({...before.config,metadata_policy:expected.mode}))return fail();}
 return after;
}
export function readPresetMetadataResponse(value:unknown,baseline:ExportPreset,mode:ExportPresetConfig['metadata_policy']):ExportPreset{
 return readPresetMetadataReceipt(value,{id:baseline.id,revision:baseline.revision,mode},baseline);
}
