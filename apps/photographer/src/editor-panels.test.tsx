// @vitest-environment jsdom
import React from 'react';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>import('react-native-web'));
import {EditorPanels} from './editor-panels';
import type {Capabilities} from './catalog-types';

afterEach(cleanup);
const params={schema_version:1,modules:{geometry:{enabled:true,algorithm:'bodywarp_v1',body_strength:.5,face_slim:0,waist_slim:.02,hip_slim:.02,torso_slim:.02,leg_length:0,protect_background:true,max_displacement_image_fraction:.02,on_unsafe:'skip_module'},extensions:{composition:{enabled:true,aspect:[3,2],headroom:.12,rotate_limit_deg:2,outpaint_policy:'review',outpaint_prompt:'Extend scene',outpaint_negative_prompt:'',outpaint_seed:0,outpaint_guidance_scale:7.5},background:{mode:'solid',asset_id:null,solid_rgb:[1,1,1],blur_strength:0,protect_subject:true}}}};
const operations=['geometry.body','composition.structure','composition.plan','geometry.crop','image.outpaint','background.compose'];
function capabilities(ready:boolean):Capabilities{return {operations:operations.map(operation=>({operation,resource_class:'gpu',supported_backends:['cuda'],available_backends:ready?['cuda']:[],available:ready,status:ready?'ready':'unqualified'}))};}
const fields=['Body strength','Face slim','Waist slim','Hip slim','Torso slim','Leg length','Maximum displacement','Headroom','Rotation limit (degrees)','Outpaint prompt','Outpaint negative prompt','Outpaint seed','Outpaint guidance','Background red','Background green','Background blue'];
it('blocks nested changes when capability has not loaded, including synthetic input events',()=>{
 const onChange=vi.fn();render(<EditorPanels params={params} extendedApply onChange={onChange} onError={vi.fn()}/>);
 for(const label of fields){const input=screen.getByLabelText(label) as HTMLInputElement;expect(input.readOnly).toBe(true);fireEvent.change(input,{target:{value:'.1'}});}
 expect(onChange).not.toHaveBeenCalled();
});
it('locks existing enabled modules when qualified worker availability disappears',()=>{
 const onChange=vi.fn(),onError=vi.fn();const view=render(<EditorPanels params={params} capabilities={capabilities(true)} extendedApply onChange={onChange} onError={onError}/>);
 for(const label of fields)expect((screen.getByLabelText(label) as HTMLInputElement).readOnly).toBe(false);
 view.rerender(<EditorPanels params={params} capabilities={capabilities(false)} extendedApply onChange={onChange} onError={onError}/>);
 for(const label of fields){const input=screen.getByLabelText(label) as HTMLInputElement;expect(input.readOnly).toBe(true);fireEvent.change(input,{target:{value:'.1'}});}
 expect(onChange).not.toHaveBeenCalled();
});
it('allows a server-qualified CUDA operation independently of the client operating system',()=>{
 const onChange=vi.fn();render(<EditorPanels params={params} capabilities={capabilities(true)} extendedApply onChange={onChange} onError={vi.fn()}/>);
 fireEvent.change(screen.getByLabelText('Waist slim'),{target:{value:'.04'}});
 expect(onChange).toHaveBeenCalledTimes(1);expect(onChange.mock.calls[0][0].modules.geometry.waist_slim).toBe(.04);
});
it('keeps composition fields locked without Apply v4 even with ready workers',()=>{
 render(<EditorPanels params={params} capabilities={capabilities(true)} onChange={vi.fn()} onError={vi.fn()}/>);
 for(const label of fields)expect((screen.getByLabelText(label) as HTMLInputElement).readOnly).toBe(true);
});

it('does not unlock controls for contradictory ready metadata',()=>{
 const inconsistent=capabilities(true);inconsistent.operations=inconsistent.operations.map(item=>({...item,available_backends:[]}));
 render(<EditorPanels params={params} capabilities={inconsistent} extendedApply onChange={vi.fn()} onError={vi.fn()}/>);
 for(const label of fields)expect((screen.getByLabelText(label) as HTMLInputElement).readOnly).toBe(true);
});

it('explains unavailable features without exposing internal evidence codes',()=>{
 const value=capabilities(false);value.operations.forEach(item=>item.reason='api_installation_missing');
 render(<EditorPanels params={params} capabilities={value} extendedApply onChange={vi.fn()} onError={vi.fn()}/>);
 expect(screen.queryByText(/api_installation_missing/)).toBeNull();
 expect(screen.getAllByText('This function is temporarily unavailable. Other editing tools remain available.').length).toBeGreaterThan(0);
});
