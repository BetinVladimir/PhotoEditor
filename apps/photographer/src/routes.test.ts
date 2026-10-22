import {describe,expect,it} from "vitest";
import {readdirSync} from "node:fs";
import {fileURLToPath} from "node:url";
import {join} from "node:path";
import endpoints from "../../../packages/contracts/endpoints.json";
import legacyEndpoints from "../../../docs/implementation/contracts/endpoints.json";
import v2Endpoints from "../../../packages/contracts/v2-endpoints.json";
import {photographerRoutes,routeCapabilities} from "./routes";

describe("photographer route contract",()=>{
  it("records archive download authority rereads and signed output discovery",()=>{
    const route=routeCapabilities.find(value=>value.route==="/interchange/[id]")!;
    for(const path of ["/api/v1/me","/api/v1/tenants/{t}/interchange-jobs/{id}","/api/v1/tenants/{t}/interchange-jobs/{id}/inspection","/api/v1/tenants/{t}/runs/{id}/outputs"])expect(route.api).toContain(`GET ${path}`);
  });
  it("traces archive upload recovery and multipart actions before import admission",()=>{
    const route=routeCapabilities.find(value=>value.route==="/interchange/upload")!;
    const root="/api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads";
    expect(route.api).toContain("GET /api/v1/me");
    for(const [method,suffix] of [["POST",""],["POST","/attempt-status"],["GET","/{upload}"],["POST","/{upload}/parts"],["POST","/{upload}/complete"],["POST","/{upload}/abort"]])expect(route.api).toContain(`${method} ${root}${suffix}`);
    expect(route.api).not.toContain("POST /api/v1/tenants/{t}/interchange-jobs");
  });
  it("traces prepared interchange review and imported mask adoption child requests",()=>{
    const root="/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}";
    const prepared=routeCapabilities.find(value=>value.route==="/interchange/prepared/[id]")!;
    const masks=routeCapabilities.find(value=>value.route==="/interchange/masks/[id]")!;
    expect(prepared.apiInventory).toBeUndefined();expect(masks.apiInventory).toBeUndefined();
    for(const suffix of ["/prepared-reviews","/prepared-reviews/saved","/person-mask-candidates"])expect(prepared.api).toContain(`GET ${root}${suffix}`);
    expect(prepared.api).toContain(`POST ${root}/prepared-reviews`);
    for(const suffix of ["/person-mask-candidates","/person-mask-candidates/{entry}","/person-mask-candidates/{entry}/previews/{override}/{mask}","/person-mask-confirmations"])expect(masks.api).toContain(`GET ${root}${suffix}`);
    expect(masks.api).toContain(`POST ${root}/person-mask-confirmations`);
    for(const route of [prepared,masks])expect(route.api).toContain("POST /api/v1/tenants/{t}/assets/{id}/media-links");
  });
  it("requires editor authority for imported mask entry and status reads",()=>{
    const root="/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}";
    for(const suffix of ["/person-mask-candidates","/person-mask-confirmations"]){
      expect(v2Endpoints.endpoints.filter(value=>value.method==="GET"&&value.path===root+suffix)).toEqual([expect.objectContaining({role:"editor",success_status:200})]);
    }
  });
  it("records exact imported mask candidate and decision contracts",()=>{
    const root="/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}";
    for(const [method,suffix,request_schema,response_schema] of [["GET","/person-mask-candidates/{entry}",null,"InterchangeMaskCandidatePage"],["POST","/person-mask-confirmations","InterchangeMaskDecision","InterchangeMaskDecisionReceipt"]]){
      expect(v2Endpoints.endpoints.filter(value=>value.method===method&&value.path===root+suffix)).toEqual([expect.objectContaining({role:"editor",success_status:200,request_schema,response_schema})]);
    }
  });
  it("records mask preview as bounded binary PNG rather than JSON",()=>{
    const path="/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/person-mask-candidates/{entry}/previews/{override}/{mask}";
    expect(routeCapabilities.find(value=>value.route==="/interchange/masks/[id]")?.api).toContain(`GET ${path}`);
    expect(v2Endpoints.endpoints.filter(value=>value.path===path)).toEqual([expect.objectContaining({method:"GET",role:"editor",response_schema:"InterchangeMaskPreviewPNG",response_media_type:"image/png",response_max_bytes:8<<20,success_status:200})]);
  });
  it("records epoch-fenced prepared review decisions",()=>{
    const path="/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/prepared-reviews";
    expect(v2Endpoints.endpoints.filter(value=>value.method==="POST"&&value.path===path)).toEqual([expect.objectContaining({role:"editor",success_status:200,request_schema:"InterchangePreparedDecision",response_schema:"InterchangePreparedReceipt"})]);
  });
  it("records bounded saved prepared pages with editor authority",()=>{
    const path="/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/prepared-reviews/saved";
    expect(v2Endpoints.endpoints.filter(value=>value.path===path)).toEqual([expect.objectContaining({method:"GET",role:"editor",response_schema:"InterchangeSavedPreparedPage",success_status:200})]);
  });
  it("records live prepared pages with editor authority",()=>{
    const path="/api/v1/tenants/{t}/projects/{id}/interchange-jobs/{workflow}/prepared-reviews";
    expect(v2Endpoints.endpoints.filter(value=>value.method==="GET"&&value.path===path)).toEqual([expect.objectContaining({role:"editor",response_schema:"InterchangePreparedPage",success_status:200})]);
  });
  it("records editor-only interchange snapshot preparation",()=>{
    const path="/api/v1/tenants/{t}/projects/{id}/interchange-export-snapshots";
    expect(routeCapabilities.find(value=>value.route==="/interchange/prepare")?.api).toContain(`POST ${path}`);
    expect(v2Endpoints.endpoints.filter(value=>value.path===path)).toEqual([expect.objectContaining({method:"POST",role:"editor",success_status:201})]);
  });
  it("records signing policy and admission routes",()=>{
    const route=routeCapabilities.find(value=>value.route==="/exports/sign")!;
    expect(route.api).toContain("GET /api/v1/tenants/{t}/exports/{id}/signing-policies");
    expect(route.api).toContain("POST /api/v1/tenants/{t}/exports/{id}/provenance");
  });
  it("distinguishes verified local screens from untraced empty API inventories",()=>{
    for(const route of routeCapabilities.filter(value=>value.api.length===0)){
      expect(route.apiInventory?.reason.length).toBeGreaterThan(20);
      expect(route.apiInventory?.kind).toBe("local");
    }
    for(const route of ["/interchange/format","/interchange/import-format"]){
      expect(routeCapabilities.find(value=>value.route===route)?.apiInventory?.kind).toBe("local");
    }
  });
  it("traces both multi operations through proposal, preview, quality and Apply",()=>{
    const route=routeCapabilities.find(value=>value.route==="/multi-replacements/[original]")!;
    for(const operation of ["panorama","focus-stack"])for(const [method,suffix,role] of [["GET","","editor"],["GET","/previews/{variant}","editor"],["GET","/quality","editor"],["POST","/apply","editor"]]){
      const path=`/api/v1/tenants/{t}/projects/{id}/${operation}-proposals/{task}${suffix}`;
      expect(route.api).toContain(`${method} ${path}`);expect(v2Endpoints.endpoints.filter(value=>value.method===method&&value.path===path)).toEqual([expect.objectContaining({role,success_status:200})]);
    }
  });
  it("traces perspective replacement through proposal, preview, quality and Apply",()=>{
    const root="/api/v1/tenants/{t}/projects/{id}/versions/{version}/perspective-proposals/{task}";
    const route=routeCapabilities.find(value=>value.route==="/perspective-replacements/[original]")!;
    for(const [method,suffix,role] of [["GET","","editor"],["GET","/previews/{variant}","editor"],["GET","/quality","editor"],["POST","/apply","editor"]]){
      expect(route.api).toContain(`${method} ${root}${suffix}`);
      expect(v2Endpoints.endpoints.filter(value=>value.method===method&&value.path===root+suffix)).toEqual([expect.objectContaining({role,success_status:200})]);
    }
  });
  it("includes authoritative scope and workflow reads for replacement review screens",()=>{
    for(const [route,kind] of [["/multi-replacements/[original]","multi"],["/perspective-replacements/[original]","perspective"]] as const){
      const entry=routeCapabilities.find(value=>value.route===route);
      expect(entry?.api).toContain(`GET /api/v1/tenants/{t}/projects/{id}/reviews/{review}/${kind}-replacements/{original}`);
      expect(entry?.api).toContain("GET /api/v1/tenants/{t}/v2-workflows/{id}");
    }
  });
  it("declares delivery run discovery, slot listing and download admission with exact roles",()=>{
    const expected=[
      ["GET","/api/v1/tenants/{t}/delivery-packages","viewer",200],
      ["GET","/api/v1/tenants/{t}/delivery-packages/by-workflow/{workflow}","viewer",200],
      ["GET","/api/v1/tenants/{t}/delivery-packages/{id}/inspection","editor",200],
      ["GET","/api/v1/tenants/{t}/delivery-packages/by-run/{run}","viewer",200],
      ["GET","/api/v1/tenants/{t}/delivery-packages/{id}/items","viewer",200],
      ["POST","/api/v1/tenants/{t}/delivery-packages/{id}/download-intent","editor",201],
    ] as const;
    for(const [method,path,role,status] of expected){
      const rows=v2Endpoints.endpoints.filter(row=>row.method===method&&row.path===path);
      expect(rows).toHaveLength(1);expect(rows[0]).toMatchObject({role,success_status:status});
    }
  });

  it("traces export identity recovery and download lifecycle",()=>{
    const api=routeCapabilities.find(value=>value.route==="/exports")!.api;
    for(const endpoint of ["GET /api/v1/me","GET /api/v1/tenants/{t}/download-jobs/{id}","POST /api/v1/tenants/{t}/download-jobs/{id}/download-intent","POST /api/v1/tenants/{t}/download-jobs/{id}/cancel","POST /api/v1/downloads/redeem"]){
      expect(api).toContain(endpoint);
    }
  });
  it("tracks every Expo screen, including new nested routes",()=>{
    const app=fileURLToPath(new URL("../screens/",import.meta.url).href);
    const screens=(directory:string,prefix=""):string[]=>readdirSync(directory,{withFileTypes:true}).flatMap(entry=>{
      if(entry.isDirectory())return screens(join(directory,entry.name),`${prefix}/${entry.name}`);
      if(!entry.isFile()||!entry.name.endsWith(".tsx")||entry.name.startsWith("_"))return [];
      const stem=entry.name.slice(0,-4);
      return [stem==="index"?(prefix||"/"):`${prefix}/${stem}`];
    });
    const actual=screens(app).sort();
    expect([...photographerRoutes].sort()).toEqual(actual);
    expect(routeCapabilities.map(value=>value.route).sort()).toEqual(actual);
    expect(new Set(photographerRoutes).size).toBe(photographerRoutes.length);
  });
  it("covers every required route on web and native",()=>{
    expect(routeCapabilities.map(x=>x.route)).toEqual(photographerRoutes);
    expect(routeCapabilities.every(x=>x.web&&x.native)).toBe(true);
  });
  it("binds only declared endpoint contracts",()=>{
    const known=new Set([...legacyEndpoints.endpoints,...endpoints.endpoints,...v2Endpoints.endpoints].map(x=>`${x.method} ${x.path}`));
    for(const route of routeCapabilities)for(const api of [...route.api,...(route.authorityAPI??[])])
      expect(known.has(api),`${route.route}: ${api}`).toBe(true);
  });
  it("keeps decided render selection and selected preview in the review screen inventory",()=>{
    const review=routeCapabilities.find(value=>value.route==="/reviews/[id]");
    expect(review?.api).toContain("GET /api/v1/tenants/{t}/reviews/{id}/render-result-selection");
    expect(review?.api).toContain("GET /api/v1/tenants/{t}/reviews/{id}/selected-preview/{variant}");
  });
});

