import {describe,expect,it} from "vitest";
import endpoints from "../../../packages/contracts/endpoints.json";
import v2Endpoints from "../../../packages/contracts/v2-endpoints.json";
import {photographerRoutes,routeCapabilities} from "./routes";

describe("photographer route contract",()=>{
  it("covers every required route on web and native",()=>{
    expect(routeCapabilities.map(x=>x.route)).toEqual(photographerRoutes);
    expect(routeCapabilities.every(x=>x.web&&x.native)).toBe(true);
  });
  it("binds only declared endpoint contracts",()=>{
    const known=new Set([...endpoints.endpoints,...v2Endpoints.endpoints].map(x=>`${x.method} ${x.path}`));
    for(const route of routeCapabilities)for(const api of route.api)
      expect(known.has(api),`${route.route}: ${api}`).toBe(true);
  });
  it("keeps decided render selection and selected preview in the review screen inventory",()=>{
    const review=routeCapabilities.find(value=>value.route==="/reviews/[id]");
    expect(review?.api).toContain("GET /api/v1/tenants/{t}/reviews/{id}/render-result-selection");
    expect(review?.api).toContain("GET /api/v1/tenants/{t}/reviews/{id}/selected-preview/{variant}");
  });
});
