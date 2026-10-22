// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {DeliveryRunLink} from './delivery-run-link';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
afterEach(cleanup);

it('opens a package only after the run/workflow binding and scoped package read',async()=>{
 const get=vi.fn(async()=>({run_id:id(1),workflow_id:id(2)}));
 const deliveryPackageForWorkflow=vi.fn(async()=>({id:id(4)}));
 const onOpen=vi.fn();
 render(<DeliveryRunLink api={{get,deliveryPackageForWorkflow} as unknown as OwnerAPI} run={id(1)} workflow={id(2)} project={id(3)} onOpen={onOpen}/>);
 fireEvent.click(await screen.findByText('Delivery package'));
 expect(onOpen).toHaveBeenCalledWith(id(4));
 expect(deliveryPackageForWorkflow).toHaveBeenCalledWith(id(3),id(2),expect.any(AbortSignal));
});
it('refuses a foreign run before requesting a package',async()=>{
 const get=vi.fn(async()=>({run_id:id(8),workflow_id:id(2)}));
 const deliveryPackageForWorkflow=vi.fn();
 render(<DeliveryRunLink api={{get,deliveryPackageForWorkflow} as unknown as OwnerAPI} run={id(1)} workflow={id(2)} project={id(3)} onOpen={vi.fn()}/>);
 await waitFor(()=>expect(screen.getByText('Refresh')).toBeTruthy());
 expect(deliveryPackageForWorkflow).not.toHaveBeenCalled();
 expect(screen.queryByText('Delivery package')).toBeNull();
});

it('stops a pending workflow read and retries without adopting the late old result',async()=>{
 let old!:(value:unknown)=>void;
 const get=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{old=resolve})).mockResolvedValue({run_id:id(1),workflow_id:id(2)});
 const deliveryPackageForWorkflow=vi.fn().mockResolvedValue({id:id(4)}),onOpen=vi.fn();
 render(<DeliveryRunLink api={{get,deliveryPackageForWorkflow} as unknown as OwnerAPI} run={id(1)} workflow={id(2)} project={id(3)} onOpen={onOpen}/>);
 fireEvent.click(screen.getByText('Stop loading'));await screen.findByText('Refresh');
 expect((get.mock.calls[0][1] as AbortSignal).aborted).toBe(true);
 fireEvent.click(screen.getByText('Refresh'));fireEvent.click(await screen.findByText('Delivery package'));
 await act(async()=>old({run_id:id(1),workflow_id:id(2)}));
 expect(deliveryPackageForWorkflow).toHaveBeenCalledOnce();expect(onOpen).toHaveBeenCalledWith(id(4));
});
it('bounds both reads by one deadline and refuses a package arriving after expiry',async()=>{
 vi.useFakeTimers();let complete!:(value:unknown)=>void;
 const get=vi.fn(async()=>({run_id:id(1),workflow_id:id(2)}));
 const deliveryPackageForWorkflow=vi.fn().mockImplementation(()=>new Promise(resolve=>{complete=resolve}));
 try{
  render(<DeliveryRunLink api={{get,deliveryPackageForWorkflow} as unknown as OwnerAPI} run={id(1)} workflow={id(2)} project={id(3)} onOpen={vi.fn()}/>);
  await act(async()=>{await vi.advanceTimersByTimeAsync(7999)});expect(screen.getByText('Stop loading')).toBeTruthy();
  await act(async()=>{await vi.advanceTimersByTimeAsync(1)});expect(screen.getByText('Refresh')).toBeTruthy();
  expect((deliveryPackageForWorkflow.mock.calls[0][2] as AbortSignal).aborted).toBe(true);
  await act(async()=>complete({id:id(4)}));expect(screen.queryByText('Delivery package')).toBeNull();
 }finally{vi.useRealTimers()}
});