it('inventories multi-frame admission, result review and collection replacement',()=>{
 const route=routeCapabilities.find(value=>value.route==='/collections/[id]/multi')!;
 expect(route.api).toContain('GET /api/v1/tenants/{t}/capabilities');
 expect(route.api).toContain('POST /api/v1/tenants/{t}/projects/{id}/render-input-snapshots');
 for(const kind of ['panorama','focus-stack']){
  const root=`/api/v1/tenants/{t}/projects/{id}/${kind}`;
  expect(route.api).toContain(`GET ${root}-capabilities`);expect(route.api).toContain(`POST ${root}-intents`);
  for(const suffix of ['', '/previews/{variant}', '/quality'])expect(route.api).toContain(`GET ${root}-proposals/{task}${suffix}`);
  expect(route.api).toContain(`POST ${root}-proposals/{task}/apply`);
 }
 expect(route.api).toContain('PUT /api/v1/tenants/{t}/collections/{id}/items/{item}/version');
});

it('declares current multi submission contracts and editor authority',()=>{
 for(const [stem,name] of [['panorama','Panorama'],['focus-stack','FocusStack']]){
  const root=`/api/v1/tenants/{t}/projects/{id}/${stem}`;
  expect(v2Endpoints.endpoints.filter(e=>e.method==='GET'&&e.path===`${root}-capabilities`)).toEqual([expect.objectContaining({role:'editor',response_schema:`${name}Capabilities`,success_status:200})]);
  expect(v2Endpoints.endpoints.filter(e=>e.method==='POST'&&e.path===`${root}-intents`)).toEqual([expect.objectContaining({role:'editor',request_schema:`${name}Intent`,response_schema:'JobTicket',success_status:202})]);
 }
});

