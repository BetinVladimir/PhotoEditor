import {describe,expect,it} from "vitest";
import en from "../../../locales/en/common.json";
import ru from "../../../locales/ru/common.json";
import bg from "../../../locales/bg/common.json";
describe("locale catalogs",()=>it("have exact shared keys and edited translations",()=>{expect(Object.keys(ru)).toEqual(Object.keys(en));expect(Object.keys(bg)).toEqual(Object.keys(en));expect(ru.loading).not.toBe(en.loading);expect(bg.loading).not.toBe(en.loading)}));
