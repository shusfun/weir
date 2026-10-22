import{c as p,r,j as e,B as o,Q as h}from"./index-O-uNVMZb.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const l=p("Copy",[["rect",{width:"14",height:"14",x:"8",y:"8",rx:"2",ry:"2",key:"17jyea"}],["path",{d:"M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2",key:"zix9uf"}]]);function m({value:s,children:u,copiedLabel:n="已复制",icon:x=!0,onCopied:c,onError:a,...y}){const[t,i]=r.useState(!1);return r.useEffect(()=>{if(!t)return;const f=setTimeout(()=>i(!1),2e3);return()=>clearTimeout(f)},[t]),e.jsxs(o,{type:"button",...y,onClick:async()=>{try{await navigator.clipboard.writeText(typeof s=="function"?s():s),i(!0),c==null||c()}catch{i(!1),a==null||a()}},children:[x&&(t?e.jsx(h,{}):e.jsx(l,{})),e.jsx("span",{"aria-live":"polite",children:t?n:u})]})}export{m as C};