it('declares editor-scoped version reconciliation and CAS contracts',()=>{
 const path='/api/v1/tenants/{t}/collections/{id}/items/{item}/version';
 expect(v2Endpoints.endpoints.filter(e=>e.path===path&&e.method==='GET')).toEqual([expect.objectContaining({role:'editor',request_schema:null,response_schema:'CollectionItemVersionState',success_status:200})]);
 expect(v2Endpoints.endpoints.filter(e=>e.path===path&&e.method==='PUT')).toEqual([expect.objectContaining({role:'editor',request_schema:'CollectionItemVersionReplace',response_schema:'CollectionVersionItem',success_status:200})]);
});

it('records delivery creation signing choice discovery',()=>{
 const path='/api/v1/tenants/{t}/projects/{project}/delivery-signing-policies';
 expect(routeCapabilities.find(e=>e.route==='/deliveries/new')?.api).toContain(`GET ${path}`);
 expect(v2Endpoints.endpoints.filter(e=>e.path===path)).toEqual([expect.objectContaining({method:'GET',role:'viewer',request_schema:null,response_schema:'DeliverySigningPolicyPage',success_status:200})]);
});


it("includes account authority read before opening durable delivery publication",()=>{
  expect(routeCapabilities.find(value=>value.route==="/deliveries/[id]")?.api).toContain("GET /api/v1/me");
});

