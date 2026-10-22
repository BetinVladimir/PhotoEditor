// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {ExportWatermarkPicker} from './export-watermark-picker';
afterEach(cleanup);
it('selects a verified ACL-listed asset and refuses repeated pages',async()=>{const id='00000000-0000-4000-8000-000000000001',change=vi.fn(),api={watermarkAssets:vi.fn().mockResolvedValueOnce({items:[{id,upload_state:'verified',metadata:{filename:'logo.png'}}],next_cursor:'next'}).mockResolvedValueOnce({items:[],next_cursor:'next'})};render(<ExportWatermarkPicker api={api as never} onChange={change} disabled={false}/>);fireEvent.click(await screen.findByRole('radio',{name:'Watermark logo.png'}));expect(change).toHaveBeenCalledWith(id);fireEvent.click(screen.getByText('More watermark images'));await screen.findByText('Unable to load watermark images')});
it('does not accept a late previous-account asset list',async()=>{let resolve!:(value:unknown)=>void;const old={watermarkAssets:()=>new Promise(r=>{resolve=r})},current={watermarkAssets:async()=>({items:[],next_cursor:null})};const view=render(<ExportWatermarkPicker api={old as never} onChange={()=>{}} disabled={false}/>);view.rerender(<ExportWatermarkPicker api={current as never} onChange={()=>{}} disabled={false}/>);resolve({items:[{id:'00000000-0000-4000-8000-000000000001',upload_state:'verified',metadata:{filename:'old.png'}}],next_cursor:null});expect(screen.queryByText('old.png')).toBeNull()});

it('cancels a watermark page, ignores the old inventory and retries the same cursor',async()=>{
 let finish!:(value:unknown)=>void;const id='00000000-0000-4000-8000-000000000001';
 const api={watermarkAssets:vi.fn().mockResolvedValueOnce({items:[{id,upload_state:'verified',metadata:{filename:'logo.png'}}],next_cursor:'next'}).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({items:[],next_cursor:null})};
 render(<ExportWatermarkPicker api={api as never} onChange={()=>{}} disabled={false}/>);await screen.findByRole('radio',{name:'Watermark logo.png'});fireEvent.click(screen.getByText('More watermark images'));
 fireEvent.click(screen.getByText('Stop loading'));await act(async()=>finish({items:[{id:'00000000-0000-4000-8000-000000000002',upload_state:'verified',metadata:{filename:'late.png'}}],next_cursor:null}));
 expect(screen.queryByText('late.png')).toBeNull();fireEvent.click(screen.getByText('Retry image list'));await act(async()=>{});
 expect(api.watermarkAssets.mock.calls[2][0]).toBe('next');expect(screen.getByRole('radio',{name:'Watermark logo.png'})).toBeTruthy();
});
