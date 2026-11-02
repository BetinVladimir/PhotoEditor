import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {readBillingCatalog,readCatalogMoney} from './billing-catalog-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const money=()=>({currency:'EUR',net_minor:'1000',tax_minor:'200',gross_minor:'1200',tax_mode:'test_bg20',tax_lines:[{code:'fixture_vat',rate_decimal:'0.20',amount_minor:'200'}]});
const plan=()=>({id:id(1),version_id:id(2),code:'start',label:'Start',price:money(),limits:[{resource_code:'standard_photo',quantity:'100',unit:'credit'}],can_purchase:false,unavailable_reason:'sales_disabled'});
const catalog=()=>({catalog_revision:'1',scope:'sandbox',currency:'EUR',plans:[plan()]});
it('validates integer money conservation and owns nested catalog data',()=>{
 const source=catalog(),decoded=readBillingCatalog(source);source.plans[0].limits[0].quantity='900';source.plans[0].price.tax_lines[0].amount_minor='900';expect(decoded.plans[0].limits[0].quantity).toBe('100');expect(decoded.plans[0].price.tax_lines[0].amount_minor).toBe('200');
 for(const changed of [{gross_minor:'1201'},{net_minor:1000},{net_minor:'9223372036854775808'},{tax_lines:[]},{currency:'USD'}])expect(()=>readCatalogMoney({...money(),...changed})).toThrow();
});
it('rejects leaked Custom, duplicate plans and unqualified Studio Pro purchases',()=>{
 const duplicate=catalog();duplicate.plans.push({...plan()});expect(()=>readBillingCatalog(duplicate)).toThrow();
 for(const changed of [{code:'custom'},{id:id(0),private_contact:'private'},{can_purchase:true,unavailable_reason:null,limits:[]},{code:'studio_pro',can_purchase:true,unavailable_reason:null}])expect(()=>readBillingCatalog({...catalog(),plans:[{...plan(),...changed}]})).toThrow();
});
it('calls the public catalog with locale and EUR; other billing reads use registered paths',async()=>{
 const request=vi.fn().mockResolvedValue(catalog()),api=new OwnerAPI({request} as unknown as ApiClient,id(4));await api.billingCatalog(undefined,'bg');expect(request.mock.calls[0][0]).toBe('/api/v1/billing/catalog?locale=bg&currency=EUR');
 request.mockResolvedValue({});await api.billingSummary();expect(request.mock.calls[1][0]).toBe(`/api/v1/tenants/${id(4)}/billing`);
 await api.billingAddonOffers();expect(request.mock.calls[2][0]).toBe(`/api/v1/tenants/${id(4)}/billing/offers`);
});
it('does not send cancelled catalog reads or unsupported locale',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as unknown as ApiClient,id(4)),controller=new AbortController();controller.abort();await expect(api.billingCatalog(controller.signal)).rejects.toThrow();await expect(api.billingCatalog(undefined,'invalid' as any)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
