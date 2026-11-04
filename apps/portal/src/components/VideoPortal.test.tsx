import {afterEach,expect,it,vi} from 'vitest';
import React,{act} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {JSDOM} from 'jsdom';

const apiMock=vi.hoisted(()=>({
 exchange:vi.fn(),getVideoDelivery:vi.fn(),
 secretFromFragment:(h:string)=>h.startsWith('#')&&h.length>1?h.slice(1):null,
 videoContentURL:(id:string,d=false)=>`/content/${id}${d?'?download=1':''}`,
 APIError:class extends Error{status:number;constructor(status:number){super('x');this.status=status;}},
}));
vi.mock('../lib/api',()=>apiMock);
import {VideoPortal} from './VideoPortal';

const share='48cf689a-c8c0-4f70-83a7-982fd95fd946';
const delivery={name:'Свадьба',items:[
 {render_id:'r1',title:'Тизер',ordinal:0,duration_ticks:705600000*65,width:1920,height:1080,bytes:10},
 {render_id:'r2',title:'Полная',ordinal:1,duration_ticks:705600000*300,width:1920,height:1080,bytes:20}]};
let root:Root|undefined,dom:JSDOM|undefined;
afterEach(async()=>{if(root)await act(async()=>root!.unmount());root=undefined;dom?.window.close();dom=undefined;vi.clearAllMocks();});

async function mount(hash:string){
 dom=new JSDOM('<div id="host"></div>',{url:`https://framevela.com/ru/v/${share}${hash}`});
 for(const key of ['window','document','HTMLElement','Node','MouseEvent','history','location','Event'])Object.defineProperty(globalThis,key,{configurable:true,value:(dom.window as any)[key]});
 (globalThis as any).IS_REACT_ACT_ENVIRONMENT=true;
 root=createRoot(document.getElementById('host')!);
 await act(async()=>root!.render(<VideoPortal locale="ru" shareID={share}/>));
 await act(async()=>{});
}

it('exchanges the fragment secret, strips it from the address and plays the first render',async()=>{
 apiMock.exchange.mockResolvedValue({capabilities:{download:false}});
 apiMock.getVideoDelivery.mockResolvedValue(delivery);
 await mount('#secret-1');
 expect(apiMock.exchange).toHaveBeenCalledWith(share,'secret-1','ru',expect.anything());
 expect(location.hash).toBe('');
 expect(document.querySelector('h1')?.textContent).toBe('Свадьба');
 expect(document.querySelector('video')?.getAttribute('src')).toBe('/content/r1');
 expect(document.querySelector('a[download]')).toBeNull();
});

it('offers download only with the capability and switches renders from the list',async()=>{
 apiMock.exchange.mockResolvedValue({capabilities:{download:true}});
 apiMock.getVideoDelivery.mockResolvedValue(delivery);
 await mount('#secret-1');
 expect(document.querySelector('a[download]')?.getAttribute('href')).toBe('/content/r1?download=1');
 const buttons=Array.from(document.querySelectorAll('nav button'));
 expect(buttons.map(b=>b.textContent)).toEqual(['Тизер','Полная']);
 await act(async()=>{buttons[1].dispatchEvent(new dom!.window.MouseEvent('click',{bubbles:true}));});
 expect(document.querySelector('video')?.getAttribute('src')).toBe('/content/r2');
});

it('shows the invalid-link message for a 403 and never reaches the player',async()=>{
 apiMock.exchange.mockRejectedValue(new apiMock.APIError(403));
 await mount('#bad');
 expect(document.querySelector('video')).toBeNull();
 expect(document.querySelector('[role=status]')).not.toBeNull();
 expect(document.querySelector('button')).toBeNull();
});

it('offers retry on a server error',async()=>{
 apiMock.getVideoDelivery.mockRejectedValue(new Error('boom'));
 await mount('');
 expect(apiMock.exchange).not.toHaveBeenCalled();
 expect(document.querySelector('[role=status] button')).not.toBeNull();
});
