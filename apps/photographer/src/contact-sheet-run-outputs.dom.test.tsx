import {clickWhenEnabled} from './test/ui-readiness';
// @vitest-environment jsdom
import {canonicalJSON} from '@photoeditor/api-client';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
const open=vi.hoisted(()=>vi.fn());
const renderedPresses=vi.hoisted(()=>[] as Array<()=>void>);
const refreshPresses=vi.hoisted(()=>[] as Array<()=>void>);
vi.mock('react-native',async()=>({...await import('react-native-web'),Linking:{openURL:open}}));
vi.mock('react-native-paper',async()=>{const paper=await vi.importActual<typeof import('react-native-paper')>('react-native-paper'),React=await import('react');return {...paper,Button:(props:any)=>{if([props.children].flat().join('')==='Open PNG 1')renderedPresses.push(props.onPress);if(props.children==='Refresh contact sheet results')refreshPresses.push(props.onPress);return React.createElement(paper.Button,props);}};});
vi.mock('./region-preview-download',()=>({readRegionArtifact:async()=>new TextEncoder().encode(canonicalJSON({format:'png_pages',font_lock_id:'b'.repeat(64),labels_embedded:true,locale:'en',pages:[{artifact:{artifact_id:'page',sha256:'a'.repeat(64)},first_index:0,height:260,item_count:1,page:1,width:320}],total_items:1}))}));
import {ContactSheetRunOutputs} from './contact-sheet-run-outputs';
afterEach(()=>{cleanup();vi.clearAllMocks();renderedPresses.length=0;refreshPresses.length=0;});
const page=()=>({artifact_id:'page',role:'contact_page',sha256:'a'.repeat(64),bytes:100,media_type:'image/png',url:'https://media.test/page?versionId=v1',expires_at:new Date(Date.now()+60000).toISOString()});
it('opens authorized PNG outputs only after explicit click',async()=>{
 const api={runOutputs:vi.fn(async()=>({items:[page(),{...page(),artifact_id:'manifest',role:'contact_sheet',media_type:'application/json'}]}))};
 render(<ContactSheetRunOutputs api={api} run="run"/>);
 const button=await screen.findByRole('button',{name:'Open PNG 1'});expect(open).not.toHaveBeenCalled();await clickWhenEnabled(button);
 expect(open).toHaveBeenCalledWith(page().url);expect(screen.queryByText('Open PNG 2')).toBeNull();
});
it('ignores late output responses after scope unmount',async()=>{
 let resolve!:(value:{items:ReturnType<typeof page>[]})=>void;
 const api={runOutputs:vi.fn((_run:string,_signal?:AbortSignal)=>new Promise<{items:ReturnType<typeof page>[]}>(done=>resolve=done))};
 const view=render(<ContactSheetRunOutputs api={api} run="old"/>);view.unmount();
 expect(api.runOutputs.mock.calls[0][1]?.aborted).toBe(true);
 await act(async()=>resolve({items:[page()]}));expect(open).not.toHaveBeenCalled();
});
it('rejects expired results and refreshes the server-authorized links',async()=>{
 const api={runOutputs:vi.fn().mockResolvedValueOnce({items:[{...page(),expires_at:new Date(Date.now()-1).toISOString()},{...page(),role:'contact_sheet',artifact_id:'manifest'}]}).mockResolvedValueOnce({items:[page(),{...page(),role:'contact_sheet',artifact_id:'manifest'}]})};
 render(<ContactSheetRunOutputs api={api} run="run"/>);await screen.findByRole('alert');
 expect(screen.queryByText('Open PNG 1')).toBeNull();await clickWhenEnabled(screen.getByRole('button',{name:'Refresh contact sheet results'}));
 await screen.findByRole('button',{name:'Open PNG 1'});await waitFor(()=>expect(api.runOutputs).toHaveBeenCalledTimes(2));
});
it('Cancel then Refresh refuses late old outputs without hiding the replacement',async()=>{
 let resolve!:(value:unknown)=>void;const api={runOutputs:vi.fn().mockReturnValueOnce(new Promise(done=>{resolve=done})).mockResolvedValue({items:[page(),{...page(),artifact_id:'manifest',role:'contact_sheet',media_type:'application/json'}]})};render(<ContactSheetRunOutputs api={api} run="run"/>);await clickWhenEnabled(screen.getByText('Cancel contact sheet loading'));await clickWhenEnabled(screen.getByText('Refresh contact sheet results'));await screen.findByText('Open PNG 1');await act(async()=>resolve({items:[]}));expect(screen.getByText('Open PNG 1')).toBeTruthy();
});
it('bounds stalled output acquisition and lets the user retry explicitly',async()=>{
 vi.useFakeTimers();try{const api={runOutputs:vi.fn().mockReturnValue(new Promise(()=>{}))};render(<ContactSheetRunOutputs api={api} run="run"/>);await act(async()=>{await vi.advanceTimersByTimeAsync(8000)});expect(screen.getByText('Refresh contact sheet results')).toBeTruthy();expect(screen.queryByText('Cancel contact sheet loading')).toBeNull();}finally{vi.useRealTimers();}
});
it('withdraws parent authority when output access expires',async()=>{
 const {APIError}=await import('@photoeditor/api-client');const denied=vi.fn(),api={runOutputs:vi.fn().mockRejectedValue(new APIError(410,'EXPIRED'))};render(<ContactSheetRunOutputs api={api} run="run" onAccessDenied={denied}/>);await screen.findByRole('alert');expect(denied).toHaveBeenCalledOnce();
});
it('retained old PNG action cannot reuse a replacement display controller after Refresh',async()=>{
 const api={runOutputs:vi.fn().mockResolvedValue({items:[page(),{...page(),artifact_id:'manifest',role:'contact_sheet',media_type:'application/json'}]})};const view=render(<ContactSheetRunOutputs api={api} run="first"/>);await screen.findByText('Open PNG 1');const old=renderedPresses.at(-1)!;expect(old).toBeTypeOf('function');view.rerender(<ContactSheetRunOutputs api={api} run="second"/>);await screen.findByText('Open PNG 1');await act(async()=>old());expect(open).not.toHaveBeenCalled();await clickWhenEnabled(screen.getByText('Open PNG 1'));expect(open).toHaveBeenCalledOnce();
});

