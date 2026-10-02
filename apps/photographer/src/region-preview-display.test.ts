import {it,expect,vi} from 'vitest';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {prepareRegionDisplay,type RegionBrowserDecoder} from './region-preview-display';
import type {VerifiedRegionManifest} from './region-preview-manifest';
const bytes=new Uint8Array([1,2,3]);
const manifest:VerifiedRegionManifest={geometry:{left:0,top:0,width:200,height:100,output_width:100,output_height:50},cacheKey:'a'.repeat(64),derivative:{artifact_id:'id',sha256:bytesToHex(sha256(bytes))},mediaType:'image/jpeg',bytes:3};
function decoder(){const close=vi.fn();return {close,decode:vi.fn().mockResolvedValue({width:100,height:50,close}),createURL:vi.fn().mockReturnValue('blob:region'),revokeURL:vi.fn()};}
it('closes decoded bitmap and revokes owned URL once on disposal or abort',async()=>{
 const value=decoder(),controller=new AbortController();const display=await prepareRegionDisplay(manifest,bytes,controller.signal,value);expect(value.close).toHaveBeenCalledOnce();expect(display.scale).toEqual({x:.5,y:.5,sourcePixels:false});controller.abort();display.dispose();expect(value.revokeURL).toHaveBeenCalledExactlyOnceWith('blob:region');
});
it('does not expose a URL after decoder dimension mismatch',async()=>{const value=decoder();value.decode.mockResolvedValue({width:101,height:50,close:value.close});await expect(prepareRegionDisplay(manifest,bytes,new AbortController().signal,value)).rejects.toThrow('DIMENSIONS');expect(value.close).toHaveBeenCalledOnce();expect(value.createURL).not.toHaveBeenCalled();});
it('rejects mutated bytes before invoking the decoder',async()=>{const value=decoder();await expect(prepareRegionDisplay(manifest,new Uint8Array([1,2,4]),new AbortController().signal,value)).rejects.toThrow('BYTES');expect(value.decode).not.toHaveBeenCalled();});
it('closes a bitmap received after abort even when decoder ignores cancellation',async()=>{
 let resolve!:(value:{width:number;height:number;close():void})=>void;const close=vi.fn();const value:RegionBrowserDecoder={decode:()=>new Promise(a=>{resolve=a;}),createURL:vi.fn(),revokeURL:vi.fn()};const controller=new AbortController();const pending=prepareRegionDisplay(manifest,bytes,controller.signal,value);controller.abort();resolve({width:100,height:50,close});await expect(pending).rejects.toThrow('ABORTED');expect(close).toHaveBeenCalledOnce();expect(value.createURL).not.toHaveBeenCalled();
});