it('records shared runtime authorization separately from screen body APIs',()=>{
 const globals=new Set(['/','/login','/register','/workspaces','/settings/profile']);
 for(const entry of routeCapabilities){
  if(globals.has(entry.route)){expect(entry.authorityAPI).toEqual([]);continue;}
  expect(entry.authorityAPI).toContain('GET /api/v1/me');
  expect(entry.authorityAPI).toContain('GET /api/v1/tenants/{t}');
  expect(entry.authorityAPI).toContain('GET /api/v1/tenants/{t}/projects/{id}');
 }
 expect(routeCapabilities.find(row=>row.route==='/editor/[version]')?.authorityAPI).toContain('GET /api/v1/tenants/{t}/assets/{id}');
});
it('records actual editor V2, masks, generation, portrait and render delegates',()=>{
 const api=routeCapabilities.find(row=>row.route==='/editor/[version]')!.api;
 for(const path of [
  'GET /api/v1/tenants/{t}/assets/{id}/v2-draft',
  'PUT /api/v1/tenants/{t}/assets/{id}/v2-draft',
  'POST /api/v1/tenants/{t}/assets/{id}/v2-draft/freeze',
  'POST /api/v1/tenants/{t}/masks',
  'POST /api/v1/tenants/{t}/generations',
  'POST /api/v1/tenants/{t}/generations/abandon',
  'POST /api/v1/tenants/{t}/projects/{id}/local-portrait-intents',
  'POST /api/v1/tenants/{t}/projects/{id}/render-batches',
  'POST /api/v1/tenants/{t}/projects/{id}/editor-effects',
 ])expect(api).toContain(path);
});