it('retained Refresh cannot restart a replacement run or its active output request',async()=>{
 const api={runOutputs:vi.fn().mockRejectedValueOnce(new Error('first unavailable')).mockReturnValue(new Promise(()=>{}))};
 const view=render(<ContactSheetRunOutputs api={api} run="first"/>);await screen.findByText('Refresh contact sheet results');const old=refreshPresses.at(-1)!;
 view.rerender(<ContactSheetRunOutputs api={api} run="second"/>);await waitFor(()=>expect(api.runOutputs).toHaveBeenCalledTimes(2));
 const replacement=api.runOutputs.mock.calls[1][1] as AbortSignal;act(()=>old());
 expect(api.runOutputs).toHaveBeenCalledTimes(2);expect(replacement.aborted).toBe(false);
});
it('retained Refresh cannot restart its own replacement request after retry',async()=>{
 const api={runOutputs:vi.fn().mockRejectedValueOnce(new Error('unavailable')).mockReturnValue(new Promise(()=>{}))};
 render(<ContactSheetRunOutputs api={api} run="run"/>);await screen.findByText('Refresh contact sheet results');const old=refreshPresses.at(-1)!;
 await clickWhenEnabled(screen.getByText('Refresh contact sheet results'));await waitFor(()=>expect(api.runOutputs).toHaveBeenCalledTimes(2));
 const replacement=api.runOutputs.mock.calls[1][1] as AbortSignal;act(()=>old());
 expect(api.runOutputs).toHaveBeenCalledTimes(2);expect(replacement.aborted).toBe(false);
});
