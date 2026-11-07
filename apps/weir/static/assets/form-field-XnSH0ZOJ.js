import{y as j,j as e,F as m,T as f,af as w,I as y}from"./index-DUN_GWjj.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const F=j("LockKeyhole",[["circle",{cx:"12",cy:"16",r:"1",key:"1au0dj"}],["rect",{x:"3",y:"10",width:"18",height:"12",rx:"2",key:"6s8ecr"}],["path",{d:"M7 10V7a5 5 0 0 1 10 0v3",key:"1pqi11"}]]);function I({label:r,name:t,field:c,type:a="text",value:n,onChange:x,required:i=!1,autoComplete:o,hint:d,customError:l,placeholder:u,className:h}){const s={id:t,value:n??"",required:i,placeholder:u,onChange:p=>x(p.target.value)};return e.jsx(m,{label:r,htmlFor:t,name:c,hint:d,customError:l,className:h,children:a==="textarea"?e.jsx(f,{...s,rows:5,autoComplete:"off"}):a==="password"?e.jsx(w,{...s,autoComplete:o??"new-password"}):e.jsx(y,{...s,type:a,autoComplete:o})})}export{F as L,I as T};
