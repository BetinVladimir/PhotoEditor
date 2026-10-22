// @vitest-environment jsdom
import {clickWhenEnabled} from './test/ui-readiness';
import {afterEach,beforeEach,it,expect,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
const mock=vi.hoisted(()=>({create:vi.fn(),start:vi.fn(),resolve:vi.fn()}));
vi.mock('../../portal/src/lib/api',async()=>({...await vi.importActual<typeof import('../../portal/src/lib/api')>('../../portal/src/lib/api'),secretFromFragment:()=>null,getCollection:vi.fn(async()=>({name:'Gallery',description:'',revision:'1',capabilities:['download'],allowed_presets:['30000000-0000-4000-8000-000000000003'],sections:[],empty:false})),createSelection:mock.create,startDownload:mock.start,abandonDownloadPreparation:mock.resolve,getDownload:vi.fn(async(id:string)=>({id,status:'queued',files_done:0,files_total:1,bytes_written:0,volumes:[]}))}));
import {Portal} from '../../portal/src/components/Portal';
import portalEnglish from '../../portal/src/catalogs/en.json';
import {APIError} from '../../portal/src/lib/api';
const share='30000000-0000-4000-8000-000000000001';
beforeEach(()=>{vi.clearAllMocks();sessionStorage.clear();sessionStorage.setItem(`photo:portal:${share}:csrf`,crypto.randomUUID());Object.defineProperty(document,'visibilityState',{configurable:true,value:'visible'});mock.create.mockRejectedValue(new Error('lost selection response'));mock.resolve.mockReset();});
afterEach(()=>cleanup());
it('keeps unknown keys after later409 and resolves acceptedjob without another Start',async()=>{
 render(<Portal shareID={share} locale="en"/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'Download'}));
 await screen.findByRole('button',{name:'Resolve pending preparation'});
 mock.create.mockRejectedValue(new APIError(409,'IDEMPOTENCY_CONFLICT'));
 fireEvent.click(screen.getByRole('button',{name:'Download'}));
 await screen.findByRole('button',{name:'Resolve pending preparation'});
 expect(mock.create.mock.calls[1][4]).toBe(mock.create.mock.calls[0][4]);
 mock.resolve.mockResolvedValue({status:'accepted',ticket:{download_job_id:'30000000-0000-4000-8000-000000000004',status:'queued'}});
 fireEvent.click(screen.getByRole('button',{name:'Resolve pending preparation'}));
 await screen.findByText(portalEnglish.status_queued);expect(mock.start).not.toHaveBeenCalled();expect(mock.resolve).toHaveBeenCalledTimes(1);
 expect(screen.queryByRole('button',{name:'Resolve pending preparation'})).toBeNull();
});
it('does not discard uncertainty when resolution transport fails',async()=>{
 render(<Portal shareID={share} locale="en"/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Download'}));
 await screen.findByRole('button',{name:'Resolve pending preparation'});mock.resolve.mockRejectedValue(Error('lost resolution'));
 fireEvent.click(screen.getByRole('button',{name:'Resolve pending preparation'}));
 await screen.findByRole('button',{name:'Resolve pending preparation'});expect(mock.resolve).toHaveBeenCalledTimes(1);expect(mock.create).toHaveBeenCalledTimes(1);
});
