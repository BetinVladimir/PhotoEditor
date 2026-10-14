import {describe,it,expect} from 'vitest';
import {readRAWDevelopment,rawUploadPolicy,readRAWCapabilities,selectRAWDevelopment} from './raw-development';
const policy=()=>({schema_version:1 as const,demosaic:'ahd' as const,wb_source:'camera' as const,exposure_ev:0,highlight_policy:'clip' as const,decoder_version:'rawpy-0.27.1/libraw-0.21.4'});
describe('RAW development request',()=>{
 it('returns an owned complete policy',()=>{const p=policy();const owned=rawUploadPolicy('frame.NEF',p);p.exposure_ev=2;expect(owned.exposure_ev).toBe(0)});
 it('refuses malformed, missing, unknown and nonfinite settings',()=>{
  const missing:Record<string,unknown>=policy();delete missing.exposure_ev;
  for(const p of [null,missing,{...policy(),extra:true},{...policy(),exposure_ev:NaN},{...policy(),exposure_ev:4},{...policy(),decoder_version:'unlocked'},{...policy(),wb_source:'guess'}])expect(()=>readRAWDevelopment(p)).toThrow();
 });
 it('refuses RAW policy on raster or unsupported format',()=>{for(const name of ['frame.jpg','frame.png','frame.tiff','frame.unknown'])expect(()=>rawUploadPolicy(name,policy())).toThrow('RAW_DEVELOPMENT_ON_RASTER')});
});

const project='11111111-1111-4111-8111-111111111111';
const catalogue=()=>({project_id:project,available:true,development:{decoder_version:policy().decoder_version,demosaic:['ahd','dcb'],wb_source:['camera','daylight','auto'],highlight_policy:['clip','blend','reconstruct'],exposure_min:-2,exposure_max:3}});
describe('RAW qualified catalogue',()=>{
 it('owns mode lists and freezes explicit selection with qualified decoder',()=>{
  const input=catalogue();const read=readRAWCapabilities(input,project);input.development.demosaic[0]='changed';
  const settings={demosaic:'dcb' as const,wb_source:'auto' as const,highlight_policy:'blend' as const,exposure_ev:2};
  const selected=selectRAWDevelopment(read,project,settings);settings.exposure_ev=0;
  expect(read.development?.demosaic).toEqual(['ahd','dcb']);expect(selected.exposure_ev).toBe(2);expect(selected.decoder_version).toBe(policy().decoder_version);
 });
 it('refuses foreign scope, hidden options, missing/duplicated modes and decoder guessing',()=>{
  for(const input of [null,{...catalogue(),project_id:'22222222-2222-4222-8222-222222222222'},{...catalogue(),available:false}, {...catalogue(),development:{...catalogue().development,demosaic:['ahd','ahd']}},{...catalogue(),development:{...catalogue().development,wb_source:['camera']}},{...catalogue(),development:{...catalogue().development,decoder_version:'latest'}},{...catalogue(),development:{...catalogue().development,exposure_max:4}}])expect(()=>readRAWCapabilities(input,project)).toThrow();
 });
 it('keeps unavailable catalogue free of settings and refuses selection',()=>{
  const unavailable=readRAWCapabilities({project_id:project,available:false},project);expect(unavailable.development).toBeUndefined();
  expect(()=>selectRAWDevelopment(unavailable,project,{demosaic:'ahd',wb_source:'camera',exposure_ev:0,highlight_policy:'clip'})).toThrow('RAW_QUALIFICATION_UNAVAILABLE');
 });
});
