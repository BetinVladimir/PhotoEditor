/** Frozen RAW parameters; decoder identity comes from a qualified deployment. */
export type RAWDevelopment={schema_version:1;demosaic:'ahd'|'dcb';wb_source:'camera'|'daylight'|'auto';exposure_ev:number;highlight_policy:'clip'|'blend'|'reconstruct';decoder_version:string};
export function readRAWDevelopment(value:unknown):RAWDevelopment{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('RAW_DEVELOPMENT_INVALID');
 const p=value as Record<string,unknown>;
 const fields=['schema_version','demosaic','wb_source','exposure_ev','highlight_policy','decoder_version'];
 if(Object.keys(p).length!==fields.length||fields.some(key=>!Object.prototype.hasOwnProperty.call(p,key))||
 p.schema_version!==1||!['ahd','dcb'].includes(p.demosaic as string)||!['camera','daylight','auto'].includes(p.wb_source as string)||
 typeof p.exposure_ev!=='number'||!Number.isFinite(p.exposure_ev)||p.exposure_ev< -2||p.exposure_ev>3||
 !['clip','blend','reconstruct'].includes(p.highlight_policy as string)||typeof p.decoder_version!=='string'||p.decoder_version.length>128||
 !/^rawpy-[0-9]+\.[0-9]+\.[0-9]+\/libraw-[0-9]+\.[0-9]+\.[0-9]+$/.test(p.decoder_version))throw new Error('RAW_DEVELOPMENT_INVALID');
 return {...p} as RAWDevelopment;
}
export function rawUploadPolicy(name:string,value:unknown):RAWDevelopment{
 if(!/\.(dng|cr2|nef|arw|raf|orf|rw2)$/i.test(name))throw new Error('RAW_DEVELOPMENT_ON_RASTER');
 return readRAWDevelopment(value);
}

export type RAWCapabilityView={decoder_version:string;demosaic:RAWDevelopment['demosaic'][];wb_source:RAWDevelopment['wb_source'][];highlight_policy:RAWDevelopment['highlight_policy'][];exposure_min:number;exposure_max:number};
export type RAWCapabilities={project_id:string;available:boolean;development?:RAWCapabilityView};
const rawUUID=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const rawObject=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const rawFields=(v:Record<string,unknown>,fields:string[])=>Object.keys(v).length===fields.length&&fields.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const exactModes=(v:unknown,expected:string[])=>Array.isArray(v)&&v.length===expected.length&&new Set(v).size===v.length&&expected.every(mode=>v.includes(mode));
/** Validate project binding and own every mutable option list before display. */
export function readRAWCapabilities(value:unknown,project:string):RAWCapabilities{
 const invalid=()=>{throw new Error('RAW_CAPABILITIES_INVALID')};
 if(!rawUUID(project)||!rawObject(value)||value.project_id!==project||typeof value.available!=='boolean')return invalid();
 if(!value.available){if(!rawFields(value,['project_id','available']))return invalid();return {project_id:project,available:false}}
 if(!rawFields(value,['project_id','available','development'])||!rawObject(value.development))return invalid();
 const d=value.development;
 if(!rawFields(d,['decoder_version','demosaic','wb_source','highlight_policy','exposure_min','exposure_max'])||
 !exactModes(d.demosaic,['ahd','dcb'])||!exactModes(d.wb_source,['camera','daylight','auto'])||!exactModes(d.highlight_policy,['clip','blend','reconstruct'])||d.exposure_min!==-2||d.exposure_max!==3)return invalid();
 const probe=readRAWDevelopment({schema_version:1,demosaic:'ahd',wb_source:'camera',exposure_ev:0,highlight_policy:'clip',decoder_version:d.decoder_version});
 return {project_id:project,available:true,development:{decoder_version:probe.decoder_version,demosaic:[...(d.demosaic as RAWCapabilityView['demosaic'])],wb_source:[...(d.wb_source as RAWCapabilityView['wb_source'])],highlight_policy:[...(d.highlight_policy as RAWCapabilityView['highlight_policy'])],exposure_min:-2,exposure_max:3}};
}
/** Explicit selection only: callers cannot override the qualified decoder. */
export function selectRAWDevelopment(capabilities:RAWCapabilities,project:string,settings:Omit<RAWDevelopment,'schema_version'|'decoder_version'>):RAWDevelopment{
 const current=readRAWCapabilities(capabilities,project);
 if(!current.available||!current.development)throw new Error('RAW_QUALIFICATION_UNAVAILABLE');
 if(!rawObject(settings)||!rawFields(settings,['demosaic','wb_source','exposure_ev','highlight_policy']))throw new Error('RAW_DEVELOPMENT_INVALID');
 return readRAWDevelopment({schema_version:1,...settings,decoder_version:current.development.decoder_version});
}
