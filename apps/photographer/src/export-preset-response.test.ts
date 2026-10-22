import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {exportPresetConfigHash,readExportPresetResponse,readPresetMetadataResponse,readPresetMetadataReceipt,readExportPresetPage,freezeExportPresetCreate,readCreatedExportPreset} from './export-preset-response';
import type {ExportPreset} from './catalog-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture():ExportPreset{const config={format:'jpeg' as const,icc_id:'srgb_v1' as const,subsampling:'444' as const,alpha:'matte' as const,matte_rgb:[1,1,1],quality:92,metadata_policy:'strip' as const,naming_template:'{original_name}_{ordinal}',size:{long_edge:1600,fit:'contain' as const,allow_upscale:false},watermark:{asset_id:id(2),opacity:.5,anchor:'bottom_right' as const,margin_x:8,margin_y:9},sharpen:{radius:.8,strength:.3,threshold:.02}};return {id:id(1),name:'Print',revision:'7',config,config_hash:exportPresetConfigHash(config),created_at:'2026-10-17T00:00:00Z'}}
function changed(before=fixture()):ExportPreset{const config={...before.config,metadata_policy:'copyright' as const};return {...before,revision:'8',config,config_hash:exportPresetConfigHash(config)}}
it('owns exact full preset and accepts only metadata change with original baseline',()=>{const before=fixture(),after=changed(before),owned=readPresetMetadataResponse(after,before,'copyright');after.config.quality=1;expect(owned.config.quality).toBe(92);expect(readExportPresetResponse(before)).toEqual(before)});
it('rejects validly rehashed immutable parameter changes',()=>{for(const patch of [{quality:80},{icc_id:'work_rec2020_gamma24_v1' as const},{watermark:undefined},{sharpen:{radius:1,strength:.2,threshold:.01}}]){const before=fixture(),after=changed(before);Object.assign(after.config,patch);if(after.config.watermark===undefined)delete after.config.watermark;after.config_hash=exportPresetConfigHash(after.config);expect(()=>readPresetMetadataResponse(after,before,'copyright')).toThrow('PRESET_METADATA_RECEIPT_INVALID')}});
it('rejects hash, scope, revision, unknown fields and malformed full config',()=>{const before=fixture(),after=changed(before);for(const bad of [{...after,config_hash:'a'.repeat(64)},{...after,id:id(9)},{...after,revision:'9'},{...after,extra:true},{...after,config:{metadata_policy:'copyright'}},{...after,config:{...after.config,quality:null}}])expect(()=>readPresetMetadataResponse(bad,before,'copyright')).toThrow()});
it('recovers an exact old receipt without inventing baseline from newer catalog',()=>{const receipt=changed(),expected={id:id(1),revision:'7',mode:'copyright' as const};expect(readPresetMetadataReceipt(receipt,expected,null)).toEqual(receipt);expect(()=>readPresetMetadataReceipt({...receipt,revision:'10'},expected,null)).toThrow();expect(()=>readPresetMetadataReceipt({...receipt,config_hash:'f'.repeat(64)},expected,null)).toThrow()});

it('decodes complete preset pages before exposing any catalog rows',()=>{
 const item=fixture(),page={items:[item],next_cursor:null};const owned=readExportPresetPage(page);item.config.quality=1;expect(owned.items[0].config.quality).toBe(92);
 for(const bad of [{items:[]},{items:[],next_cursor:null,extra:true},{items:[fixture(),fixture()],next_cursor:null},{items:[{id:id(1)}],next_cursor:null}])expect(()=>readExportPresetPage(bad)).toThrow();
});
it('binds newly created preset to the frozen normalized request',()=>{
 const before=fixture(),request=freezeExportPresetCreate(' Print ',{...before.config,watermark:undefined});const created={...before,name:'Print',revision:'1',config:request.config,config_hash:exportPresetConfigHash(request.config)};
 expect(readCreatedExportPreset(created,request)).toEqual(created);
 for(const bad of [{...created,name:'Other'},{...created,revision:'2'},{...created,config:before.config,config_hash:before.config_hash}])expect(()=>readCreatedExportPreset(bad,request)).toThrow();
});
it('freezes preset config before transport and refuses late cancellation',async()=>{
 const config=fixture().config,expected=structuredClone(config),abort=new AbortController();
 const request=vi.fn().mockImplementation(async(_path,options)=>{expect(options.body.config).toEqual(expected);config.quality=1;return {...fixture(),revision:'1',config:expected,config_hash:exportPresetConfigHash(expected)}});
 const api=new OwnerAPI({request} as unknown as ApiClient,id(9));expect((await api.createPreset('Print',config,id(8))).config.quality).toBe(92);
 request.mockImplementationOnce(async()=>{abort.abort();return {items:[],next_cursor:null}});
 await expect(api.presets(undefined,abort.signal)).rejects.toMatchObject({name:'AbortError'});
 const pre=new AbortController();pre.abort();await expect(api.createPreset('Print',expected,id(8),pre.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(2);
});
