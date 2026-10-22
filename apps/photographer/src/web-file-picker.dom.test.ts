// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {pickWebFiles} from './web-file-picker';
afterEach(()=>{vi.restoreAllMocks();document.body.innerHTML='';});
it('keeps the actual input attached during click and removes it after selection',async()=>{
 let input!:HTMLInputElement;
 vi.spyOn(HTMLInputElement.prototype,'click').mockImplementation(function(this:HTMLInputElement){input=this;expect(this.isConnected).toBe(true);});
 const pending=pickWebFiles(),file=new File(['photo'],'real.jpg',{type:'image/jpeg'});
 Object.defineProperty(input,'files',{value:[file]});input.dispatchEvent(new Event('change'));
 expect(await pending).toEqual([file]);expect(input.isConnected).toBe(false);expect(input.onchange).toBeNull();
});
it('cancellation settles empty and removes the live input',async()=>{
 vi.spyOn(HTMLInputElement.prototype,'click').mockImplementation(()=>{});
 const pending=pickWebFiles(),input=document.querySelector('input')!;input.dispatchEvent(new Event('cancel'));
 expect(await pending).toEqual([]);expect(input.isConnected).toBe(false);
});
it('existing import Stop scope abort settles and prevents a retained change from restoring files',async()=>{
 vi.spyOn(HTMLInputElement.prototype,'click').mockImplementation(()=>{});
 const controller=new AbortController(),pending=pickWebFiles(controller.signal),input=document.querySelector('input')!,change=input.onchange;
 controller.abort();await expect(pending).rejects.toMatchObject({name:'AbortError'});expect(input.isConnected).toBe(false);
 change?.call(input,new Event('change'));expect(document.querySelector('input')).toBeNull();
 await expect(pickWebFiles(controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(HTMLInputElement.prototype.click).toHaveBeenCalledTimes(1);
});
it('cleans up when opening the browser chooser throws',async()=>{
 const error=new Error('chooser unavailable');vi.spyOn(HTMLInputElement.prototype,'click').mockImplementation(()=>{throw error});
 await expect(pickWebFiles()).rejects.toBe(error);expect(document.querySelector('input')).toBeNull();
});
