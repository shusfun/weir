import{c as r,j as e,p as k,q as j,P as m,I as w}from"./index-11RbxUgD.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const v=r("KeyRound",[["path",{d:"M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z",key:"1s6t7t"}],["circle",{cx:"16.5",cy:"7.5",r:".5",fill:"currentColor",key:"w0ekpg"}]]);/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const K=r("LockKeyhole",[["circle",{cx:"12",cy:"16",r:"1",key:"1au0dj"}],["rect",{x:"3",y:"10",width:"18",height:"12",rx:"2",key:"6s8ecr"}],["path",{d:"M7 10V7a5 5 0 0 1 10 0v3",key:"1pqi11"}]]);function g({label:c,name:o,field:n,type:a="text",value:l,onChange:x,required:d=!1,autoComplete:s,hint:i,customError:h,placeholder:u,className:p}){const t={id:o,value:l??"",required:d,placeholder:u,onChange:y=>x(y.target.value)};return e.jsx(k,{label:c,htmlFor:o,name:n,hint:i,customError:h,className:p,children:a==="textarea"?e.jsx(j,{...t,rows:5,autoComplete:"off"}):a==="password"?e.jsx(m,{...t,autoComplete:s??"new-password"}):e.jsx(w,{...t,type:a,autoComplete:s})})}export{v as K,K as L,g as T};