it('records mounted review/export/upload delegates instead of only their route shells',()=>{
 const body=(route:string)=>routeCapabilities.find(entry=>entry.route===route)?.api??[];
 expect(body('/reviews/[id]')).toEqual(expect.arrayContaining(['GET /api/v1/tenants/{t}/reviews/{id}/restoration-result-selection','GET /api/v1/tenants/{t}/reviews/{id}/restoration-selected-preview/{variant}','GET /api/v1/tenants/{t}/reviews/{id}/culling-items','POST /api/v1/tenants/{t}/reviews/{id}/culling-decision','GET /api/v1/tenants/{t}/reviews/{id}/relight-collection-targets','POST /api/v1/tenants/{t}/reviews/{id}/relight-viewed']));
 expect(body('/exports')).toEqual(expect.arrayContaining(['GET /api/v1/tenants/{t}/assets','PATCH /api/v1/tenants/{t}/export-presets/{preset}/metadata-policy','POST /api/v1/tenants/{t}/exports','GET /api/v1/tenants/{t}/exports/batches/{id}']));
 expect(body('/imports/new')).toContain('GET /api/v1/tenants/{t}/projects/{id}/raw-capabilities');
});

it('records Training actual style/project reads and paged acknowledged set recovery',()=>{
 const api=routeCapabilities.find(entry=>entry.route==='/styles/[id]/training')!.api;
 expect(api).toEqual(expect.arrayContaining(['GET /api/v1/me','GET /api/v1/tenants/{t}/styles/{id}','GET /api/v1/tenants/{t}/projects/{id}','GET /api/v1/tenants/{t}/training-sets/{id}','GET /api/v1/tenants/{t}/styles/{id}/analysis-capabilities']));
 expect(v2Endpoints.endpoints.find(entry=>entry.method==='GET'&&entry.path==='/api/v1/tenants/{t}/training-sets/{id}')).toMatchObject({role:'editor',response_schema:'TrainingSet'});
});


it('includes conditional bootstrap and nested recovery transports in their mounted screen',()=>{
 const required={
  '/login':['GET /api/v1/auth/csrf'],
  '/workspaces':['GET /api/v1/me','GET /api/v1/tenants/{t}'],
  '/imports/new':['GET /api/v1/me','GET /api/v1/tenants/{t}/uploads/{id}','POST /api/v1/tenants/{t}/uploads/{id}/abort'],
  '/collections/[id]/lens':['GET /api/v1/tenants/{t}/assets/{id}/v2-binding'],
  '/semantic/[id]':['GET /api/v1/me','GET /api/v1/tenants/{t}/capabilities'],
  '/runs/[id]':['GET /api/v1/tenants/{t}/v2-workflows/{id}','GET /api/v1/tenants/{t}/runs/{id}/semantic-intent'],
 } as const;
 for(const [route,apis] of Object.entries(required))expect(routeCapabilities.find(row=>row.route===route)?.api,route).toEqual(expect.arrayContaining([...apis]));
});

it('has no pending API inventory or duplicate body edges on any current screen',()=>{
 for(const row of routeCapabilities){
  expect(row.apiInventory?.kind,row.route).not.toBe('pending');
  expect(new Set(row.api).size,row.route).toBe(row.api.length);
 }
});


it('includes delegated Lens replacement and Run multi-image review instead of discovery alone',()=>{
 const lens=routeCapabilities.find(row=>row.route==='/collections/[id]/lens')!.api;
 for(const method of ['GET','PUT'])expect(lens).toContain(`${method} /api/v1/tenants/{t}/collections/{id}/items/{item}/version`);
 const run=routeCapabilities.find(row=>row.route==='/runs/[id]')!.api;
 for(const kind of ['panorama','focus-stack'])for(const [method,suffix] of [['GET',''],['GET','/previews/{variant}'],['GET','/quality'],['POST','/apply']])expect(run).toContain(`${method} /api/v1/tenants/{t}/projects/{id}/${kind}-proposals/{task}${suffix}`);
 expect(routeCapabilities.find(row=>row.route==='/semantic/[id]')!.api).toContain('GET /api/v1/tenants/{t}/runs/{id}');
 expect(routeCapabilities.find(row=>row.route==='/imports/new')!.api).toContain('GET /api/v1/tenants/{t}/collections/{id}');
});

it('register uses the existing OTP transports and has no tenant authority reads',()=>{
 const login=routeCapabilities.find(row=>row.route==='/login')!;
 const register=routeCapabilities.find(row=>row.route==='/register')!;
 expect(register.api).toEqual(login.api);expect(register.authorityAPI).toEqual([]);
});
