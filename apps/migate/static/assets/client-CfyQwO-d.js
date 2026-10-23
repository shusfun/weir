import{c as i,j as r,ah as c,a as t}from"./index-CR--XSVU.js";import{S as l}from"./smartphone-DqmsdtQq.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const u=i("Download",[["path",{d:"M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4",key:"ih7n3h"}],["polyline",{points:"7 10 12 15 17 10",key:"2ggqvy"}],["line",{x1:"12",x2:"12",y1:"15",y2:"3",key:"1vk2je"}]]);/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const g=i("Globe",[["circle",{cx:"12",cy:"12",r:"10",key:"1mglay"}],["path",{d:"M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20",key:"13o1zl"}],["path",{d:"M2 12h20",key:"9i4pu4"}]]);/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const h=i("Shield",[["path",{d:"M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z",key:"oel41y"}]]);function y({icon:s,data:e}){if(e!=null&&e.startsWith("data:image/png;base64,")||e!=null&&e.startsWith("data:image/jpeg;base64,"))return r.jsx("img",{src:e,alt:"",width:28,height:28,className:"catalog-uploaded-icon"});const a=s==="phone"?l:s==="shield"?h:s==="globe"?g:c;return r.jsx(a,{size:22})}const d={catalog:(s=!1)=>t(s?"/admin/clients":"/clients"),sourceUsage:()=>t("/admin/sources/usage"),users:(s="")=>t("/admin/users"+s),devices:()=>t("/admin/devices"),grants:()=>t("/admin/grants"),tasks:(s="")=>t("/admin/tasks"+s),resources:async s=>{const e=await t("/admin/resources/"+s+"?page=1&page_size=500");if((e.total??e.items.length)>5e4)throw Error("库存超过当前选择器加载上限，请缩小范围");const a=[...e.items];for(let o=2;a.length<(e.total??0);o++){const n=await t("/admin/resources/"+s+"?page="+o+"&page_size=500");if(!n.items.length)throw Error("资源列表正在变化，请刷新后重试");a.push(...n.items)}return{...e,items:a}}};export{y as C,u as D,d as c};
