import{c as r,j as t,aA as i,B as c,b4 as d,N as u}from"./index-C7NjAv9u.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const o=r("RefreshCw",[["path",{d:"M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8",key:"v9h5vc"}],["path",{d:"M21 3v5h-5",key:"1q7to0"}],["path",{d:"M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16",key:"3uifl3"}],["path",{d:"M8 16H3v5",key:"1cv678"}]]);function h({loading:s,error:e,empty:l,onRetry:n,detail:a}){return s?t.jsx(i,{kind:"loading",title:"正在加载数据",detail:"请稍候，正在获取最新状态。"}):e?t.jsx(i,{kind:"error",title:"暂时无法加载",detail:a||e instanceof Error&&e.message||"请检查连接后重试。现有数据不会被更改。",action:n&&t.jsxs(c,{variant:"outline",onClick:n,children:[t.jsx(o,{}),"重新加载"]})}):t.jsx(i,{kind:"empty",title:l??"暂无记录",detail:a})}function j({error:s,onReload:e}){return d(s)?t.jsx(u,{tone:"bad",children:t.jsxs("div",{className:"actions",style:{justifyContent:"space-between"},children:[t.jsx("span",{children:s.message}),t.jsxs(c,{size:"sm",variant:"outline",onClick:e,children:[t.jsx(o,{}),"重新读取最新版本"]})]})}):null}export{j as C,o as R,h as S};
