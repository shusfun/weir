var Iu=Object.defineProperty;var Os=E=>{throw TypeError(E)};var Lu=(E,k,R)=>k in E?Iu(E,k,{enumerable:!0,configurable:!0,writable:!0,value:R}):E[k]=R;var N=(E,k,R)=>Lu(E,typeof k!="symbol"?k+"":k,R),Ri=(E,k,R)=>k.has(E)||Os("Cannot "+R);var d=(E,k,R)=>(Ri(E,k,"read from private field"),R?R.call(E):k.get(E)),j=(E,k,R)=>k.has(E)?Os("Cannot add the same private member more than once"):k instanceof WeakSet?k.add(E):k.set(E,R),L=(E,k,R,Z)=>(Ri(E,k,"write to private field"),Z?Z.call(E,R):k.set(E,R),R),q=(E,k,R)=>(Ri(E,k,"access private method"),R);import{c as ie,t as T,T as Jn,B as In,m as _r,cX as Ni,k as Ou,q as Nu,l as Mu,cW as ju,dk as Du,dl as Fu,C as Uu,G as Hu,N as Vu,X as Pi,O as Qr,au as Ii,c0 as zu,a6 as Bu}from"./index-BfXOB-A4.js";import{a as Ku}from"./password-CE9wKiXr.js";import{c as qu}from"./passkey-Dm4YLdl6.js";import{A as Gu}from"./arrow-right-C_LuS_GN.js";import{A as Ns}from"./arrow-left-ZUGZLdGI.js";var Ms={},js;function Wu(){var gs,dr,$n,Bn,hr,vr,pr,dn,gr,ut,Hr,hn,Mt,Jt,mr,Kn,oe,Mi,ta,ji,Us,Hs,xr,Yu,na,ms,bs,bt,Vr,St,qn,nt,wt,rt,yt,Zt,Gn,Rn,br,zr,Br,vn,xa,se,Vs,zs,Di,Bs,Fi,ra,$a,Ui,Hi,jt,pn,Dt,Yn,Kr,ka,wi,gn,Ct;if(js)return Ms;js=1;const E=!1;var k=Array.isArray,R=Array.prototype.indexOf,Z=Array.prototype.includes,$e=Array.from,Me=Object.keys,P=Object.defineProperty,V=Object.getOwnPropertyDescriptor,ot=Object.getOwnPropertyDescriptors,Re=Object.prototype,ke=Array.prototype,he=Object.getPrototypeOf,le=Object.isExtensible;const Pe=()=>{};function G(e){for(var t=0;t<e.length;t++)e[t]()}function je(){var e,t,n=new Promise((r,a)=>{e=r,t=a});return{promise:n,resolve:e,reject:t}}const ve=2,ze=4,Be=8,nn=1<<24,ce=16,be=32,Ae=64,ht=128,vt=256,Ie=512,we=1024,pe=2048,Te=4096,Qe=8192,Le=16384,rn=32768,Ln=1<<25,pt=65536,Zn=1<<17,kr=1<<18,an=1<<19,Qn=1<<20,Ut=65536,On=1<<21,on=1<<22,_t=1<<23,bn=Symbol("$state"),Nn=Symbol("component"),aa=Symbol("legacy props"),Pa=Symbol(""),er=Symbol("attributes"),tr=Symbol("class"),nr=Symbol("style"),wn=Symbol("text"),yn=Symbol("form reset"),_n=new class extends Error{constructor(){super(...arguments);N(this,"name","StaleReactionError");N(this,"message","The reaction that called `getAbortSignal()` was re-run or destroyed")}},xn=!!((gs=globalThis.document)!=null&&gs.contentType)&&globalThis.document.contentType.includes("xml"),kn=3,En=8;function Er(e){return e===this.v}function Sr(e,t){return e!=e?t==t:e!==t||e!==null&&typeof e=="object"||typeof e=="function"}function z(e){return!Sr(e,this.v)}function xt(e){throw new Error("https://svelte.dev/e/lifecycle_outside_component")}function Pt(){throw new Error("https://svelte.dev/e/async_derived_orphan")}function It(e){throw new Error("https://svelte.dev/e/effect_in_teardown")}function Sn(){throw new Error("https://svelte.dev/e/effect_in_unowned_derived")}function rr(e){throw new Error("https://svelte.dev/e/effect_orphan")}function sn(){throw new Error("https://svelte.dev/e/effect_update_depth_exceeded")}function Cr(){throw new Error("https://svelte.dev/e/hydration_failed")}function Ks(){throw new Error("https://svelte.dev/e/state_descriptors_fixed")}function qs(){throw new Error("https://svelte.dev/e/state_prototype_fixed")}function Gs(){throw new Error("https://svelte.dev/e/state_unsafe_mutation")}function Ys(){throw new Error("https://svelte.dev/e/svelte_boundary_reset_onerror")}let Ws=!1;const Xs=1,Js=2,Ia="[",Vi="[!",zi="[?",Bi="]",Mn={},Oe=Symbol("uninitialized"),Ki="http://www.w3.org/1999/xhtml",Zs="http://www.w3.org/2000/svg",Qs="http://www.w3.org/1998/Math/MathML",el="@attach";let Ye=null;function ar(e){Ye=e}function ln(e,t=!1,n){Ye={p:Ye,i:!1,c:null,e:null,s:e,x:null,r:U,l:null}}function cn(e){var t=Ye,n=t.e;if(n!==null){t.e=null;for(var r of n)$o(r)}return e!==void 0&&(t.x=e),t.i=!0,Ye=t.p,La(e)}function La(e={}){return P(e,Nn,{value:!0}),e}function qi(){return!0}let jn=[];function Gi(){var e=jn;jn=[],G(e)}function Ht(e){if(jn.length===0&&!Pr){var t=jn;queueMicrotask(()=>{t===jn&&Gi()})}jn.push(e)}function tl(){for(;jn.length>0;)Gi()}function nl(){console.warn("https://svelte.dev/e/derived_inert")}function Ar(e){console.warn("https://svelte.dev/e/hydration_mismatch")}function rl(){console.warn("https://svelte.dev/e/select_multiple_invalid_value")}function al(){console.warn("https://svelte.dev/e/svelte_boundary_reset_noop")}let F=!1;function Vt(e){F=e}let B;function et(e){if(e===null)throw Ar(),Mn;return B=e}function Dn(){return et(Bt(B))}function Ne(e){if(F){if(Bt(B)!==null)throw Ar(),Mn;B=e}}function Oa(e=1){if(F){for(var t=e,n=B;t--;)n=Bt(n);B=n}}function Na(e=!0){for(var t=0,n=B;;){if(n.nodeType===En){var r=n.data;if(r===Bi){if(t===0)return n;t-=1}else(r===Ia||r===Vi||r[0]==="["&&!isNaN(Number(r.slice(1))))&&(t+=1)}var a=Bt(n);e&&n.remove(),n=a}}function Yi(e){if(!e||e.nodeType!==En)throw Ar(),Mn;return e.data}function un(e){if(typeof e!="object"||e===null||bn in e||Nn in e)return e;const t=he(e);if(t!==Re&&t!==ke)return e;var n=new Map,r=k(e),a=W(0),o=Hn,l=s=>{if(Hn===o)return s();var f=Y,u=Hn;kt(null),xo(o);var h=s();return kt(f),xo(u),h};return r&&n.set("length",W(e.length)),new Proxy(e,{defineProperty(s,f,u){(!("value"in u)||u.configurable===!1||u.enumerable===!1||u.writable===!1)&&Ks();var h=n.get(f);return h===void 0?l(()=>{var m=W(u.value);return n.set(f,m),m}):C(h,u.value,!0),!0},deleteProperty(s,f){var u=n.get(f);if(u===void 0){if(f in s){const h=l(()=>W(Oe));n.set(f,h),Lr(a)}}else C(u,Oe),Lr(a);return!0},get(s,f,u){var p;if(f===bn)return e;var h=n.get(f),m=f in s;if(h===void 0&&(!m||(p=V(s,f))!=null&&p.writable)&&(h=l(()=>{var b=un(m?s[f]:Oe),w=W(b);return w}),n.set(f,h)),h!==void 0){var g=i(h);return g===Oe?void 0:g}return Reflect.get(s,f,u)},getOwnPropertyDescriptor(s,f){var u=Reflect.getOwnPropertyDescriptor(s,f);if(u&&"value"in u){var h=n.get(f);h&&(u.value=i(h))}else if(u===void 0){var m=n.get(f),g=m==null?void 0:m.v;if(m!==void 0&&g!==Oe)return{enumerable:!0,configurable:!0,value:g,writable:!0}}return u},has(s,f){var g;if(f===bn)return!0;var u=n.get(f),h=u!==void 0&&u.v!==Oe||Reflect.has(s,f);if(u!==void 0||U!==null&&(!h||(g=V(s,f))!=null&&g.writable)){u===void 0&&(u=l(()=>{var p=h?un(s[f]):Oe,b=W(p);return b}),n.set(f,u));var m=i(u);if(m===Oe)return!1}return h},set(s,f,u,h){var Se;var m=n.get(f),g=f in s;if(r&&f==="length")for(var p=u;p<m.v;p+=1){var b=n.get(p+"");b!==void 0?C(b,Oe):p in s&&(b=l(()=>W(Oe)),n.set(p+"",b))}if(m===void 0)(!g||(Se=V(s,f))!=null&&Se.writable)&&(m=l(()=>W(void 0)),C(m,un(u)),n.set(f,m));else{g=m.v!==Oe;var w=l(()=>un(u));C(m,w)}var H=Reflect.getOwnPropertyDescriptor(s,f);if(H!=null&&H.set&&H.set.call(h,u),!g){if(r&&typeof f=="string"){var M=n.get("length"),Q=Number(f);Number.isInteger(Q)&&Q>=M.v&&C(M,Q+1)}Lr(a)}return!0},ownKeys(s){i(a);var f=Reflect.ownKeys(s).filter(m=>{var g=n.get(m);return g===void 0||g.v!==Oe});for(var[u,h]of n)h.v!==Oe&&!(u in s)&&f.push(u);return f},setPrototypeOf(){qs()}})}function Wi(e){try{if(e!==null&&typeof e=="object"&&bn in e)return e[bn]}catch{}return e}function Xi(e,t){return Object.is(Wi(e),Wi(t))}var Fn,Ma,Ji,Zi,Qi;function ja(){if(Fn===void 0){Fn=window,Ma=document,Ji=/Firefox/.test(navigator.userAgent);var e=Element.prototype,t=Node.prototype,n=Text.prototype;Zi=V(t,"firstChild").get,Qi=V(t,"nextSibling").get,le(e)&&(e[tr]=void 0,e[er]=null,e[nr]=void 0,e.__e=void 0),le(n)&&(n[wn]=void 0)}}function zt(e=""){return document.createTextNode(e)}function st(e){return Zi.call(e)}function Bt(e){return Qi.call(e)}function He(e,t){if(!F)return st(e);var n=st(B);if(n===null)n=B.appendChild(zt());else if(t&&n.nodeType!==kn){var r=zt();return n==null||n.before(r),et(r),r}return t&&ia(n),et(n),n}function ir(e,t=!1){if(!F){var n=st(e);return n instanceof Comment&&n.data===""?Bt(n):n}if(t){if((B==null?void 0:B.nodeType)!==kn){var r=zt();return B==null||B.before(r),et(r),r}ia(B)}return B}function or(e,t=!1){if(!F)return st(e);var n=He(e,t);return Ne(e),n}function ye(e,t=1,n=!1){let r=F?B:e;for(var a;t--;)a=r,r=Bt(r);if(!F)return r;if(n){if((r==null?void 0:r.nodeType)!==kn){var o=zt();return r===null?a==null||a.after(o):r.before(o),et(o),o}ia(r)}return et(r),r}function il(e){e.textContent=""}function Da(e,t,n){return t==null||t===Ki?document.createElement(e):document.createElementNS(t,e)}function ia(e){if(e.nodeValue.length<65536)return;let t=e.nextSibling;for(;t!==null&&t.nodeType===kn;)t.remove(),e.nodeValue+=t.nodeValue,t=e.nextSibling}function ol(e){var t=U;if(t===null)return Y.f|=_t,e;if((t.f&rn)===0&&(t.f&ze)===0)throw e;Kt(e,t)}function Kt(e,t){if(!(t!==null&&(t.f&Le)!==0)){for(;t!==null;){if((t.f&ht)!==0&&(t.f&(Le|Ln))===0){if((t.f&rn)===0)throw e;try{t.b.error(e);return}catch(n){e=n}}t=t.parent}throw e}}const sl=-7169;function Ce(e,t){e.f=e.f&sl|t}function Fa(e){(e.f&Ie)!==0||e.deps===null?Ce(e,we):Ce(e,Te)}function eo(e){if(e!==null)for(const t of e)(t.f&ve)===0||(t.f&Ut)===0||(t.f^=Ut,eo(t.deps))}function to(e,t,n){(e.f&pe)!==0?t.add(e):(e.f&Te)!==0&&n.add(e),eo(e.deps),Ce(e,we)}function no(e,t,n){if(e==null)return t(void 0),Pe;const r=Mr(()=>e.subscribe(t,n));return r.unsubscribe?()=>r.unsubscribe():r}const sr=[];function ll(e,t=Pe){let n=null;const r=new Set;function a(s){if(Sr(e,s)&&(e=s,n)){const f=!sr.length;for(const u of r)u[1](),sr.push(u,e);if(f){for(let u=0;u<sr.length;u+=2)sr[u][0](sr[u+1]);sr.length=0}}}function o(s){a(s(e))}function l(s,f=Pe){const u=[s,f];return r.add(u),r.size===1&&(n=t(a,o)||Pe),s(e),()=>{r.delete(u),r.size===0&&n&&(n(),n=null)}}return{set:a,update:o,subscribe:l}}function Tr(e){let t;return no(e,n=>t=n)(),t}let Ua=Symbol("unmounted");function ro(e,t,n){const r=n[t]??(n[t]={store:null,source:mo(void 0),unsubscribe:Pe});if(r.store!==e&&!(Ua in n))if(r.unsubscribe(),r.store=e??null,e==null)r.source.v=void 0,r.unsubscribe=Pe;else{var a=!0;r.unsubscribe=no(e,o=>{a?r.source.v=o:C(r.source,o)}),a=!1}return e&&Ua in n?Tr(e):i(r.source)}function cl(){const e={};function t(){fa(()=>{for(var n in e)e[n].unsubscribe();P(e,Ua,{enumerable:!1,value:!0})})}return[e,t]}function ul(e,t){if(t){const n=document.body;e.autofocus=!0,Ht(()=>{document.activeElement===n&&e.focus()})}}let ao=!1;function io(){ao||(ao=!0,document.addEventListener("reset",e=>{Promise.resolve().then(()=>{var t;if(!e.defaultPrevented)for(const n of e.target.elements)(t=n[yn])==null||t.call(n)})},{capture:!0}))}function lr(e){var t=Y,n=U;kt(null),Gt(null);try{return e()}finally{kt(t),Gt(n)}}function fl(e,t,n,r=n){e.addEventListener(t,()=>lr(n));const a=e[yn];a?e[yn]=()=>{a(),r(!0)}:e[yn]=()=>r(!0),io()}function oo(e,t,n,r){const a=Ha;var o=e.filter(p=>!p.settled),l=t.map(a);if(n.length===0&&o.length===0){r(l);return}var s=U,f=dl(),u=o.length===1?o[0].promise:o.length>1?Promise.all(o.map(p=>p.promise)):null;function h(p){if((s.f&Le)===0){f();try{r([...l,...p])}catch(b){Kt(b,s)}oa()}}var m=so();if(n.length===0){u.then(()=>h([])).finally(m);return}function g(){Promise.all(n.map(p=>hl(p))).then(h).catch(p=>Kt(p,s)).finally(m)}u?u.then(()=>{f(),g(),oa()}):g()}function dl(){var e=U,t=Y,n=Ye,r=$;return function(o=!0){Gt(e),kt(t),ar(n),o&&(e.f&Le)===0&&(r==null||r.activate(),r==null||r.apply())}}function oa(e=!0){Gt(null),kt(null),ar(null),e&&($==null||$.deactivate())}function so(){var e=U,t=e.b,n=$,r=!!(t!=null&&t.is_rendered());return t==null||t.update_pending_count(1,n),n.increment(r,e),()=>{t==null||t.update_pending_count(-1,n),n.decrement(r,e)}}function Ha(e){var t=ve|pe;return U!==null&&(U.f|=an),{ctx:Ye,deps:null,effects:null,equals:Er,f:t,fn:e,reactions:null,rv:0,v:Oe,wv:0,parent:U,ac:null}}const $r=Symbol("obsolete");function hl(e,t,n){let r=U;r===null&&Pt();var a=void 0,o=Ir(Oe),l=!Y,s=new Set;return Sl(()=>{var p,b;var f=U,u=je();a=u.promise;try{Promise.resolve(e()).then(u.resolve,w=>{w!==_n&&u.reject(w)}).finally(oa)}catch(w){u.reject(w),oa()}var h=$;if(l){if((f.f&rn)!==0)var m=so();if((p=r.b)!=null&&p.is_rendered())(b=h.async_deriveds.get(f))==null||b.reject($r);else for(const w of s.values())w.reject($r);s.add(u),h.async_deriveds.set(f,u)}const g=(w,H=void 0)=>{m==null||m(),s.delete(u),H!==$r&&(h.activate(),H?(o.f|=_t,ca(o,H)):((o.f&_t)!==0&&(o.f^=_t),ca(o,w)),h.deactivate())};u.promise.then(g,w=>g(null,w||"unknown"))}),fa(()=>{for(const f of s)f.reject($r)}),new Promise(f=>{function u(h){function m(){h===a?f(o):u(a)}h.then(m,m)}u(a)})}function tt(e){const t=Ha(e);return yo(t),t}function vl(e){var t=e.effects;if(t!==null){e.effects=null;for(var n=0;n<t.length;n+=1)qe(t[n])}}function Va(e){var t,n=U,r=e.parent;if(!fn&&r!==null&&e.v!==Oe&&(r.f&(Le|Qe))!==0)return nl(),e.v;Gt(r);try{e.f&=~Ut,vl(e),t=So(e)}finally{Gt(n)}return t}function lo(e){var t=Va(e);if(!e.equals(t)&&(e.wv=ko(),(!($!=null&&$.is_fork)||e.deps===null)&&($!==null?($.capture(e,t,!0),Rr==null||Rr.capture(e,t,!0)):e.v=t,e.deps===null))){Ce(e,we);return}fn||(Ke!==null?(Ya()||$!=null&&$.is_fork)&&Ke.set(e,t):Fa(e))}function pl(e){var t;if(e.effects!==null)for(const n of e.effects)(n.teardown||n.ac)&&((t=n.teardown)==null||t.call(n),n.ac!==null&&lr(()=>{n.ac.abort(_n),n.ac=null}),n.fn!==null&&(n.teardown=Pe),Nr(n,0),Xa(n))}function co(e){if(e.effects!==null)for(const t of e.effects)t.teardown&&t.fn!==null&&fr(t)}let za=null,cr=null,$=null,Rr=null,Ke=null,Ba=null,Pr=!1,Ka=!1,ur=null,sa=null;var uo=0;let gl=1;const _a=class _a{constructor(){j(this,oe);N(this,"id",gl++);j(this,dr,!1);N(this,"linked",!0);j(this,$n,null);j(this,Bn,null);N(this,"async_deriveds",new Map);N(this,"current",new Map);N(this,"previous",new Map);j(this,hr,new Set);j(this,vr,new Set);j(this,pr,0);j(this,dn,new Map);j(this,gr,null);j(this,ut,[]);j(this,Hr,[]);j(this,hn,new Set);j(this,Mt,new Set);j(this,Jt,new Map);j(this,mr,new Set);N(this,"is_fork",!1);j(this,Kn,!1);cr===null?za=cr=this:(L(cr,Bn,this),L(this,$n,cr)),cr=this}skip_effect(t){d(this,Jt).has(t)||d(this,Jt).set(t,{d:[],m:[]}),d(this,mr).delete(t)}unskip_effect(t,n=r=>this.schedule(r)){var r=d(this,Jt).get(t);if(r){d(this,Jt).delete(t);for(var a of r.d)Ce(a,pe),n(a);for(a of r.m)Ce(a,Te),n(a)}d(this,mr).add(t)}capture(t,n,r=!1){t.v!==Oe&&!this.previous.has(t)&&this.previous.set(t,t.v),(t.f&_t)===0&&(this.current.set(t,[n,r]),Ke==null||Ke.set(t,n)),this.is_fork||(t.v=n)}activate(){$=this}deactivate(){$=null,Ke=null}flush(){try{Ka=!0,$=this,q(this,oe,ta).call(this)}finally{uo=0,Ba=null,ur=null,sa=null,Ka=!1,$=null,Ke=null,qt.clear()}}discard(){var t;for(const n of d(this,vr))n(this);d(this,vr).clear();for(const n of this.async_deriveds.values())n.reject($r);q(this,oe,na).call(this),(t=d(this,gr))==null||t.resolve()}register_created_effect(t){d(this,Hr).push(t)}increment(t,n){if(L(this,pr,d(this,pr)+1),t){let r=d(this,dn).get(n)??0;d(this,dn).set(n,r+1)}}decrement(t,n){if(L(this,pr,d(this,pr)-1),t){let r=d(this,dn).get(n)??0;r===1?d(this,dn).delete(n):d(this,dn).set(n,r-1)}d(this,Kn)||(L(this,Kn,!0),Ht(()=>{L(this,Kn,!1),this.linked&&this.flush()}))}transfer_effects(t,n){for(const r of t)d(this,hn).add(r);for(const r of n)d(this,Mt).add(r);t.clear(),n.clear()}oncommit(t){d(this,hr).add(t)}ondiscard(t){d(this,vr).add(t)}settled(){return(d(this,gr)??L(this,gr,je())).promise}static ensure(){if($===null){const t=$=new _a;!Ka&&!Pr&&Ht(()=>{d(t,dr)||t.flush()})}return $}apply(){{Ke=null;return}}schedule(t){var a;if(Ba=t,(a=t.b)!=null&&a.is_pending&&(t.f&(ze|Be|nn))!==0&&(t.f&rn)===0){t.b.defer_effect(t);return}for(var n=t;n.parent!==null;){n=n.parent;var r=n.f;if(ur!==null&&n===U&&(Y===null||(Y.f&ve)===0))return;if((r&(Ae|be))!==0){if((r&we)===0)return;n.f^=we}}d(this,ut).push(n)}};dr=new WeakMap,$n=new WeakMap,Bn=new WeakMap,hr=new WeakMap,vr=new WeakMap,pr=new WeakMap,dn=new WeakMap,gr=new WeakMap,ut=new WeakMap,Hr=new WeakMap,hn=new WeakMap,Mt=new WeakMap,Jt=new WeakMap,mr=new WeakMap,Kn=new WeakMap,oe=new WeakSet,Mi=function(){if(this.is_fork)return!0;for(const r of d(this,dn).keys()){for(var t=r,n=!1;t.parent!==null;){if(d(this,Jt).has(t)){n=!0;break}t=t.parent}if(!n)return!0}return!1},ta=function(){var f,u,h,m;L(this,dr,!0),uo++>1e3&&(q(this,oe,na).call(this),ml());for(const g of d(this,hn))d(this,Mt).delete(g),Ce(g,pe),this.schedule(g);for(const g of d(this,Mt))Ce(g,Te),this.schedule(g);const t=d(this,ut);L(this,ut,[]),this.apply();var n=ur=[],r=[],a=sa=[];for(const g of t)try{q(this,oe,ji).call(this,g,n,r)}catch(p){throw po(g),q(this,oe,Mi).call(this)||this.discard(),p}if($=null,a.length>0){var o=_a.ensure();for(const g of a)o.schedule(g)}if(ur=null,sa=null,q(this,oe,Mi).call(this)){q(this,oe,xr).call(this,r),q(this,oe,xr).call(this,n);for(const[g,p]of d(this,Jt))vo(g,p);a.length>0&&q(f=$,oe,ta).call(f);return}const l=q(this,oe,Us).call(this);if(l){q(this,oe,xr).call(this,r),q(this,oe,xr).call(this,n),q(u=l,oe,Hs).call(u,this);return}d(this,hn).clear(),d(this,Mt).clear();for(const g of d(this,hr))g(this);d(this,hr).clear(),Rr=this,fo(r),fo(n),Rr=null,(h=d(this,gr))==null||h.resolve();var s=$;if(d(this,pr)===0&&(d(this,ut).length===0||s!==null)&&q(this,oe,na).call(this),d(this,ut).length>0)if(s!==null){const g=s;d(g,ut).push(...d(this,ut).filter(p=>!d(g,ut).includes(p)))}else s=this;s!==null&&(qt.clear(),q(m=s,oe,ta).call(m))},ji=function(t,n,r){t.f^=we;for(var a=t.first;a!==null;){var o=a.f,l=(o&(be|Ae))!==0,s=l&&(o&we)!==0,f=s||(o&Qe)!==0||d(this,Jt).has(a);if(!f&&a.fn!==null){l?a.f^=we:(o&ze)!==0?n.push(a):Or(a)&&((o&ce)!==0&&d(this,Mt).add(a),fr(a));var u=a.first;if(u!==null){a=u;continue}}for(;a!==null;){var h=a.next;if(h!==null){a=h;break}a=a.parent}}},Us=function(){for(var t=d(this,$n);t!==null;){if(!t.is_fork){for(const[n,[,r]]of this.current)if(t.current.has(n)&&!r)return t}t=d(t,$n)}return null},Hs=function(t){var r;for(const[a,o]of t.current)!this.previous.has(a)&&t.previous.has(a)&&this.previous.set(a,t.previous.get(a)),this.current.set(a,o);for(const[a,o]of t.async_deriveds){const l=this.async_deriveds.get(a);l&&o.promise.then(l.resolve).catch(l.reject)}t.async_deriveds.clear(),this.transfer_effects(d(t,hn),d(t,Mt));const n=a=>{var o=a.reactions;if(o!==null&&!((a.f&ve)!==0&&(a.f&(pe|Te))===0))for(const f of o){var l=f.f;if((l&ve)!==0)n(f);else{var s=f;l&(on|ce)&&!this.async_deriveds.has(s)&&(d(this,Mt).delete(s),Ce(s,pe),this.schedule(s))}}};for(const a of this.current.keys())n(a);this.oncommit(()=>t.discard()),q(r=t,oe,na).call(r),$=this,q(this,oe,ta).call(this)},xr=function(t){for(var n=0;n<t.length;n+=1)to(t[n],d(this,hn),d(this,Mt))},Yu=function(){var m;for(let g=za;g!==null;g=d(g,Bn)){var t=g.id<this.id,n=[];for(const[p,[b,w]]of this.current){if(g.current.has(p)){var r=g.current.get(p)[0];if(t&&b!==r)g.current.set(p,[b,w]);else continue}n.push(p)}if(t)for(const[p,b]of this.async_deriveds){const w=g.async_deriveds.get(p);w&&b.promise.then(w.resolve).catch(w.reject)}var a=[...g.current.keys()].filter(p=>!g.current.get(p)[1]);if(!(!d(g,dr)||a.length===0)){var o=a.filter(p=>!this.current.has(p));if(o.length===0)t&&g.discard();else if(n.length>0){if(t)for(const p of d(this,mr))g.unskip_effect(p,b=>{var w;(b.f&(ce|on))!==0?g.schedule(b):q(w=g,oe,xr).call(w,[b])});g.activate();var l=new Set,s=new Map;for(var f of n)ho(f,o,l,s);s=new Map;var u=[...g.current].filter(([p,b])=>{const w=this.current.get(p);return w?w[0]!==b[0]||w[1]!==b[1]:!0}).map(([p])=>p);if(u.length>0)for(const p of d(this,Hr))(p.f&(Le|Qe|Zn))===0&&qa(p,u,s)&&((p.f&(on|ce))!==0?(Ce(p,pe),g.schedule(p)):d(g,hn).add(p));if(d(g,ut).length>0&&!d(g,Kn)){g.apply();for(var h of d(g,ut))q(m=g,oe,ji).call(m,h,[],[]);L(g,ut,[])}g.deactivate()}}}},na=function(){if(this.linked){var t=d(this,$n),n=d(this,Bn);t===null?za=n:L(t,Bn,n),n===null?cr=t:L(n,$n,t),this.linked=!1}};let Cn=_a;function ue(e){var t=Pr;Pr=!0;try{for(var n;;){if(tl(),$===null)return n;$.flush()}}finally{Pr=t}}function ml(){try{sn()}catch(e){Kt(e,Ba)}}let Lt=null;function fo(e){var t=e.length;if(t!==0){for(var n=0;n<t;){var r=e[n++];if((r.f&(Le|Qe))===0&&Or(r)&&(Lt=new Set,fr(r),r.deps===null&&r.first===null&&r.nodes===null&&r.teardown===null&&r.ac===null&&Lo(r),(Lt==null?void 0:Lt.size)>0)){qt.clear();for(const a of Lt){if((a.f&(Le|Qe))!==0)continue;const o=[a];let l=a.parent;for(;l!==null;)Lt.has(l)&&(Lt.delete(l),o.push(l)),l=l.parent;for(let s=o.length-1;s>=0;s--){const f=o[s];(f.f&(Le|Qe))===0&&fr(f)}}Lt.clear()}}Lt=null}}function ho(e,t,n,r){if(!n.has(e)&&(n.add(e),e.reactions!==null))for(const a of e.reactions){const o=a.f;(o&ve)!==0?ho(a,t,n,r):(o&(on|ce))!==0&&(o&pe)===0&&qa(a,t,r)&&(Ce(a,pe),Ga(a))}}function qa(e,t,n){const r=n.get(e);if(r!==void 0)return r;if(e.deps!==null)for(const a of e.deps){if(Z.call(t,a))return!0;if((a.f&ve)!==0&&qa(a,t,n))return n.set(a,!0),!0}return n.set(e,!1),!1}function Ga(e){$.schedule(e)}function vo(e,t){if(!((e.f&be)!==0&&(e.f&we)!==0)){(e.f&pe)!==0?t.d.push(e):(e.f&Te)!==0&&t.m.push(e),Ce(e,we);for(var n=e.first;n!==null;)vo(n,t),n=n.next}}function po(e){Ce(e,we);for(var t=e.first;t!==null;)po(t),t=t.next}let la=new Set;const qt=new Map;let go=!1;function Ir(e,t){var n={f:0,v:e,reactions:null,equals:Er,rv:0,wv:0};return n}function W(e,t){const n=Ir(e);return yo(n),n}function mo(e,t=!1,n=!0){const r=Ir(e);return t||(r.equals=z),r}function C(e,t,n=!1){Y!==null&&(!Ot||(Y.f&Zn)!==0)&&qi()&&(Y.f&(ve|ce|on|Zn))!==0&&(Yt===null||!Yt.has(e))&&Gs();let r=n?un(t):t;return ca(e,r,sa)}function ca(e,t,n=null){if(!e.equals(t)){fn?qt.set(e,t):qt.has(e)||qt.set(e,e.v);var r=Cn.ensure();if(r.capture(e,t),(e.f&ve)!==0){const a=e;(e.f&pe)!==0&&Va(a),Ke===null&&Fa(a)}e.wv=ko(),bo(e,pe,n),U!==null&&(U.f&we)!==0&&(U.f&(be|Ae))===0&&(Et===null?wl([e]):Et.push(e)),!r.is_fork&&la.size>0&&!go&&bl()}return t}function bl(){go=!1;for(const e of la){(e.f&we)!==0&&Ce(e,Te);let t;try{t=Or(e)}catch{t=!0}t&&fr(e)}la.clear()}function Lr(e){C(e,e.v+1)}function bo(e,t,n){var r=e.reactions;if(r!==null)for(var a=r.length,o=0;o<a;o++){var l=r[o],s=l.f,f=(s&pe)===0;if(f&&Ce(l,t),(s&Zn)!==0)la.add(l);else if((s&ve)!==0){var u=l;Ke==null||Ke.delete(u),(s&Ut)===0&&(s&Ie&&(U===null||(U.f&On)===0)&&(l.f|=Ut),bo(u,Te,n))}else if(f){var h=l;(s&ce)!==0&&Lt!==null&&Lt.add(h),n!==null?n.push(h):Ga(h)}}}let ua=!1,fn=!1;function wo(e){fn=e}let Y=null,Ot=!1;function kt(e){Y=e}let U=null;function Gt(e){U=e}let Yt=null;function yo(e){Y!==null&&(Yt??(Yt=new Set)).add(e)}let lt=null,gt=0,Et=null;function wl(e){Et=e}let _o=1,Un=0,Hn=Un;function xo(e){Hn=e}function ko(){return++_o}function Or(e){var t=e.f;if((t&pe)!==0)return!0;if(t&ve&&(e.f&=~Ut),(t&Te)!==0){for(var n=e.deps,r=n.length,a=0;a<r;a++){var o=n[a];if(Or(o)&&lo(o),o.wv>e.wv)return!0}(t&Ie)!==0&&Ke===null&&Ce(e,we)}return!1}function Eo(e,t,n=!0){var r=e.reactions;if(r!==null&&!(Yt!==null&&Yt.has(e)))for(var a=0;a<r.length;a++){var o=r[a];(o.f&ve)!==0?Eo(o,t,!1):t===o&&(n?Ce(o,pe):(o.f&we)!==0&&Ce(o,Te),Ga(o))}}function So(e){var t=lt,n=gt,r=Et,a=Y,o=Yt,l=Ye,s=Ot,f=Hn,u=e.f;lt=null,gt=0,Et=null,Y=(u&(be|Ae))===0?e:null,Yt=null,ar(e.ctx),Ot=!1,Hn=++Un,e.ac!==null&&(lr(()=>{e.ac.abort(_n)}),e.ac=null);try{e.f|=On;var h=e.fn,m=h();e.f|=rn;var g=Co(e);if(qi()&&Et!==null&&!Ot&&g!==null&&(e.f&(ve|Te|pe))===0)for(var p=0;p<Et.length;p++)Eo(Et[p],e);if(a!==null&&a!==e){if(Un++,a.deps!==null)for(let b=0;b<n;b+=1)a.deps[b].rv=Un;if(t!==null)for(const b of t)b.rv=Un;Et!==null&&(r===null?r=Et:r.push(...Et))}return(e.f&_t)!==0&&(e.f^=_t),m}catch(b){return Co(e),ol(b)}finally{e.f^=On,lt=t,gt=n,Et=r,Y=a,Yt=o,ar(l),Ot=s,Hn=f}}function Co(e){var a;var t=e.deps,n=$==null?void 0:$.is_fork;if(lt!==null){var r;if(n||Nr(e,gt),t!==null&&gt>0)for(t.length=gt+lt.length,r=0;r<lt.length;r++)t[gt+r]=lt[r];else e.deps=t=lt;if(Ya()&&(e.f&Ie)!==0)for(r=gt;r<t.length;r++)((a=t[r]).reactions??(a.reactions=[])).push(e)}else!n&&t!==null&&gt<t.length&&(Nr(e,gt),t.length=gt);return t}function yl(e,t){let n=t.reactions;if(n!==null){var r=R.call(n,e);if(r!==-1){var a=n.length-1;a===0?n=t.reactions=null:(n[r]=n[a],n.pop())}}if(n===null&&(t.f&ve)!==0&&(lt===null||!Z.call(lt,t))){var o=t;(o.f&Ie)!==0&&(o.f^=Ie,o.f&=~Ut),o.v!==Oe&&Fa(o),o.ac!==null&&lr(()=>{o.ac.abort(_n),o.ac=null,Ce(o,pe)}),pl(o),Nr(o,0)}}function Nr(e,t){var n=e.deps;if(n!==null)for(var r=t;r<n.length;r++)yl(e,n[r])}function fr(e){var t=e.f;if((t&Le)===0){Ce(e,we);var n=U,r=ua;U=e,ua=(t&(be|Ae))===0;try{(t&(ce|nn))!==0?Cl(e):Xa(e),Po(e);var a=So(e);e.teardown=typeof a=="function"?a:null,e.wv=_o;var o;E&&Ws&&(e.f&pe)!==0&&e.deps}finally{ua=r,U=n}}}async function Vn(){await Promise.resolve(),ue()}function i(e){var t=e.f,n=(t&ve)!==0;if(Y!==null&&!Ot){var r=U!==null&&(U.f&Le)!==0;if(!r&&(Yt===null||!Yt.has(e))){var a=Y.deps;if((Y.f&On)!==0)e.rv<Un&&(e.rv=Un,lt===null&&a!==null&&a[gt]===e?gt++:lt===null?lt=[e]:lt.push(e));else{Y.deps??(Y.deps=[]),Z.call(Y.deps,e)||Y.deps.push(e);var o=e.reactions;o===null?e.reactions=[Y]:Z.call(o,Y)||o.push(Y)}}}if(fn&&qt.has(e))return qt.get(e);if(n){var l=e;if(fn){var s=l.v;return((l.f&we)===0&&l.reactions!==null||To(l))&&(s=Va(l)),qt.set(l,s),s}var f=(l.f&Ie)===0&&!Ot&&Y!==null&&(ua||(Y.f&Ie)!==0),u=(l.f&rn)===0;Or(l)&&(f&&(l.f|=Ie),lo(l)),f&&!u&&(co(l),Ao(l))}if(Ke!=null&&Ke.has(e))return Ke.get(e);if((e.f&_t)!==0)throw e.v;return e.v}function Ao(e){if(e.f|=Ie,e.deps!==null)for(const t of e.deps)(t.reactions??(t.reactions=[])).push(e),(t.f&ve)!==0&&(t.f&Ie)===0&&(co(t),Ao(t))}function To(e){if(e.v===Oe)return!0;if(e.deps===null)return!1;for(const t of e.deps)if(qt.has(t)||(t.f&ve)!==0&&To(t))return!0;return!1}function Mr(e){var t=Ot;try{return Ot=!0,e()}finally{Ot=t}}function _l(e){U===null&&(Y===null&&rr(),Sn()),fn&&It()}function xl(e,t){var n=t.last;n===null?t.last=t.first=e:(n.next=e,e.prev=n,t.last=e)}function Nt(e,t){var n=U;n!==null&&(n.f&Qe)!==0&&(e|=Qe);var r={ctx:Ye,deps:null,nodes:null,f:e|pe|Ie,first:null,fn:t,last:null,next:null,parent:n,b:n&&n.b,prev:null,teardown:null,wv:0,ac:null};$==null||$.register_created_effect(r);var a=r;if((e&ze)!==0)ur!==null?ur.push(r):Cn.ensure().schedule(r);else if(t!==null){try{fr(r)}catch(l){throw qe(r),l}a.deps===null&&a.teardown===null&&a.nodes===null&&a.first===a.last&&(a.f&an)===0&&(a=a.first,(e&ce)!==0&&(e&pt)!==0&&a!==null&&(a.f|=pt))}if(a!==null&&(a.parent=n,n!==null&&xl(a,n),Y!==null&&(Y.f&ve)!==0&&(e&Ae)===0)){var o=Y;(o.effects??(o.effects=[])).push(a)}return r}function Ya(){return Y!==null&&!Ot}function fa(e){const t=Nt(Be,null);return Ce(t,we),t.teardown=e,t}function ct(e){_l();var t=U.f,n=!Y&&(t&be)!==0&&Ye!==null&&!Ye.i;if(n){var r=Ye;(r.e??(r.e=[])).push(e)}else return $o(e)}function $o(e){return Nt(ze|Qn,e)}function kl(e){Cn.ensure();const t=Nt(Ae|an,e);return()=>{qe(t)}}function El(e){Cn.ensure();const t=Nt(Ae|an,e);return(n={})=>new Promise(r=>{n.outro?Dr(t,()=>{qe(t),r(void 0)}):(qe(t),r(void 0))})}function Wa(e){return Nt(ze,e)}function Sl(e){return Nt(on|an,e)}function da(e,t=0){return Nt(Be|t,e)}function We(e,t=[],n=[],r=[]){oo(r,t,n,a=>{Nt(Be,()=>{e(...a.map(i))})})}function jr(e,t=0){var n=Nt(ce|t,e);return n}function Ro(e,t=0){var n=Nt(nn|t,e);return n}function Wt(e){return Nt(be|an,e)}function Po(e){var t=e.teardown;if(t!==null){const n=fn,r=Y;wo(!0),kt(null);try{t.call(null)}catch(a){Kt(a,e.parent)}finally{wo(n),kt(r)}}}function Xa(e,t=!1){var n=e.first;for(e.first=e.last=null;n!==null;){const a=n.ac;a!==null&&lr(()=>{a.abort(_n)});var r=n.next;(n.f&Ae)!==0?n.parent=null:qe(n,t),n=r}}function Cl(e){for(var t=e.first;t!==null;){var n=t.next;(t.f&be)===0&&qe(t),t=n}}function qe(e,t=!0){var n=!1;(t||(e.f&kr)!==0)&&e.nodes!==null&&e.nodes.end!==null&&(Io(e.nodes.start,e.nodes.end),n=!0),e.f|=Ln,Xa(e,t&&!n),Nr(e,0);var r=e.nodes&&e.nodes.t;if(r!==null)for(const o of r)o.stop();Po(e),e.f^=Ln,e.f|=Le;var a=e.parent;a!==null&&a.first!==null&&Lo(e),e.next=e.prev=e.teardown=e.ctx=e.deps=e.fn=e.nodes=e.ac=e.b=null}function Io(e,t){for(;e!==null;){var n=e===t?null:Bt(e);e.remove(),e=n}}function Lo(e){var t=e.parent,n=e.prev,r=e.next;n!==null&&(n.next=r),r!==null&&(r.prev=n),t!==null&&(t.first===e&&(t.first=r),t.last===e&&(t.last=n))}function Dr(e,t,n=!0){var r=[];e.f|=vt,Oo(e,r,!0);var a=()=>{n&&qe(e),t&&t()},o=r.length;if(o>0){var l=()=>--o||a();for(var s of r)s.out(l)}else a()}function Oo(e,t,n){if((e.f&Qe)===0){e.f^=Qe;var r=e.nodes&&e.nodes.t;if(r!==null)for(const s of r)(s.is_global||n)&&t.push(s);for(var a=e.first;a!==null;){var o=a.next;if((a.f&Ae)===0){var l=(a.f&pt)!==0||(a.f&be)!==0&&(e.f&ce)!==0;Oo(a,t,l?n:!1)}a=o}}}function No(e){e.f&=~vt,Mo(e,!0)}function Mo(e,t){if((e.f&vt)===0&&(e.f&Qe)!==0){e.f^=Qe,(e.f&we)===0&&(Ce(e,pe),Cn.ensure().schedule(e));for(var n=e.first;n!==null;){var r=n.next,a=(n.f&pt)!==0||(n.f&be)!==0;Mo(n,a?t:!1),n=r}var o=e.nodes&&e.nodes.t;if(o!==null)for(const l of o)(l.is_global||t)&&l.in()}}function jo(e,t){if(e.nodes)for(var n=e.nodes.start,r=e.nodes.end;n!==null;){var a=n===r?null:Bt(n);t.append(n),n=a}}function Al(e){let t=0,n=Ir(0),r;return()=>{Ya()&&(i(n),da(()=>(t===0&&(r=Mr(()=>e(()=>Lr(n)))),t+=1,()=>{Ht(()=>{t-=1,t===0&&(r==null||r(),r=void 0,Lr(n))})})))}}function Do(e){const t={get:n=>Tr(t.store)[n],set:(n,r)=>{typeof n=="string"?Object.assign(Tr(t.store),{[n]:r}):Object.assign(Tr(t.store),n),t.store.set(Tr(t.store))},store:ll(e)};return t}globalThis.$altcha=globalThis.$altcha||{algorithms:new Map,defaults:Do({}),i18n:Do({}),instances:new Set,plugins:new Set};const Tl={ariaLinkLabel:"Altcha (official website)",cancel:"Cancel",enterCode:"Enter code",enterCodeAria:"Enter code you hear. Press Space to play audio.",enterCodeFromImage:"To proceed, please enter the code from the image below.",error:"Verification failed. Try again later.",expired:"Verification expired. Try again.",footer:'Protected by <a href="https://altcha.org/" target="_blank" rel="noopener" aria-label="Altcha (official website, opens in new tab)">ALTCHA</a>',getAudioChallenge:"Get an audio challenge",label:"I'm not a robot",loading:"Loading...",reload:"Reload",verify:"Verify",verificationRequired:"Verification required!",verified:"Verified",verifying:"Verifying...",waitAlert:"Verifying... please wait."};globalThis.$altcha.i18n.set("en",Tl);const $l="5";typeof window<"u"&&((ms=window.__svelte??(window.__svelte={})).v??(ms.v=new Set)).add($l);const zn=Symbol("events"),Fo=new Set,Ja=new Set;function Uo(e,t,n,r={}){function a(o){if(r.capture||ei.call(t,o),!o.cancelBubble)return lr(()=>n==null?void 0:n.call(this,o))}return e.startsWith("pointer")||e.startsWith("touch")||e==="wheel"?Ht(()=>{t.addEventListener(e,a,r)}):t.addEventListener(e,a,r),a}function De(e,t,n,r,a){var o={capture:r,passive:a},l=Uo(e,t,n,o);(t===document.body||t===window||t===document||t instanceof HTMLMediaElement)&&fa(()=>{t.removeEventListener(e,l,o)})}function ha(e,t,n){(t[zn]??(t[zn]={}))[e]=n}function va(e){for(var t=0;t<e.length;t++)Fo.add(e[t]);for(var n of Ja)n(e)}let Za=null,Qa=!1;function ei(e){var w,H;var t=this,n=t.ownerDocument,r=e.type,a=((w=e.composedPath)==null?void 0:w.call(e))||[],o=a[0]||e.target;Za=e,Qa||(Qa=!0,setTimeout(()=>{Qa=!1,Za=null}));var l=0,s=Za===e&&e[zn];if(s){var f=a.indexOf(s);if(f!==-1&&(t===document||t===window)){e[zn]=t;return}var u=a.indexOf(t);if(u===-1)return;f<=u&&(l=f)}if(o=a[l]||e.target,o!==t){P(e,"currentTarget",{configurable:!0,get(){return o||n}});var h=Y,m=U;kt(null),Gt(null);try{for(var g,p=[];o!==null&&o!==t;){try{var b=(H=o[zn])==null?void 0:H[r];b!=null&&(!o.disabled||e.target===o)&&b.call(o,e)}catch(M){g?p.push(M):g=M}if(e.cancelBubble)break;l++,o=l<a.length?a[l]:null}if(g){for(let M of p)queueMicrotask(()=>{throw M});throw g}}finally{e[zn]=t,delete e.currentTarget,kt(h),Gt(m)}}}const ti=((bs=globalThis==null?void 0:globalThis.window)==null?void 0:bs.trustedTypes)&&globalThis.window.trustedTypes.createPolicy("svelte-trusted-html",{createHTML:e=>e});function Rl(e){return(ti==null?void 0:ti.createHTML(e))??e}function Ho(e){var t=Da("template");return t.innerHTML=Rl(e.replaceAll("<!>","<!---->")),t.content}function mt(e,t){var n=U;n.nodes===null&&(n.nodes={start:e,end:t,a:null,t:null})}function Ee(e,t){var n=(t&Xs)!==0,r=(t&Js)!==0,a,o=!e.startsWith("<!>");return()=>{if(F)return mt(B,null),B;a===void 0&&(a=Ho(o?e:"<!>"+e),n||(a=st(a)));var l=r||Ji?document.importNode(a,!0):a.cloneNode(!0);if(n){var s=st(l),f=l.lastChild;mt(s,f)}else mt(l,l);return l}}function Pl(e,t,n="svg"){var r=!e.startsWith("<!>"),a=`<${n}>${r?e:"<!>"+e}</${n}>`,o;return()=>{if(F)return mt(B,null),B;if(!o){var l=Ho(a),s=st(l);o=st(s)}var f=o.cloneNode(!0);return mt(f,f),f}}function ni(e,t){return Pl(e,t,"svg")}function pa(e=""){if(!F){var t=zt(e+"");return mt(t,t),t}var n=B;return n.nodeType!==kn?(n.before(n=zt()),et(n)):ia(n),mt(n,n),n}function Vo(){if(F)return mt(B,null),B;var e=document.createDocumentFragment(),t=document.createComment(""),n=zt();return e.append(t,n),mt(t,n),e}function X(e,t){if(F){var n=U;((n.f&rn)===0||n.nodes.end===null)&&(n.nodes.end=B),Dn();return}e!==null&&e.before(t)}function Il(e){return e.endsWith("capture")&&e!=="gotpointercapture"&&e!=="lostpointercapture"}const Ll=["beforeinput","click","change","dblclick","contextmenu","focusin","focusout","input","keydown","keyup","mousedown","mousemove","mouseout","mouseover","mouseup","pointerdown","pointermove","pointerout","pointerover","pointerup","touchend","touchmove","touchstart"];function Ol(e){return Ll.includes(e)}const Nl={formnovalidate:"formNoValidate",ismap:"isMap",nomodule:"noModule",playsinline:"playsInline",readonly:"readOnly",defaultvalue:"defaultValue",defaultchecked:"defaultChecked",srcobject:"srcObject",novalidate:"noValidate",allowfullscreen:"allowFullscreen",disablepictureinpicture:"disablePictureInPicture",disableremoteplayback:"disableRemotePlayback"};function Ml(e){return e=e.toLowerCase(),Nl[e]??e}const jl=["touchstart","touchmove"];function Dl(e){return jl.includes(e)}var Fl=pt|an;function Ul(e,t,n,r){new Hl(e,t,n,r)}class Hl{constructor(t,n,r,a){j(this,se);N(this,"parent");N(this,"is_pending",!1);N(this,"transform_error");j(this,bt);j(this,Vr,F?B:null);j(this,St);j(this,qn);j(this,nt);j(this,wt,null);j(this,rt,null);j(this,yt,null);j(this,Zt,null);j(this,Gn,0);j(this,Rn,0);j(this,br,!1);j(this,zr,new Set);j(this,Br,new Set);j(this,vn,null);j(this,xa,Al(()=>(L(this,vn,Ir(d(this,Gn))),()=>{L(this,vn,null)})));var o;L(this,bt,t),L(this,St,n),L(this,qn,l=>{var s=U;s.b=this,s.f|=ht,r(l)}),this.parent=U.b,this.transform_error=a??((o=this.parent)==null?void 0:o.transform_error)??(l=>l),L(this,nt,jr(()=>{if(F){const l=d(this,Vr);Dn();const s=l.data===Vi;if(l.data.startsWith(zi)){const u=JSON.parse(l.data.slice(zi.length));q(this,se,zs).call(this,u)}else s?q(this,se,Bs).call(this):q(this,se,Vs).call(this)}else q(this,se,Fi).call(this)},Fl)),F&&L(this,bt,B)}defer_effect(t){to(t,d(this,zr),d(this,Br))}is_rendered(){return!this.is_pending&&(!this.parent||this.parent.is_rendered())}has_pending_snippet(){return!!d(this,St).pending}update_pending_count(t,n){q(this,se,Ui).call(this,t,n),L(this,Gn,d(this,Gn)+t),!(!d(this,vn)||d(this,br))&&(L(this,br,!0),Ht(()=>{L(this,br,!1),d(this,vn)&&ca(d(this,vn),d(this,Gn))}))}get_effect_pending(){return d(this,xa).call(this),i(d(this,vn))}error(t){if(!d(this,St).onerror&&!d(this,St).failed)throw t;$!=null&&$.is_fork?(d(this,wt)&&$.skip_effect(d(this,wt)),d(this,rt)&&$.skip_effect(d(this,rt)),d(this,yt)&&$.skip_effect(d(this,yt)),$.oncommit(()=>{q(this,se,Hi).call(this,t)})):q(this,se,Hi).call(this,t)}}bt=new WeakMap,Vr=new WeakMap,St=new WeakMap,qn=new WeakMap,nt=new WeakMap,wt=new WeakMap,rt=new WeakMap,yt=new WeakMap,Zt=new WeakMap,Gn=new WeakMap,Rn=new WeakMap,br=new WeakMap,zr=new WeakMap,Br=new WeakMap,vn=new WeakMap,xa=new WeakMap,se=new WeakSet,Vs=function(){try{L(this,wt,Wt(()=>d(this,qn).call(this,d(this,bt))))}catch(t){this.error(t)}},zs=function(t){const n=d(this,St).failed,{reset:r,invoke_onerror:a}=q(this,se,Di).call(this,t);Ht(a),n&&L(this,yt,Wt(()=>{n(d(this,bt),()=>t,()=>r)}))},Di=function(t){var n=!1,r=!1;const a=()=>{if(n){al();return}n=!0,r&&Ys(),d(this,yt)!==null&&Dr(d(this,yt),()=>{L(this,yt,null)}),q(this,se,$a).call(this,()=>{q(this,se,Fi).call(this)})};return{reset:a,invoke_onerror:()=>{var l,s;try{r=!0,(s=(l=d(this,St)).onerror)==null||s.call(l,t,a),r=!1}catch(f){Kt(f,d(this,nt)&&d(this,nt).parent)}}}},Bs=function(){const t=d(this,St).pending;t&&(this.is_pending=!0,L(this,rt,Wt(()=>t(d(this,bt)))),Ht(()=>{var n=L(this,Zt,document.createDocumentFragment()),r=zt(),a=!1;if(n.append(r),L(this,wt,q(this,se,$a).call(this,()=>{try{return Wt(()=>d(this,qn).call(this,r))}catch(o){try{this.error(o),a=!0}catch(l){Kt(l,d(this,nt).parent)}return null}})),d(this,wt)===null){L(this,Zt,null),a&&q(this,se,ra).call(this,$);return}d(this,Rn)===0&&(d(this,bt).before(n),L(this,Zt,null),Dr(d(this,rt),()=>{L(this,rt,null)}),q(this,se,ra).call(this,$))}))},Fi=function(){try{if(this.is_pending=this.has_pending_snippet(),L(this,Rn,0),L(this,Gn,0),L(this,wt,Wt(()=>{d(this,qn).call(this,d(this,bt))})),d(this,Rn)>0){var t=L(this,Zt,document.createDocumentFragment());jo(d(this,wt),t);const n=d(this,St).pending;L(this,rt,Wt(()=>n(d(this,bt))))}else q(this,se,ra).call(this,$)}catch(n){this.error(n)}},ra=function(t){this.is_pending=!1,t.transfer_effects(d(this,zr),d(this,Br))},$a=function(t){var n=U,r=Y,a=Ye;Gt(d(this,nt)),kt(d(this,nt)),ar(d(this,nt).ctx);try{return Cn.ensure(),t()}finally{Gt(n),kt(r),ar(a)}},Ui=function(t,n){var r;if(!this.has_pending_snippet()){this.parent&&q(r=this.parent,se,Ui).call(r,t,n);return}L(this,Rn,d(this,Rn)+t),d(this,Rn)===0&&(q(this,se,ra).call(this,n),d(this,rt)&&Dr(d(this,rt),()=>{L(this,rt,null)}),d(this,Zt)&&(d(this,bt).before(d(this,Zt)),L(this,Zt,null)))},Hi=function(t){d(this,wt)&&(qe(d(this,wt)),L(this,wt,null)),d(this,rt)&&(qe(d(this,rt)),L(this,rt,null)),d(this,yt)&&(qe(d(this,yt)),L(this,yt,null)),F&&(et(d(this,Vr)),Oa(),et(Na()));let n=d(this,St).failed;const r=a=>{const{reset:o,invoke_onerror:l}=q(this,se,Di).call(this,a);l(),n&&L(this,yt,q(this,se,$a).call(this,()=>{try{return Wt(()=>{var s=U;s.b=this,s.f|=ht,n(d(this,bt),()=>a,()=>o)})}catch(s){return Kt(s,d(this,nt).parent),null}}))};Ht(()=>{var a;try{a=this.transform_error(t)}catch(o){Kt(o,d(this,nt)&&d(this,nt).parent);return}a!==null&&typeof a=="object"&&typeof a.then=="function"?a.then(r,o=>Kt(o,d(this,nt)&&d(this,nt).parent)):r(a)})};function Xt(e,t){var n=t==null?"":typeof t=="object"?`${t}`:t;n!==(e[wn]??(e[wn]=e.nodeValue))&&(e[wn]=n,e.nodeValue=`${n}`)}function zo(e,t){return Bo(e,t)}function Vl(e,t){ja(),t.intro=t.intro??!1;const n=t.target,r=F,a=B;try{for(var o=st(n);o&&(o.nodeType!==En||o.data!==Ia);)o=Bt(o);if(!o)throw Mn;Vt(!0),et(o);const l=Bo(e,{...t,anchor:o});return Vt(!1),l}catch(l){if(l instanceof Error&&l.message.split(`
`).some(s=>s.startsWith("https://svelte.dev/e/")))throw l;return l!==Mn&&console.warn("Failed to hydrate: ",l),t.recover===!1&&Cr(),ja(),il(n),Vt(!1),zo(e,t)}finally{Vt(r),et(a)}}const ga=new Map;function Bo(e,{target:t,anchor:n,props:r={},events:a,context:o,intro:l=!0,transformError:s}){ja();var f=void 0,u=El(()=>{var h=n??t.appendChild(zt());Ul(h,{pending:()=>{}},p=>{ln({});var b=Ye;if(o&&(b.c=o),a&&(r.$$events=a),F&&mt(p,null),f=e(p,r)||La(),F&&(U.nodes.end=B,B===null||B.nodeType!==En||B.data!==Bi))throw Ar(),Mn;cn()},s);var m=new Set,g=p=>{for(var b=0;b<p.length;b++){var w=p[b];if(!m.has(w)){m.add(w);var H=Dl(w);for(const Se of[t,document]){var M=ga.get(Se);M===void 0&&(M=new Map,ga.set(Se,M));var Q=M.get(w);Q===void 0?(Se.addEventListener(w,ei,{passive:H}),M.set(w,1)):M.set(w,Q+1)}}}};return g($e(Fo)),Ja.add(g),()=>{var H;for(var p of m)for(const M of[t,document]){var b=ga.get(M),w=b.get(p);--w==0?(M.removeEventListener(p,ei),b.delete(p),b.size===0&&ga.delete(M)):b.set(p,w)}Ja.delete(g),h!==n&&((H=h.parentNode)==null||H.removeChild(h))}});return ri.set(f,u),f}let ri=new WeakMap;function zl(e,t){const n=ri.get(e);return n?(ri.delete(e),n(t)):Promise.resolve()}class ma{constructor(t,n=!0){N(this,"anchor");j(this,jt,new Map);j(this,pn,new Map);j(this,Dt,new Map);j(this,Yn,new Set);j(this,Kr,!0);j(this,ka,t=>{if(d(this,jt).has(t)){var n=d(this,jt).get(t),r=d(this,pn).get(n);if(r)No(r),d(this,Yn).delete(n);else{var a=d(this,Dt).get(n);a&&(No(a.effect),d(this,pn).set(n,a.effect),d(this,Dt).delete(n),a.fragment.lastChild.remove(),this.anchor.before(a.fragment),r=a.effect)}for(const[o,l]of d(this,jt)){if(d(this,jt).delete(o),o===t)break;const s=d(this,Dt).get(l);s&&(qe(s.effect),d(this,Dt).delete(l))}for(const[o,l]of d(this,pn)){if(o===n||d(this,Yn).has(o))continue;const s=()=>{if(Array.from(d(this,jt).values()).includes(o)){var u=document.createDocumentFragment();jo(l,u),u.append(zt()),d(this,Dt).set(o,{effect:l,fragment:u})}else qe(l);d(this,Yn).delete(o),d(this,pn).delete(o)};d(this,Kr)||!r?(d(this,Yn).add(o),Dr(l,s,!1)):s()}}});j(this,wi,t=>{d(this,jt).delete(t);const n=Array.from(d(this,jt).values());for(const[r,a]of d(this,Dt))n.includes(r)||(qe(a.effect),d(this,Dt).delete(r))});this.anchor=t,L(this,Kr,n)}ensure(t,n){var r=$;n&&!d(this,pn).has(t)&&!d(this,Dt).has(t)&&d(this,pn).set(t,Wt(()=>n(this.anchor))),d(this,jt).set(r,t),F&&(this.anchor=B),d(this,ka).call(this,r)}}jt=new WeakMap,pn=new WeakMap,Dt=new WeakMap,Yn=new WeakMap,Kr=new WeakMap,ka=new WeakMap,wi=new WeakMap;function Bl(e,t,...n){var r=new ma(e);jr(()=>{const a=t()??null;r.ensure(a,a&&(o=>a(o,...n)))},pt)}function ai(e){Ye===null&&xt(),ct(()=>{const t=Mr(e);if(typeof t=="function")return t})}function Fe(e,t,n=!1){var r;F&&(r=B,Dn());var a=new ma(e),o=n?pt:0;function l(s,f){if(F){var u=Yi(r);if(s!==parseInt(u.substring(1))){var h=Na();et(h),a.anchor=h,Vt(!1),a.ensure(s,f),Vt(!0);return}}a.ensure(s,f)}jr(()=>{var s=!1;t((f,u=0)=>{s=!0,l(u,f)}),s||l(-1,null)},o)}const Kl=Symbol("NaN");function ql(e,t,n){F&&Dn();var r=new ma(e);jr(()=>{var a=t();a!==a&&(a=Kl),r.ensure(a,n)})}function Ko(e,t,n=!1,r=!1,a=!1,o=!1){var l=e,s="";if(n){var f=e;F&&(l=et(st(f)))}We(()=>{var u=U;if(s===(s=t()??"")){F&&Dn();return}if(n&&!F){u.nodes=null,f.innerHTML=s,s!==""&&mt(st(f),f.lastChild);return}if(u.nodes!==null&&(Io(u.nodes.start,u.nodes.end),u.nodes=null),s!==""){if(F){B.data;for(var h=Dn(),m=h;h!==null&&(h.nodeType!==En||h.data!=="");)m=h,h=Bt(h);if(h===null)throw Ar(),Mn;mt(B,m),l=et(h);return}var g=r?Zs:a?Qs:void 0,p=Da(r?"svg":a?"math":"template",g);p.innerHTML=s;var b=r||a?p:p.content;if(mt(st(b),b.lastChild),r||a)for(;st(b);)l.before(st(b));else l.before(b)}})}function Gl(e,t,n){var r;F&&(r=B,Dn());var a=new ma(e);jr(()=>{var o=t()??null;if(F){var l=Yi(r),s=l===Ia,f=o!==null;if(s!==f){var u=Na();et(u),a.anchor=u,Vt(!1),a.ensure(o,o&&(h=>n(h,o))),Vt(!0);return}}a.ensure(o,o&&(h=>n(h,o)))},pt)}function Yl(e,t){var n=void 0,r;Ro(()=>{n!==(n=t())&&(r&&(qe(r),r=null),n&&(r=Wt(()=>{Wa(()=>n(e))})))})}function qo(e){var t,n,r="";if(typeof e=="string"||typeof e=="number")r+=e;else if(typeof e=="object")if(Array.isArray(e)){var a=e.length;for(t=0;t<a;t++)e[t]&&(n=qo(e[t]))&&(r&&(r+=" "),r+=n)}else for(n in e)e[n]&&(r&&(r+=" "),r+=n);return r}function Wl(){for(var e,t,n=0,r="",a=arguments.length;n<a;n++)(e=arguments[n])&&(t=qo(e))&&(r&&(r+=" "),r+=t);return r}function Xl(e){return typeof e=="object"?Wl(e):e??""}const Go=[...` 	
\r\f \v\uFEFF`];function Jl(e,t,n){var r=e==null?"":""+e;if(n){for(var a of Object.keys(n))if(n[a])r=r?r+" "+a:a;else if(r.length)for(var o=a.length,l=0;(l=r.indexOf(a,l))>=0;){var s=l+o;(l===0||Go.includes(r[l-1]))&&(s===r.length||Go.includes(r[s]))?r=(l===0?"":r.substring(0,l))+r.substring(s+1):l=s}}return r===""?null:r}function Yo(e,t=!1){var n=t?" !important;":";",r="";for(var a of Object.keys(e)){var o=e[a];o!=null&&o!==""&&(r+=" "+a+": "+o+n)}return r}function ii(e){return e[0]!=="-"||e[1]!=="-"?e.toLowerCase():e}function Zl(e,t){if(t){var n="",r,a;if(Array.isArray(t)?(r=t[0],a=t[1]):r=t,e){e=String(e).replaceAll(/\/\*.*?\*\//g,"").trim();var o=!1,l=0,s=!1,f=[];r&&f.push(...Object.keys(r).map(ii)),a&&f.push(...Object.keys(a).map(ii));var u=0,h=-1;const w=e.length;for(var m=0;m<w;m++){var g=e[m];if(s?g==="/"&&e[m-1]==="*"&&(s=!1):o?o===g&&(o=!1):g==="/"&&e[m+1]==="*"?s=!0:g==='"'||g==="'"?o=g:g==="("?l++:g===")"&&l--,!s&&o===!1&&l===0){if(g===":"&&h===-1)h=m;else if(g===";"||m===w-1){if(h!==-1){var p=ii(e.substring(u,h).trim());if(!f.includes(p)){g!==";"&&m++;var b=e.substring(u,m).trim();n+=" "+b+";"}}u=m+1,h=-1}}}}return r&&(n+=Yo(r)),a&&(n+=Yo(a,!0)),n=n.trim(),n===""?null:n}return e==null?null:String(e)}function Ql(e,t,n,r,a,o){var l=e[tr];if(F||l!==n||l===void 0){var s=Jl(n,r,o);(!F||s!==e.getAttribute("class"))&&(s==null?e.removeAttribute("class"):t?e.className=s:e.setAttribute("class",s)),e[tr]=n}else if(o&&a!==o)for(var f in o){var u=!!o[f];(a==null||u!==!!a[f])&&e.classList.toggle(f,u)}return o}function oi(e,t={},n,r){for(var a in n){var o=n[a];t[a]!==o&&(n[a]==null?e.style.removeProperty(a):e.style.setProperty(a,o,r))}}function ec(e,t,n,r){var a=e[nr];if(F||a!==t){var o=Zl(t,r);(!F||o!==e.getAttribute("style"))&&(o==null?e.removeAttribute("style"):e.style.cssText=o),e[nr]=t}else r&&(Array.isArray(r)?(oi(e,n==null?void 0:n[0],r[0]),oi(e,n==null?void 0:n[1],r[1],"important")):oi(e,n,r));return r}function Wo(e,t){t?e.hasAttribute("selected")||e.setAttribute("selected",""):e.removeAttribute("selected")}function Xo(e,t){var n=!("__defaultValue"in e);!n&&e.__defaultValue===t||(e.__defaultValue=t,Jo(e,!n||"__value"in e))}function Jo(e,t){var n=e.__defaultValue,r=e.multiple,a=r?n??[]:null;if(!(r&&!k(a))){var o=e.selectedIndex,l=t&&r?new Set(e.selectedOptions):null;for(var s of e.options){var f=li(s);Wo(s,r?a.includes(f):Xi(f,n))}if(t)if(l!==null)for(s of e.options){var u=l.has(s);s.selected!==u&&(s.selected=u)}else e.selectedIndex!==o&&(e.selectedIndex=o)}}function si(e,t,n=!1){if(e.multiple){if(t==null)return;if(!k(t))return rl();for(var r of e.options)r.selected=t.includes(li(r));return}for(r of e.options){var a=li(r);if(Xi(a,t)){r.selected=!0;return}}(!n||t!==void 0)&&(e.selectedIndex=-1)}function tc(e){var t=new MutationObserver(n=>{n.every(nc)||("__defaultValue"in e&&Jo(e,!1),"__value"in e&&si(e,e.__value))});t.observe(e,{childList:!0,subtree:!0,attributes:!0,attributeFilter:["value"]}),fa(()=>{t.disconnect()})}function li(e){return"__value"in e?e.__value:e.value}function nc(e){if(e.target.closest("selectedcontent")!==null)return!0;if(e.type==="childList"){var t=[...e.addedNodes,...e.removedNodes];return t.length>0&&t.every(n=>n.nodeName==="SELECTEDCONTENT")}return!1}const Fr=Symbol("class"),Ur=Symbol("style"),Zo=Symbol("is custom element"),Qo=Symbol("is html"),rc=xn?"link":"LINK",es=xn?"input":"INPUT",ac=xn?"option":"OPTION",ts=xn?"select":"SELECT",ic=xn?"progress":"PROGRESS";function ci(e){if(F){var t=!1,n=()=>{if(!t){if(t=!0,e.hasAttribute("value")){var r=e.value;ee(e,"value",null),e.value=r}if(e.hasAttribute("checked")){var a=e.checked;ee(e,"checked",null),e.checked=a}}};e[yn]=n,Ht(n),io()}}function oc(e,t){var n=ui(e);n.value===(n.value=t??void 0)||e.value===t&&(t!==0||e.nodeName!==ic)||(e.value=t??"")}function ee(e,t,n,r){var a=ui(e);F&&(a[t]=e.getAttribute(t),t==="src"||t==="srcset"||t==="href"&&e.nodeName===rc)||a[t]!==(a[t]=n)&&(t==="loading"&&(e[Pa]=n),n==null?e.removeAttribute(t):typeof n!="string"&&rs(e).has(t)?e[t]=n:e.setAttribute(t,n))}function sc(e,t,n,r,a=!1,o=!1){F&&a&&e.nodeName===es&&("defaultValue"in n||"defaultChecked"in n||ci(e));var l=ui(e),s=l[Zo],f=!l[Qo];let u=F&&s;u&&Vt(!1);var h=t||{},m=e.nodeName===ac,g=e.nodeName===ts;for(var p in t)!(p in n)&&p[0]+p[1]!=="$$"&&(n[p]=null);n.class?n.class=Xl(n.class):n[Fr]&&(n.class=null),n[Ur]&&(n.style??(n.style=null));var b=rs(e);if(e.nodeName===es&&"type"in n&&("value"in n||"__value"in n)){var w=n.type;(w!==h.type||w===void 0&&e.hasAttribute("type"))&&(h.type=w,ee(e,"type",w))}for(const te in n){let I=n[te];if(m&&te==="value"&&I==null){e.value=e.__value="",h[te]=I;continue}if(te==="class"){var H=e.namespaceURI==="http://www.w3.org/1999/xhtml";Ql(e,H,I,r,t==null?void 0:t[Fr],n[Fr]),h[te]=I,h[Fr]=n[Fr];continue}if(te==="style"){ec(e,I,t==null?void 0:t[Ur],n[Ur]),h[te]=I,h[Ur]=n[Ur];continue}var M=h[te];if(!(I===M&&!(I===void 0&&e.hasAttribute(te)))){h[te]=I;var Q=te[0]+te[1];if(Q!=="$$")if(Q==="on"){const ge={},Je="$$"+te;let K=te.slice(2);var Se=Ol(K);if(Il(K)&&(K=K.slice(0,-7),ge.capture=!0),!Se&&M){if(I!=null)continue;e.removeEventListener(K,h[Je],ge),h[Je]=null}if(Se)ha(K,e,I),va([K]);else if(I!=null){let Ue=function(At){h[te].call(this,At)};h[Je]=Uo(K,e,Ue,ge)}}else if(te==="style")ee(e,te,I);else if(te==="autofocus")ul(e,!!I);else if(!s&&(te==="__value"||te==="value"&&I!=null))e.value=e.__value=I;else if(te==="selected"&&m)Wo(e,I);else{var J=te;f||(J=Ml(J));var Xe=J==="defaultValue"||J==="defaultChecked";if(g&&J==="defaultValue")continue;if(I==null&&!s&&!Xe)if(l[te]=null,J==="value"||J==="checked"){let ge=e;const Je=t===void 0;if(J==="value"){let K=ge.defaultValue;ge.removeAttribute(J),ge.defaultValue=K,ge.value=ge.__value=Je?K:null}else{let K=ge.defaultChecked;ge.removeAttribute(J),ge.defaultChecked=K,ge.checked=Je?K:!1}}else e.removeAttribute(te);else Xe||(s||typeof I!="string")&&b.has(J)?(e[J]=I,J in l&&(l[J]=Oe)):typeof I!="function"&&ee(e,J,I)}}}return u&&Vt(!0),h}function ba(e,t,n=[],r=[],a=[],o,l=!1,s=!1){oo(a,n,r,f=>{var u=void 0,h={},m=e.nodeName===ts,g=!1;if(Ro(()=>{var b=t(...f.map(i)),w=sc(e,u,b,o,l,s);if(g&&m){var H=e;"defaultValue"in b&&Xo(H,b.defaultValue),"value"in b&&si(H,b.value)}for(let Q of Object.getOwnPropertySymbols(h))b[Q]||qe(h[Q]);for(let Q of Object.getOwnPropertySymbols(b)){var M=b[Q];Q.description===el&&(!u||M!==u[Q])&&(h[Q]&&qe(h[Q]),h[Q]=Wt(()=>Yl(e,()=>M))),w[Q]=M}u=w}),m){var p=e;Wa(()=>{var b=u;"defaultValue"in b&&Xo(p,b.defaultValue),si(p,b.value,!0),tc(p)})}g=!0})}function ui(e){return e[er]??(e[er]={[Zo]:e.nodeName.includes("-"),[Qo]:e.namespaceURI===Ki})}var ns=new Map;function rs(e){var t=e.getAttribute("is")||e.nodeName,n=ns.get(t);if(n)return n;ns.set(t,n=new Set);for(var r,a=e,o=Element.prototype;o!==a;){r=ot(a);for(var l in r)r[l].set&&l!=="innerHTML"&&l!=="textContent"&&l!=="innerText"&&n.add(l);a=he(a)}return n}function lc(e,t,n=t){var r=new WeakSet;fl(e,"input",async a=>{var o=a?e.defaultValue:e.value;if(o=fi(e)?di(o):o,n(o),$!==null&&r.add($),await Vn(),o!==(o=t())){var l=e.selectionStart,s=e.selectionEnd,f=e.value.length;if(e.value=o??"",s!==null){var u=e.value.length;l===s&&s===f&&u>f?(e.selectionStart=u,e.selectionEnd=u):(e.selectionStart=l,e.selectionEnd=Math.min(s,u))}}}),(F&&e.defaultValue!==e.value||Mr(t)==null&&e.value)&&(n(fi(e)?di(e.value):e.value),$!==null&&r.add($)),da(()=>{var a=t();if(e===document.activeElement){var o=$;if(r.has(o))return}fi(e)&&a===di(e.value)||e.type==="date"&&!a&&!e.value||a!==e.value&&(e.value=a??"")})}function fi(e){var t=e.type;return t==="number"||t==="range"}function di(e){return e===""?null:+e}function hi(e,t){return e===t||(e==null?void 0:e[bn])===t}function An(e=La(),t,n,r){var a=Ye.r,o=U;return Wa(()=>{var l,s;return da(()=>{l=s,s=[],Mr(()=>{hi(n(...s),e)||(t(e,...s),l&&hi(n(...l),e)&&t(null,...l))})}),()=>{let f=o;for(;f!==a&&f.parent!==null&&f.parent.f&Ln;)f=f.parent;const u=()=>{s&&hi(n(...s),e)&&t(null,...s)},h=f.teardown;f.teardown=()=>{u(),h==null||h()}}}),e}const cc={get(e,t){if(!e.exclude.has(t))return e.props[t]},set(e,t){return!1},getOwnPropertyDescriptor(e,t){if(!e.exclude.has(t)&&t in e.props)return{enumerable:!0,configurable:!0,value:e.props[t]}},has(e,t){return e.exclude.has(t)?!1:t in e.props},ownKeys(e){return Reflect.ownKeys(e.props).filter(t=>!e.exclude.has(t))}};function wa(e,t,n){return new Proxy({props:e,exclude:t},cc)}function _e(e,t,n,r){var a=r,o=!0,l=()=>(o&&(o=!1,a=r),a),s;s=e[t],s===void 0&&r!==void 0&&(s=l());var f;f=()=>{var g=e[t];return g===void 0?l():(o=!0,g)};var u=!1,h=Ha(()=>(u=!1,f())),m=U;return(function(g,p){if(arguments.length>0){const b=p?i(h):g;return C(h,b),u=!0,a!==void 0&&(a=b),g}return fn&&u||(m.f&Le)!==0?h.v:i(h)})}function uc(e){return new fc(e)}class fc{constructor(t){j(this,gn);j(this,Ct);var o;var n=new Map,r=(l,s)=>{var f=mo(s,!1,!1);return n.set(l,f),f};const a=new Proxy({...t.props||{},$$events:{}},{get(l,s){return i(n.get(s)??r(s,Reflect.get(l,s)))},has(l,s){return s===aa?!0:(i(n.get(s)??r(s,Reflect.get(l,s))),Reflect.has(l,s))},set(l,s,f){return C(n.get(s)??r(s,f),f),Reflect.set(l,s,f)}});L(this,Ct,(t.hydrate?Vl:zo)(t.component,{target:t.target,anchor:t.anchor,props:a,context:t.context,intro:t.intro??!1,recover:t.recover,transformError:t.transformError})),(!((o=t==null?void 0:t.props)!=null&&o.$$host)||t.sync===!1)&&ue(),L(this,gn,a.$$events);for(const l of Object.keys(d(this,Ct)))l==="$set"||l==="$destroy"||l==="$on"||P(this,l,{get(){return d(this,Ct)[l]},set(s){d(this,Ct)[l]=s},enumerable:!0});d(this,Ct).$set=l=>{Object.assign(a,l)},d(this,Ct).$destroy=()=>{zl(d(this,Ct))}}$set(t){d(this,Ct).$set(t)}$on(t,n){d(this,gn)[t]=d(this,gn)[t]||[];const r=(...a)=>n.call(this,...a);return d(this,gn)[t].push(r),()=>{d(this,gn)[t]=d(this,gn)[t].filter(a=>a!==r)}}$destroy(){d(this,Ct).$destroy()}}gn=new WeakMap,Ct=new WeakMap;let as=class{};typeof HTMLElement=="function"&&(as=class extends HTMLElement{constructor(t,n,r){super();N(this,"$$ctor");N(this,"$$s");N(this,"$$c");N(this,"$$cn",!1);N(this,"$$d",{});N(this,"$$r",!1);N(this,"$$p_d",{});N(this,"$$l",{});N(this,"$$l_u",new Map);N(this,"$$me");N(this,"$$shadowRoot",null);this.$$ctor=t,this.$$s=n,r&&(this.$$shadowRoot=this.attachShadow(r))}addEventListener(t,n,r){if(this.$$l[t]=this.$$l[t]||[],this.$$l[t].push(n),this.$$c){const a=this.$$c.$on(t,n);this.$$l_u.set(n,a)}super.addEventListener(t,n,r)}removeEventListener(t,n,r){if(super.removeEventListener(t,n,r),this.$$c){const a=this.$$l_u.get(n);a&&(a(),this.$$l_u.delete(n))}}async connectedCallback(){if(this.$$cn=!0,!this.$$c){let t=function(a){return o=>{const l=Da("slot");a!=="default"&&(l.name=a),X(o,l)}};if(await Promise.resolve(),!this.$$cn||this.$$c)return;const n={},r=dc(this);for(const a of this.$$s)a in r&&(a==="default"&&!this.$$d.children?(this.$$d.children=t(a),n.default=!0):n[a]=t(a));for(const a of this.attributes){const o=this.$$g_p(a.name);o in this.$$d||(this.$$d[o]=ya(o,a.value,this.$$p_d,"toProp"))}for(const a in this.$$p_d)!(a in this.$$d)&&this[a]!==void 0&&(this.$$d[a]=this[a],delete this[a]);this.$$c=uc({component:this.$$ctor,target:this.$$shadowRoot||this,props:{...this.$$d,$$slots:n,$$host:this}}),this.$$me=kl(()=>{da(()=>{var a;this.$$r=!0;for(const o of Me(this.$$c)){if(!((a=this.$$p_d[o])!=null&&a.reflect))continue;this.$$d[o]=this.$$c[o];const l=ya(o,this.$$d[o],this.$$p_d,"toAttribute");l==null?this.removeAttribute(this.$$p_d[o].attribute||o):this.setAttribute(this.$$p_d[o].attribute||o,l)}this.$$r=!1})});for(const a in this.$$l)for(const o of this.$$l[a]){const l=this.$$c.$on(a,o);this.$$l_u.set(o,l)}this.$$l={}}}attributeChangedCallback(t,n,r){var a;this.$$r||(t=this.$$g_p(t),this.$$d[t]=ya(t,r,this.$$p_d,"toProp"),(a=this.$$c)==null||a.$set({[t]:this.$$d[t]}))}disconnectedCallback(){this.$$cn=!1,Promise.resolve().then(()=>{!this.$$cn&&this.$$c&&(this.$$c.$destroy(),this.$$me(),this.$$c=void 0)})}$$g_p(t){return Me(this.$$p_d).find(n=>this.$$p_d[n].attribute===t||!this.$$p_d[n].attribute&&n.toLowerCase()===t)||t}});function ya(e,t,n,r){var o;const a=(o=n[e])==null?void 0:o.type;if(t=a==="Boolean"&&typeof t!="boolean"?t!=null:t,!r||!n[e])return t;if(r==="toAttribute")switch(a){case"Object":case"Array":return t==null?null:JSON.stringify(t);case"Boolean":return t?"":null;case"Number":return t??null;default:return t}else switch(a){case"Object":case"Array":return t&&JSON.parse(t);case"Boolean":return t;case"Number":return t!=null?+t:t;default:return t}}function dc(e){const t={};return e.childNodes.forEach(n=>{t[n.slot||"default"]=!0}),t}function Tn(e,t,n,r,a,o){let l=class extends as{constructor(){super(e,n,a),this.$$p_d=t}static get observedAttributes(){return Me(t).map(s=>(t[s].attribute||s).toLowerCase())}};return Me(t).forEach(s=>{P(l.prototype,s,{get(){return this.$$c&&s in this.$$c?this.$$c[s]:this.$$d[s]},set(f){var m;f=ya(s,f,t),this.$$d[s]=f;var u=this.$$c;if(u){var h=(m=V(u,s))==null?void 0:m.get;h?u[s]=f:u.$set({[s]:f})}}})}),r.forEach(s=>{P(l.prototype,s,{get(){var f;return(f=this.$$c)==null?void 0:f[s]}})}),e.element=l,l}var hc=new Set(["$$slots","$$events","$$legacy","$$host","loading"]),vc=Ee('<div class="altcha-checkbox"><input/> <svg aria-hidden="true" width="12" height="9" viewBox="0 0 12 9"><polyline points="1 5 4 8 11 1"></polyline></svg> <div class="altcha-spinner altcha-checkbox-spinner" aria-hidden="true"></div></div>');function is(e,t){ln(t,!0);let n=_e(t,"loading"),r=wa(t,hc),a;function o(){a==null||a.click()}var l={get loading(){return n()},set loading(h){n(h),ue()}},s=vc(),f=He(s);ba(f,()=>({type:"checkbox",...r}),void 0,void 0,void 0,void 0,!0),An(f,h=>a=h,()=>a);var u=ye(f,2);return Oa(2),Ne(s),We(()=>ee(s,"data-loading",n())),ha("click",u,o),X(e,s),cn(l)}va(["click"]),Tn(is,{loading:{}},[],[],{mode:"open"});var pc=new Set(["$$slots","$$events","$$legacy","$$host","loading"]),gc=Ee('<div class="altcha-checkbox-native"><input/> <div class="altcha-spinner altcha-checkbox-native-spinner"></div></div>');function os(e,t){ln(t,!0);let n=_e(t,"loading"),r=wa(t,pc);var a={get loading(){return n()},set loading(s){n(s),ue()}},o=gc(),l=He(o);return ba(l,()=>({type:"checkbox",...r}),void 0,void 0,void 0,void 0,!0),Oa(2),Ne(o),We(()=>ee(o,"data-loading",n())),X(e,o),cn(a)}Tn(os,{loading:{}},[],[],{mode:"open"});var mc=Ee('<div><a target="_blank" rel="noopener" class="altcha-logo" aria-hidden="true" tabindex="-1"><svg width="22" height="22" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M2.33955 16.4279C5.88954 20.6586 12.1971 21.2105 16.4279 17.6604C18.4699 15.947 19.6548 13.5911 19.9352 11.1365L17.9886 10.4279C17.8738 12.5624 16.909 14.6459 15.1423 16.1284C11.7577 18.9684 6.71167 18.5269 3.87164 15.1423C1.03163 11.7577 1.4731 6.71166 4.8577 3.87164C8.24231 1.03162 13.2883 1.4731 16.1284 4.8577C16.9767 5.86872 17.5322 7.02798 17.804 8.2324L19.9522 9.01429C19.7622 7.07737 19.0059 5.17558 17.6604 3.57212C14.1104 -0.658624 7.80283 -1.21043 3.57212 2.33956C-0.658625 5.88958 -1.21046 12.1971 2.33955 16.4279Z" fill="currentColor"></path><path d="M3.57212 2.33956C1.65755 3.94607 0.496389 6.11731 0.12782 8.40523L2.04639 9.13961C2.26047 7.15832 3.21057 5.25375 4.8577 3.87164C8.24231 1.03162 13.2883 1.4731 16.1284 4.8577L13.8302 6.78606L19.9633 9.13364C19.7929 7.15555 19.0335 5.20847 17.6604 3.57212C14.1104 -0.658624 7.80283 -1.21043 3.57212 2.33956Z" fill="currentColor"></path><path d="M7 10H5C5 12.7614 7.23858 15 10 15C12.7614 15 15 12.7614 15 10H13C13 11.6569 11.6569 13 10 13C8.3431 13 7 11.6569 7 10Z" fill="currentColor"></path></svg></a></div>');function vi(e,t){ln(t,!0);let n=_e(t,"strings");const r="https://altcha.org";var a={get strings(){return n()},set strings(s){n(s),ue()}},o=mc(),l=He(o);return ee(l,"href",r),Ne(o),We(()=>ee(l,"aria-label",n().ariaLinkLabel)),X(e,o),cn(a)}Tn(vi,{strings:{}},[],[],{mode:"open"});var bc=Ee('<div class="altcha-footer"><p></p> <!></div>');function pi(e,t){ln(t,!0);let n=_e(t,"logo"),r=_e(t,"strings");var a={get logo(){return n()},set logo(u){n(u),ue()},get strings(){return r()},set strings(u){r(u),ue()}},o=bc(),l=He(o);Ko(l,()=>r().footer,!0),Ne(l);var s=ye(l,2);{var f=u=>{vi(u,{get strings(){return r()}})};Fe(s,u=>{n()&&u(f)})}return Ne(o),X(e,o),cn(a)}Tn(pi,{logo:{},strings:{}},[],[],{mode:"open"});var wc=new Set(["$$slots","$$events","$$legacy","$$host","loading"]),yc=Ee('<div class="altcha-switch"><input/>  <div class="altcha-switch-toggle"><div class="altcha-spinner altcha-switch-spinner"></div></div></div>');function ss(e,t){ln(t,!0);let n=_e(t,"loading"),r=wa(t,wc),a;function o(){a==null||a.click()}var l={get loading(){return n()},set loading(h){n(h),ue()}},s=yc(),f=He(s);ba(f,()=>({type:"checkbox",...r}),void 0,void 0,void 0,void 0,!0),An(f,h=>a=h,()=>a);var u=ye(f,2);return Ne(s),We(()=>ee(s,"data-loading",n())),ha("click",u,o),X(e,s),cn(l)}va(["click"]),Tn(ss,{loading:{}},[],[],{mode:"open"});var Ge=(e=>(e.ERROR="error",e.LOADING="loading",e.PLAYING="playing",e.PAUSED="paused",e.READY="ready",e))(Ge||{}),ls=(e=>(e.SHA_256="SHA-256",e.SHA_384="SHA-384",e.SHA_512="SHA-512",e))(ls||{}),ne=(e=>(e.CODE="code",e.ERROR="error",e.VERIFIED="verified",e.VERIFYING="verifying",e.UNVERIFIED="unverified",e.EXPIRED="expired",e))(ne||{}),_c=Ee('<div class="altcha-code-challenge-title"> </div>'),xc=Ee('<div class="altcha-spinner"></div>'),kc=ni('<svg aria-hidden="true" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M12.8659 3.00017L22.3922 19.5002C22.6684 19.9785 22.5045 20.5901 22.0262 20.8662C21.8742 20.954 21.7017 21.0002 21.5262 21.0002H2.47363C1.92135 21.0002 1.47363 20.5525 1.47363 20.0002C1.47363 19.8246 1.51984 19.6522 1.60761 19.5002L11.1339 3.00017C11.41 2.52187 12.0216 2.358 12.4999 2.63414C12.6519 2.72191 12.7782 2.84815 12.8659 3.00017ZM10.9999 16.0002V18.0002H12.9999V16.0002H10.9999ZM10.9999 9.00017V14.0002H12.9999V9.00017H10.9999Z"></path></svg>'),Ec=ni('<svg aria-hidden="true" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M15 7C15 6.44772 15.4477 6 16 6C16.5523 6 17 6.44772 17 7V17C17 17.5523 16.5523 18 16 18C15.4477 18 15 17.5523 15 17V7ZM7 7C7 6.44772 7.44772 6 8 6C8.55228 6 9 6.44772 9 7V17C9 17.5523 8.55228 18 8 18C7.44772 18 7 17.5523 7 17V7Z"></path></svg>'),Sc=ni('<svg aria-hidden="true" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M4 12H7C8.10457 12 9 12.8954 9 14V19C9 20.1046 8.10457 21 7 21H4C2.89543 21 2 20.1046 2 19V12C2 6.47715 6.47715 2 12 2C17.5228 2 22 6.47715 22 12V19C22 20.1046 21.1046 21 20 21H17C15.8954 21 15 20.1046 15 19V14C15 12.8954 15.8954 12 17 12H20C20 7.58172 16.4183 4 12 4C7.58172 4 4 7.58172 4 12Z"></path></svg>'),Cc=Ee('<button type="button" class="altcha-button altcha-button-secondary"><!></button>'),Ac=Ee('<audio hidden="" autoplay=""></audio>'),Tc=Ee('<div class="altcha-code-challenge"><form data-code-challenge="true"><!> <div class="altcha-code-challenge-text"> </div> <img class="altcha-code-challenge-image" alt=""/> <div class="altcha-code-challenge-row"><input type="text" class="altcha-input" autocomplete="off" name="" required=""/> <!> <button type="button" class="altcha-button altcha-button-secondary"><svg aria-hidden="true" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="M2 12C2 17.5228 6.47715 22 12 22C17.5228 22 22 17.5228 22 12C22 6.47715 17.5228 2 12 2V4C16.4183 4 20 7.58172 20 12C20 16.4183 16.4183 20 12 20C7.58172 20 4 16.4183 4 12C4 9.25022 5.38734 6.82447 7.50024 5.38451L7.5 8H9.5V2L3.5 2V4L5.99918 3.99989C3.57075 5.82434 2 8.72873 2 12Z"></path></svg></button></div> <div class="altcha-code-challenge-buttons"><button type="submit" class="altcha-button"> </button> <button type="button" class="altcha-button altcha-button-secondary"> </button></div></form> <!></div>');function cs(e,t){ln(t,!0);let n=_e(t,"audioUrl"),r=_e(t,"codeChallenge"),a=_e(t,"config"),o=_e(t,"imageUrl"),l=_e(t,"onCancel"),s=_e(t,"onReload"),f=_e(t,"onSubmit"),u=_e(t,"strings"),h=W(void 0),m=W(void 0),g=W(void 0),p=W(!1),b=W(""),w=W(!1);ai(()=>(a().disableAutoFocus||Vn().then(()=>{var S;(S=i(g))==null||S.focus()}),()=>{i(m)&&(i(m).pause(),C(m,void 0))}));function H(){C(h,Ge.PAUSED,!0)}function M(S){C(h,Ge.ERROR,!0)}function Q(){C(h,Ge.READY,!0)}function Se(){C(h,Ge.LOADING,!0)}function J(){C(h,Ge.PLAYING,!0)}function Xe(){C(h,Ge.PAUSED,!0)}function te(S){var re;S.code==="Space"?(S.preventDefault(),S.stopPropagation(),Je()):S.code==="Escape"&&(S.preventDefault(),S.stopPropagation(),(re=l())==null||re())}function I(S){var re;S.preventDefault(),S.stopPropagation(),(re=f())==null||re(i(b))}function ge(S){S.play().catch(re=>{if(!(re instanceof DOMException&&re.name==="AbortError"))throw re})}function Je(){i(m)?i(h)===Ge.LOADING||(i(m).paused?(n()&&i(m).src!==n()&&(i(m).src=n()),i(m).currentTime=0,ge(i(m))):i(m).pause()):(C(w,!0),requestAnimationFrame(()=>{i(m)&&n()&&(i(m).src=n(),ge(i(m)))}))}var K={get audioUrl(){return n()},set audioUrl(S){n(S),ue()},get codeChallenge(){return r()},set codeChallenge(S){r(S),ue()},get config(){return a()},set config(S){a(S),ue()},get imageUrl(){return o()},set imageUrl(S){o(S),ue()},get onCancel(){return l()},set onCancel(S){l(S),ue()},get onReload(){return s()},set onReload(S){s(S),ue()},get onSubmit(){return f()},set onSubmit(S){f(S),ue()},get strings(){return u()},set strings(S){u(S),ue()}},Ue=Tc(),At=He(Ue),ft=He(At);{var Tt=S=>{var re=_c(),Xn=or(re,!0);We(()=>Xt(Xn,u().verificationRequired)),X(S,re)};Fe(ft,S=>{a().codeChallengeDisplay!=="standard"&&S(Tt)})}var xe=ye(ft,2),mn=or(xe,!0),A=ye(xe,2),Ve=ye(A,2),fe=He(Ve);ci(fe),fe.disabled=i(p),An(fe,S=>C(g,S),()=>i(g));var _=ye(fe,2);{var qr=S=>{var re=Cc(),Xn=He(re);{var Jr=at=>{var en=xc();X(at,en)},yi=at=>{var en=kc();X(at,en)},_i=at=>{var en=Ec();X(at,en)},xi=at=>{var en=Sc();X(at,en)};Fe(Xn,at=>{i(h)===Ge.LOADING?at(Jr):i(h)===Ge.ERROR?at(yi,1):i(h)===Ge.PLAYING?at(_i,2):at(xi,-1)})}Ne(re),We(()=>{ee(re,"title",u().getAudioChallenge),re.disabled=i(h)===Ge.LOADING||i(h)===Ge.ERROR,ee(re,"aria-label",i(h)===Ge.LOADING?u().loading:u().getAudioChallenge)}),De("click",re,()=>Je(),!0),X(S,re)};Fe(_,S=>{r().audio&&S(qr)})}var Gr=ye(_,2);Ne(Ve);var Qt=ye(Ve,2),Yr=He(Qt),Ea=or(Yr,!0),Wn=ye(Yr,2),Wr=or(Wn,!0);Ne(Qt),Ne(At);var Xr=ye(At,2);{var dt=S=>{var re=Ac();An(re,Xn=>C(m,Xn),()=>i(m)),De("error",re,M),De("loadstart",re,Se),De("canplay",re,Q),De("pause",re,Xe),De("playing",re,J),De("ended",re,H),X(S,re)};Fe(Xr,S=>{i(w)&&S(dt)})}return Ne(Ue),We(()=>{Xt(mn,u().enterCodeFromImage),ee(A,"src",o()),ee(fe,"minlength",r().length||1),ee(fe,"maxlength",r().length),ee(fe,"placeholder",u().enterCode),ee(fe,"aria-label",i(h)===Ge.LOADING?u().loading:i(h)===Ge.PLAYING?"":u().enterCodeAria),ee(fe,"aria-live",i(h)?"assertive":"polite"),ee(fe,"aria-busy",i(h)===Ge.LOADING),ee(Gr,"title",u().reload),ee(Gr,"aria-label",u().reload),ee(Yr,"aria-label",u().verify),Xt(Ea,u().verify),ee(Wn,"aria-label",u().cancel),Xt(Wr,u().cancel)}),De("submit",At,I,!0),ha("keydown",fe,te),lc(fe,()=>i(b),S=>C(b,S)),De("click",Gr,()=>{var S;return(S=s())==null?void 0:S()},!0),De("click",Wn,()=>{var S;return(S=l())==null?void 0:S()},!0),X(e,Ue),cn(K)}va(["keydown"]),Tn(cs,{audioUrl:{},codeChallenge:{},config:{},imageUrl:{},onCancel:{},onReload:{},onSubmit:{},strings:{}},[],[],{mode:"open"});var $c=new Set(["$$slots","$$events","$$legacy","$$host","anchor","children","display","backdrop","onClickOutside","onClickOutsideDelay","onClose","placement","updateUISignal","variant"]),Rc=Ee('<div class="altcha-popover-backdrop" data-backdrop=""></div>'),Pc=Ee('<div class="altcha-popover-arrow"></div>'),Ic=Ee('<div role="button" class="altcha-popover-close">&times;</div>'),Lc=Ee('<!> <div><!> <!> <div class="altcha-popover-content"><!></div></div>',1);function gi(e,t){ln(t,!0);let n=_e(t,"anchor"),r=_e(t,"children"),a=_e(t,"display",7,"standard"),o=_e(t,"backdrop",7,!1),l=_e(t,"onClickOutside"),s=_e(t,"onClickOutsideDelay",7,600),f=_e(t,"onClose"),u=_e(t,"placement",7,"auto"),h=_e(t,"updateUISignal"),m=_e(t,"variant",7,"neutral"),g=wa(t,$c),p=W(void 0),b=W(void 0),w=W(!1),H=W(0);ct(()=>{u()!=="auto"&&C(w,u()==="top")}),ct(()=>{h()&&Xe()}),ai(()=>{const A=a()==="bottomsheet"||a()==="overlay";return A&&(i(b)&&document.body.append(i(b)),i(p)&&document.body.append(i(p))),Xe(),Vn().then(()=>{C(H,Date.now(),!0)}),()=>{A&&(i(b)&&document.body.removeChild(i(b)),i(p)&&document.body.removeChild(i(p)))}});function M(){var A;(A=f())==null||A()}function Q(A){var fe,_;const Ve=A.target;!((fe=i(p))!=null&&fe.contains(Ve))&&(!s()||i(H)+s()<Date.now())&&((_=l())==null||_())}function Se(){Xe()}function J(){Xe()}function Xe(){if(n()&&u()==="auto"&&i(p)){const A=n().getBoundingClientRect(),fe=document.documentElement.clientHeight-(A.top+A.height)<i(p).clientHeight;i(w)!==fe&&C(w,fe)}}var te={get anchor(){return n()},set anchor(A){n(A),ue()},get children(){return r()},set children(A){r(A),ue()},get display(){return a()},set display(A="standard"){a(A),ue()},get backdrop(){return o()},set backdrop(A=!1){o(A),ue()},get onClickOutside(){return l()},set onClickOutside(A){l(A),ue()},get onClickOutsideDelay(){return s()},set onClickOutsideDelay(A=600){s(A),ue()},get onClose(){return f()},set onClose(A){f(A),ue()},get placement(){return u()},set placement(A="auto"){u(A),ue()},get updateUISignal(){return h()},set updateUISignal(A){h(A),ue()},get variant(){return m()},set variant(A="neutral"){m(A),ue()}},I=Lc();De("click",Fn,Q,!0),De("resize",Fn,Se),De("scroll",Fn,J);var ge=ir(I);{var Je=A=>{var Ve=Rc();An(Ve,fe=>C(b,fe),()=>i(b)),X(A,Ve)};Fe(ge,A=>{o()&&A(Je)})}var K=ye(ge,2);ba(K,()=>({...g,class:`altcha-popover ${(t.class||"")??""}`,"data-popover":!0,"data-variant":m(),"data-top":i(w),"data-display":a()}));var Ue=He(K);{var At=A=>{var Ve=Pc();X(A,Ve)};Fe(Ue,A=>{a()==="standard"&&A(At)})}var ft=ye(Ue,2);{var Tt=A=>{var Ve=Ic();De("click",Ve,M,!0),X(A,Ve)};Fe(ft,A=>{a()!=="standard"&&A(Tt)})}var xe=ye(ft,2),mn=He(xe);return Bl(mn,()=>r()??Pe),Ne(xe),Ne(K),An(K,A=>C(p,A),()=>i(p)),X(e,I),cn(te)}Tn(gi,{anchor:{},children:{},display:{},backdrop:{},onClickOutside:{},onClickOutsideDelay:{},onClose:{},placement:{},updateUISignal:{},variant:{}},[],[],{mode:"open"});function Oc(e){return Array.from(new Uint8Array(e)).map(t=>t.toString(16).padStart(2,"0")).join("")}function Nc(e,t="altcha-css",n){var r,a;if(typeof document<"u"&&document&&!document.getElementById(t)){const o=document.createElement("style");o.id=t,o.textContent=e;const l=((r=document.currentScript)==null?void 0:r.nonce)??((a=document.querySelector('meta[name="csp-nonce"]'))==null?void 0:a.content);l&&(o.nonce=l),document.head.appendChild(o)}}Object.values(ls);async function us(e){var g;const{challenge:t,concurrency:n=navigator.hardwareConcurrency,controller:r=new AbortController,createWorker:a,onOutOfMemory:o=p=>p>1?Math.floor(p/2):0,counterMode:l,timeout:s}=e,f=Math.min(16,Math.max(1,n)),u=[],h=()=>{for(const p of u)p.terminate()};for(let p=0;p<f;p++)u.push(await a(t.parameters.algorithm));let m=null;try{m=await Promise.race(u.map((p,b)=>(r.signal.addEventListener("abort",()=>{p.postMessage({type:"abort"})}),new Promise((w,H)=>{p.addEventListener("error",M=>{H(M)}),p.addEventListener("message",M=>{if(M.data){for(const Q of u)Q!==p&&Q.postMessage({type:"abort"});if(M.data.error)return H(new Error(M.data.error))}w(M.data)}),p.postMessage({challenge:t,counterMode:l,counterStart:b,counterStep:f,timeout:s,type:"work"})}))))}catch(p){if(p instanceof Error&&!!((g=p==null?void 0:p.message)!=null&&g.includes("Out of memory"))&&o){h();const w=o(f);if(w)return us({...e,challenge:t,controller:r,concurrency:w,createWorker:a})}throw p}finally{h()}return r.signal.aborted?null:m||null}class Mc{constructor(t={}){N(this,"TAG_CODES",{INPUT:1,TEXTAREA:2,SELECT:3,BUTTON:4,A:5,DETAILS:6,SUMMARY:7,IFRAME:8,VIDEO:9,AUDIO:10});N(this,"maxSamples");N(this,"sampleInterval");N(this,"target");N(this,"focusStartTime",0);N(this,"focusInteraction",0);N(this,"focusInteractionTimer",null);N(this,"lastPointerSample",0);N(this,"lastTouchSample",0);N(this,"lastScrollSample",0);N(this,"pendingPointer",null);N(this,"pendingTouch",null);N(this,"focus",[]);N(this,"pointer",[]);N(this,"scroll",[]);N(this,"touch",[]);N(this,"onFocus",t=>{if(this.focusInteraction===2)return;const n=t.target;if(!(n instanceof Element))return;const r=performance.now();this.focusStartTime===0&&(this.focusStartTime=r),this.focus.push([Math.round(r-this.focusStartTime),n.tabIndex,this.TAG_CODES[n.tagName]??0,this.focusInteraction?1:0]),this.evict(this.focus)});N(this,"onInteraction",t=>{this.focusInteraction="keyCode"in t?1:2,this.focusInteractionTimer&&clearTimeout(this.focusInteractionTimer),this.focusInteractionTimer=setTimeout(()=>{this.focusInteraction=0},100)});N(this,"onPointer",t=>{if(t.pointerType==="touch")return;const n=t.timeStamp||performance.now();this.pendingPointer=[Math.round(t.clientX),Math.round(t.clientY),Math.round(n)],n-this.lastPointerSample>=this.sampleInterval&&(this.pointer.push(this.pendingPointer),this.lastPointerSample=n,this.pendingPointer=null,this.evict(this.pointer))});N(this,"onScroll",()=>{const t=performance.now();t-this.lastScrollSample<this.sampleInterval||(this.scroll.push([Math.round(window.scrollY),Math.round(t)]),this.lastScrollSample=t,this.evict(this.scroll))});N(this,"onTouchMove",t=>{const n=t.timeStamp||performance.now(),r=t.touches[0];r&&(this.pendingTouch=[Math.round(r.clientX),Math.round(r.clientY),Math.round(n),Math.round(r.force*1e3)/1e3,Math.round(r.radiusX||0),Math.round(r.radiusY||0)],n-this.lastTouchSample>=this.sampleInterval&&(this.touch.push(this.pendingTouch),this.lastTouchSample=n,this.pendingTouch=null,this.evict(this.touch)))});const{maxSamples:n=60,sampleInterval:r=50,target:a=window}=t;this.maxSamples=n,this.sampleInterval=r,this.target=a,this.attach()}destroy(){const t={capture:!0};this.target.removeEventListener("focusin",this.onFocus,t),this.target.removeEventListener("keydown",this.onInteraction,t),this.target.removeEventListener("pointerdown",this.onInteraction,t),this.target.removeEventListener("pointermove",this.onPointer,t),this.target.removeEventListener("scroll",this.onScroll,t),this.target.removeEventListener("touchmove",this.onTouchMove,t)}export(){return{focus:this.focus,maxTouchPoints:navigator.maxTouchPoints||0,pointer:this.pointer,scroll:this.scroll,time:Date.now(),touch:this.touch}}attach(){const t={passive:!0,capture:!0};this.target.addEventListener("focusin",this.onFocus,t),this.target.addEventListener("keydown",this.onInteraction,t),this.target.addEventListener("pointerdown",this.onInteraction,t),this.target.addEventListener("pointermove",this.onPointer,t),this.target.addEventListener("scroll",this.onScroll,t),this.target.addEventListener("touchmove",this.onTouchMove,t)}evict(t){t.length>this.maxSamples&&t.splice(0,t.length-this.maxSamples)}}var jc=Ee('<div class="altcha-overlay-backdrop" data-backdrop=""></div>'),Dc=Ee('<div class="altcha-overlay-content"></div>'),Fc=Ee('<div role="button" class="altcha-overlay-close">&times;</div> <!>',1),Uc=Ee('<div class="altcha-floating-arrow"></div>'),Hc=Ee('<input type="hidden"/>'),Vc=Ee('<div class="altcha-error">Secure context (HTTPS) required.</div>'),fs=Ee('<div class="altcha-error"> </div>'),zc=Ee("<!> <!>",1),Bc=Ee('<!> <div class="altcha"><!> <div class="altcha-main"><div><div class="altcha-checkbox-wrap"><!> <label><!></label></div> <!></div> <!> <!> <!></div> <!></div>',1);function Kc(e,t){ln(t,!0);const n=()=>ro(h,"$altchaDefaults",a),r=()=>ro(b,"$altchaI18nStore",a),[a,o]=cl(),l='input[type="text"]:not([data-no-spamfilter]), textarea:not([data-no-spamfilter])',s='input[type="submit"], button[type="submit"], button:not([type="button"]):not([type="reset"])',f=["ar","fa","he","ur"],{isSecureContext:u}=globalThis,{store:h}=globalThis.$altcha.defaults,m=navigator.hardwareConcurrency||2,g=navigator.deviceMemory||0,p=g&&g<=4?Math.min(4,m):m,b=globalThis.$altcha.i18n.store,w=t.$$host,H=(c,v)=>{Vn().then(()=>{w==null||w.dispatchEvent(new CustomEvent(c,{detail:v}))})};let M=null,Q=W(un(new URL(location.origin))),Se=W(!1),J=W(null),Xe=W(null),te=W(null),I=W(un(ne.UNVERIFIED)),ge=W(void 0),Je=W(void 0),K=W(null),Ue=W(void 0),At=W(null),ft=W(null),Tt=W(null),xe=W(null),mn=W(un([])),A=W(0),Ve=W(un({})),fe=W(!0);const _=tt(()=>({fetch:(c,v)=>fetch(c,v),audioChallengeLanguage:"",auto:"off",barPlacement:"bottom",challenge:"",codeChallenge:null,codeChallengeDisplay:"standard",credentials:null,debug:!1,disableAutoFocus:!1,display:"standard",floatingAnchor:"",floatingOffset:8,floatingPersist:!1,floatingPlacement:"auto",hideFooter:!1,hideLogo:!1,humanInteractionSignature:!0,language:"",mockError:!1,minDuration:500,overlayContent:"",name:"altcha",popoverPlacement:"auto",retryOnOutOfMemoryError:!0,setCookie:null,serverVerificationFields:!1,serverVerificationTimeZone:!1,test:!1,timeout:9e4,type:"checkbox",validationMessage:"",verifyFunction:null,verifyUrl:"",workers:p,...n(),...i(Ve)})),qr=tt(()=>`altcha-checkbox-${t.id||Math.floor(Math.random()*1e12).toString(16)}`),Gr=tt(()=>Yc(i(_).type)),Qt=tt(()=>i(_).auto),Yr=tt(()=>i(I)===ne.VERIFYING),Ea=tt(()=>!i(_).hideFooter),Wn=tt(()=>!i(_).hideLogo&&i(_).display!=="bar"),Wr=tt(()=>Wc(r(),[i(_).language,document.documentElement.lang,...navigator.languages])),Xr=tt(()=>f.includes(i(Wr).language)?"rtl":void 0),dt=tt(()=>({...i(Wr).strings})),S=tt(()=>{var c,v,x;return(v=(c=i(J))==null?void 0:c.audio)!=null&&v.match(/^(https?:)?\//)?Sa(i(J).audio,i(Q),{language:i(_).audioChallengeLanguage||i(Wr).language}).toString():(x=i(J))==null?void 0:x.audio}),re=tt(()=>{var c,v,x;return(v=(c=i(J))==null?void 0:c.image)!=null&&v.match(/^(https?:)?\//)?Sa(i(J).image,i(Q)):(x=i(J))==null?void 0:x.image});ct(()=>{Zr({auto:t.auto,challenge:t.challenge,display:t.display,language:t.language,name:t.name,type:t.type,workers:t.workers})}),ct(()=>{t.theme?w==null||w.setAttribute("theme",t.theme):w==null||w.removeAttribute("theme")}),ct(()=>{if(t.configuration)try{Zr(JSON.parse(t.configuration))}catch{de("unable to parse the `configuration` attribute (JSON expected)")}}),ct(()=>{i(te)!==i(_).display&&Ca(i(_).display)}),ct(()=>{i(Se)&&i(I)===ne.VERIFYING&&C(Se,!1)}),ct(()=>{!i(Se)&&i(I)===ne.VERIFIED&&C(Se,!0)}),ct(()=>{if(!i(Se)){const c=ki();c&&c.checked&&(c.checked=!1)}}),ct(()=>{var c;i(I)===ne.VERIFIED&&((c=ki())==null||c.setCustomValidity(""))}),ct(()=>{if(i(Qt)==="onload"){const c=setTimeout(()=>{wr()},10);return()=>{c&&clearTimeout(c)}}}),ct(()=>{i(ft)&&de("error:",i(ft))}),ct(()=>{i(xe)&&i(_).setCookie&&uu(i(xe),i(_).setCookie)}),ai(()=>{var c,v;return de("mounted","3.5.0"),w&&globalThis.$altcha.instances.add(w),C(K,Jc(),!0),(c=i(K))==null||c.addEventListener("reset",_s),(v=i(K))==null||v.addEventListener("submit",xs,{capture:!0}),document.addEventListener("focusin",ys),Xn(),i(_).humanInteractionSignature&&(de("human interaction signature enabled"),M=new Mc),H("load"),u||de("secure context (HTTPS) required"),()=>{var x,y;yi(),w&&globalThis.$altcha.instances.delete(w),i(Tt)&&clearTimeout(i(Tt)),(x=i(K))==null||x.removeEventListener("reset",_s),(y=i(K))==null||y.removeEventListener("submit",xs,{capture:!0}),document.removeEventListener("focusin",ys),M==null||M.destroy()}});function Xn(){C(mn,[...globalThis.$altcha.plugins].map(c=>new c(w)),!0),de("activating plugins",i(mn).map(c=>c.constructor.name));for(const c of i(mn))c.activate()}async function Jr(c,...v){let x;for(const y of i(mn))x=await y[c].call(y,...v);return x}function yi(){for(const c of i(mn))c.destroy()}function _i(c){const[v,x]=c.salt.split("?"),y={};if(x)try{Object.assign(y,Object.fromEntries(new URLSearchParams(x).entries()))}catch{}const D={codeChallenge:c.codeChallenge,parameters:{algorithm:c.algorithm,cost:1,data:y,expiresAt:y!=null&&y.expires?parseInt(y.expires,10):void 0,keyLength:c.algorithm==="SHA-512"?64:c.algorithm==="SHA-384"?48:32,nonce:Oc(new TextEncoder().encode(c.salt)),keyPrefix:c.challenge,salt:""},signature:c.signature};return Object.defineProperties(D,{_originalSalt:{enumerable:!1,value:c.salt,writable:!1},_version:{enumerable:!1,value:1,writable:!1}}),D}function xi(c,v){return{algorithm:c.parameters.algorithm,challenge:c.parameters.keyPrefix,number:v.counter,salt:"_originalSalt"in c?c._originalSalt:c.parameters.nonce,signature:c.signature,took:v.time||0}}async function at(c){await new Promise(v=>setTimeout(v,c))}async function en(c=i(_).challenge,v){const x=await Jr("onFetchChallenge",c);let y=null;if(x!==void 0)return x;if(typeof c=="string")if(c.startsWith("{")){de("parsing JSON challenge");try{y=JSON.parse(c)}catch{throw new Error("Unable to parse JSON challenge.")}}else{de("fetching challenge from",(v==null?void 0:v.method)||"GET",c),C(Q,new URL(c,location.origin),!0);const D=await i(_).fetch(c,{credentials:i(_).credentials||void 0,...v});await Es(D);const O=D.headers.get("x-altcha-config");O&&su(O);const ae=await D.json();if(ae&&"his"in ae&&ae.his){if(de("requested HIS"),!M)throw new Error("Server requested HIS data but collector is disabled.");return en(Sa(ae.his.url,i(Q)),{body:JSON.stringify({his:M.export()}),headers:{"content-type":"application/json"},method:"POST"})}ae&&"hisResult"in ae&&ae.hisResult&&de("HIS result",ae.hisResult),y=ae}else if(c&&typeof c=="object")try{y=JSON.parse(JSON.stringify(c))}catch{throw new Error("Unable to parse JSON challenge.")}if(qc(y)&&(y=_i(y)),!Gc(y))throw new Error("Challenge validation failed.");return y}function qc(c){return c!==null&&typeof c=="object"&&"challenge"in c}function Gc(c){return!!c&&typeof c=="object"&&"parameters"in c&&!!c.parameters&&typeof c.parameters=="object"&&"algorithm"in c.parameters&&"nonce"in c.parameters&&"salt"in c.parameters&&"keyPrefix"in c.parameters}function ki(){return document.getElementById(i(qr))}function Yc(c){switch(c){case"checkbox":return is;case"switch":return ss;case"native":default:return os}}function Wc(c,v){const x=Object.keys(c).map(D=>D.toLowerCase());let y=v.reduce((D,O)=>(O=O.toLowerCase(),D||(c[O]?O:null)||x.find(ae=>O.split("-")[0]===ae.split("-")[0])||null),null);return c[y||""]||(y="en"),{language:y,strings:c[y]}}function Xc(c){switch(c){case"bar":return i(_).barPlacement||"bottom";case"floating":return i(_).floatingPlacement||"auto";default:return}}function Jc(){var c,v;if(t.form){const x=w==null?void 0:w.getRootNode(),y=(c=x==null?void 0:x.getElementById)==null?void 0:c.call(x,t.form);return y instanceof HTMLFormElement?y:null}return((v=i(Ue))==null?void 0:v.closest("form"))??null}function ws(c){return i(K)?Array.from(i(K).elements).filter(v=>v.matches(c)):[]}function Zc(c){return ws(l).reduce((x,y)=>{const D=y.name,O=y.value;return D&&O&&(x[D]=/\n/.test(O)?O.replace(new RegExp("(?<!\\r)\\n","g"),`\r
`):O),x},{})}function Qc(){try{return Intl.DateTimeFormat().resolvedOptions().timeZone}catch{}}function Sa(c,v,x){const y=new URL(c,v);if(y.search||(y.search=v.search),x)for(const D in x)x[D]!==void 0&&x[D]!==null&&y.searchParams.set(D,x[D]);return y.toString()}function eu(c){!i(Se)&&c.currentTarget.checked?(c.preventDefault(),c.currentTarget.checked=!1,i(I)!==ne.VERIFYING&&wr()):c.currentTarget.checked||(c.preventDefault(),$t())}function tu(c){i(I)===ne.VERIFYING?c.currentTarget.setCustomValidity(i(dt).waitAlert):i(_).validationMessage&&c.currentTarget.setCustomValidity(i(_).validationMessage)}function nu(){Ca(i(_).display),$t()}function ru(){Aa()}function au(c){const v=c.target;i(_).display==="floating"&&v&&!(w!=null&&w.contains(v))&&!v.hasAttribute("data-backdrop")&&!v.closest("[data-popover]")&&i(I)!==ne.VERIFIED&&!i(_).floatingPersist&&Ei()}function ys(c){const v=c.target;i(Qt)==="onfocus"&&i(I)===ne.UNVERIFIED&&i(K)&&v instanceof Element&&(i(K).contains(v)||v.form===i(K))&&wr()}function _s(){Ca(i(_).display),$t()}function xs(c){const v=c.target;(v==null?void 0:v.getAttribute("data-code-challenge"))!=="true"&&i(Qt)==="onsubmit"&&i(I)===ne.UNVERIFIED&&(c.preventDefault(),c.stopPropagation(),C(At,c.submitter,!0),Si(),wr().then(x=>{x&&!i(J)&&Vn().then(()=>{ks(i(At))})}))}function iu(c){c.persisted&&(Ca(i(_).display),$t())}function ou(){Aa()}function su(c){var v,x;try{const y=JSON.parse(c);y&&typeof y=="object"&&Zr({serverVerificationFields:(v=y==null?void 0:y.sentinel)==null?void 0:v.fields,serverVerificationTimeZone:(x=y==null?void 0:y.sentinel)==null?void 0:x.timeZone,verifyUrl:y.verifyurl,...y})}catch(y){de("unable to configure from x-altcha-config header",y)}}function lu(c=20){if(!i(Ue))return;const v=i(_).floatingPlacement;if(!i(Je)&&(C(Je,(i(_).floatingAnchor instanceof HTMLElement?i(_).floatingAnchor:i(_).floatingAnchor?document.querySelector(i(_).floatingAnchor):ws(s)[0])||i(K),!0),!i(Je))){de("unable to find floating anchor element");return}const x=parseInt(i(_).floatingOffset,10)||12,y=i(Je).getBoundingClientRect(),D=i(Ue).getBoundingClientRect(),O=document.documentElement.clientHeight,ae=document.documentElement.clientWidth,Ze=!v||v==="auto"?y.bottom+D.height+x+c>O:v==="top",me=Math.max(c,Math.min(ae-c-D.width,y.left+y.width/2-D.width/2));if(i(Ue).style.setProperty("--altcha-floating-left",`${me}px`),i(Ue).style.setProperty("--altcha-floating-top",Ze?`${y.top-(D.height+x)}px`:`${y.bottom+x}px`),i(Ue).setAttribute("data-floating-position",Ze?"top":"bottom"),i(ge)){const it=i(ge).getBoundingClientRect();i(ge).style.left=y.left-me+y.width/2-it.width/2+"px"}}async function cu(c,v){const x=await Jr("onRequestServerVerification",c,v);if(x!==void 0)return x;if(de("requesting server verification from",i(_).verifyUrl),!i(_).verifyUrl)throw new Error("Parameter verifyUrl must be set for server verification.");const y=await i(_).fetch(Sa(i(_).verifyUrl,i(Q)),{body:JSON.stringify({code:v,fields:i(_).serverVerificationFields?Zc():void 0,payload:c,timeZone:i(_).serverVerificationTimeZone?Qc():void 0}),credentials:i(_).credentials||void 0,headers:{"Content-Type":"application/json"},method:"POST"});await Es(y);const D=await y.json();return D&&typeof D=="object"&&"payload"in D&&D.payload&&H("serververification",D),D}function ks(c){var v;i(K)&&"requestSubmit"in i(K)?i(K).requestSubmit(c):(v=i(K))!=null&&v.reportValidity()&&(c?c.click():i(K).submit())}function uu(c,v={}){const{domain:x,name:y=i(_).name,maxAge:D,path:O,sameSite:ae,secure:Ze}=v;let me=`${encodeURIComponent(y)}=${encodeURIComponent(c)}`;x&&(me+=`; Domain=${x}`),D!=null&&(me+=`; Max-Age=${D}`),O&&(me+=`; Path=${O}`),ae&&(me+=`; SameSite=${ae}`),Ze&&(me+="; Secure"),document.cookie=me}function Ca(c){switch(c){case"bar":case"floating":case"overlay":Ei(),(!i(Qt)||i(Qt)==="off")&&(i(Ve).auto="onsubmit");break;case"standard":Si()}i(te)!==c&&C(te,c,!0)}function fu(c){i(Tt)&&clearTimeout(i(Tt));const v=()=>{i(I)!==ne.UNVERIFIED?(C(Se,!1),Rt(ne.EXPIRED)):$t(),H("expired")},x=c*1e3-Date.now();x>=1?C(Tt,setTimeout(v,x),!0):v()}async function Es(c){var x;if(c.status>=400){if((x=c.headers.get("content-type"))!=null&&x.includes("/json")){let y;try{y=await c.json()}catch{}if(y&&"error"in y)throw new Error(`Server responded with ${c.status} - ${y.error}`)}throw new Error(`Server responded with ${c.status}.`)}const v=c.headers.get("content-type");if(!v||!v.includes("/json"))throw new Error(`Server responded with invalid content-type. Expected application/json, received ${v}.`)}async function Ss(c){var x;if(!i(xe)){Rt(ne.ERROR,"Cannot verify code challenge without PoW payload.");return}Rt(ne.VERIFYING);let v=null;if(i(_).verifyUrl)v=await cu(i(xe),c);else if(i(_).verifyFunction)v=await i(_).verifyFunction(i(xe),c);else{Rt(ne.ERROR,"Parameter verifyUrl is required for code challenge verification.");return}v!=null&&v.payload&&(C(xe,v.payload,!0),de("server payload",i(xe))),(v==null?void 0:v.verified)===!0?(de("verified"),Rt(ne.VERIFIED),H("verified",{payload:i(xe)}),i(Qt)==="onsubmit"&&Vn().then(()=>{ks(i(At))})):Rt(ne.ERROR,(v==null?void 0:v.reason)||"Verification failed."),i(_).disableAutoFocus||(x=ki())==null||x.focus()}function Zr(c){Object.assign(i(Ve),{...Object.fromEntries(Object.entries(c).filter(([v,x])=>x!==void 0))})}function du(){return{...i(_)}}function hu(){return i(I)}function Ei(){C(fe,!1)}function de(...c){(i(_).debug||c.some(v=>v instanceof Error))&&console[c[0]instanceof Error?"error":"log"]("ALTCHA",`[name=${i(_).name}]`,...c)}function $t(c=ne.UNVERIFIED,v=null){C(Se,!1),C(ft,v,!0),C(xe,null),i(Xe)&&i(Xe).abort(),i(Tt)&&(clearTimeout(i(Tt)),C(Tt,null)),Rt(c)}function Rt(c,v=null){C(I,c,!0),C(ft,v,!0),H("statechange",{payload:i(xe),state:i(I)})}function Si(){C(fe,!0),Vn().then(()=>{Aa()})}function Aa(){switch(i(_).display){case"floating":return lu()}C(A,i(A)+1)}async function wr(c={}){var it,Pn;const{concurrency:v=Math.max(1,i(_).workers),controller:x=new AbortController,minDuration:y=i(_).minDuration}=c,D=performance.now();let O=null,ae=null,Ze=!1;const me=await Jr("onVerify",c);if(me!==void 0)return me;$t(ne.VERIFYING),C(Xe,x,!0);try{if(!u)throw new Error("Secure context (HTTPS) required.");if(i(_).mockError)throw new Error("Mock error.");if(i(_).test)return de("running test mode with null challenge"),await at(Math.max(0,y-(performance.now()-D))),(it=i(Xe))!=null&&it.signal.aborted?($t(),null):(C(xe,btoa(JSON.stringify({challenge:null,solution:null,test:!0})),!0),de("verified"),Rt(ne.VERIFIED),H("verified",{payload:i(xe)}),{payload:i(xe)});if(O=await en(),!O)throw new Error("Failed to fetch challenge.");de("challenge",O),"configuration"in O&&(de("re-configuring from challenge",O.configuration),Zr(O.configuration)),O.parameters.expiresAt&&fu(O.parameters.expiresAt),Ze="_version"in O&&O._version===1;const Ft=globalThis.$altcha.algorithms.get(O.parameters.algorithm);if(!Ft)throw new Error(`Unsupported algorithm ${O.parameters.algorithm}.`);if(ae=await us({challenge:O,concurrency:v,controller:x,createWorker:Ft,counterMode:Ze?"string":"uint32",onOutOfMemory:Is=>{if(de("out of memory error received"),H("outofmemory"),i(_).retryOnOutOfMemoryError&&Is>1){const Ls=Math.floor(Is/2);return de(`retrying with ${Ls} workers...`),Ls}},timeout:i(_).timeout}),(Pn=i(Xe))!=null&&Pn.signal.aborted)return $t(),null;if(!ae)throw new Error("Failed to find solution.");de("solution",ae),await at(Math.max(0,y-(performance.now()-D))),C(J,O.codeChallenge||i(_).codeChallenge||null,!0),Ze?C(xe,btoa(JSON.stringify(xi(O,ae))),!0):C(xe,btoa(JSON.stringify({challenge:{parameters:O.parameters,signature:O.signature},solution:ae})),!0),i(J)?(de("requesting code verification"),Rt(ne.CODE),H("codechallenge",{codeChallenge:i(J)})):i(_).verifyUrl?await Ss():(de("verified"),Rt(ne.VERIFIED),H("verified",{payload:i(xe)}))}catch(Ft){return de("verification failed",Ft),Rt(ne.ERROR,String(Ft)),null}finally{C(Xe,null)}return{challenge:O,payload:i(xe),solution:ae}}var vu={configure:Zr,getConfiguration:du,getState:hu,hide:Ei,log:de,reset:$t,setState:Rt,show:Si,updateUI:Aa,verify:wr},Cs=Bc();De("scroll",Ma,ru),De("click",Ma,au),De("pageshow",Fn,iu),De("resize",Fn,ou);var As=ir(Cs);{var pu=c=>{var v=jc();X(c,v)};Fe(As,c=>{i(_).display==="overlay"&&i(fe)&&c(pu)})}var tn=ye(As,2),Ts=He(tn);{var gu=c=>{var v=Fc(),x=ir(v),y=ye(x,2);{var D=O=>{var ae=Dc();Ko(ae,()=>{var Ze;return(Ze=document.querySelector(i(_).overlayContent))==null?void 0:Ze.innerHTML},!0),Ne(ae),X(O,ae)};Fe(y,O=>{i(_).overlayContent&&O(D)})}De("click",x,nu,!0),X(c,v)};Fe(Ts,c=>{i(_).display==="overlay"&&i(fe)&&c(gu)})}var Ci=ye(Ts,2),Ai=He(Ci),Ti=He(Ai),$s=He(Ti);{let c=tt(()=>i(_).display==="standard"&&i(Qt)!=="onsubmit"||i(I)===ne.VERIFYING);Gl($s,()=>i(Gr),(v,x)=>{x(v,{get id(){return i(qr)},get form(){return t.form},name:"",get required(){return i(c)},get loading(){return i(Yr)},get checked(){return i(Se)},onchange:eu,oninvalid:tu})})}var $i=ye($s,2),mu=He($i);{var bu=c=>{var v=pa();We(()=>Xt(v,i(dt).verificationRequired)),X(c,v)},wu=c=>{var v=pa();We(()=>Xt(v,i(dt).verifying)),X(c,v)},yu=c=>{var v=pa();We(()=>Xt(v,i(dt).verified)),X(c,v)},_u=c=>{var v=pa();We(()=>Xt(v,i(dt).label)),X(c,v)};Fe(mu,c=>{i(I)===ne.CODE&&i(J)?c(bu):i(I)===ne.VERIFYING?c(wu,1):i(I)===ne.VERIFIED?c(yu,2):c(_u,-1)})}Ne($i),Ne(Ti);var xu=ye(Ti,2);{var ku=c=>{vi(c,{get strings(){return i(dt)}})};Fe(xu,c=>{i(Wn)&&c(ku)})}Ne(Ai);var Rs=ye(Ai,2);{var Eu=c=>{{let v=tt(()=>i(_).display==="bar"&&i(Wn));pi(c,{get logo(){return i(v)},get strings(){return i(dt)}})}};Fe(Rs,c=>{i(Ea)&&c(Eu)})}var Ps=ye(Rs,2);{var Su=c=>{var v=Uc();An(v,x=>C(ge,x),()=>i(ge)),X(c,v)};Fe(Ps,c=>{i(_).display==="floating"&&c(Su)})}var Cu=ye(Ps,2);{var Au=c=>{var v=Hc();ci(v),We(()=>{ee(v,"form",t.form),ee(v,"name",i(_).name),oc(v,i(xe))}),X(c,v)};Fe(Cu,c=>{i(_).setCookie||c(Au)})}Ne(Ci);var Tu=ye(Ci,2);{var $u=c=>{gi(c,{get anchor(){return i(Ue)},onClickOutside:()=>{u&&$t()},get placement(){return i(_).popoverPlacement},role:"alert",variant:"error",get dir(){return i(Xr)},get updateUISignal(){return i(A)},children:(v,x)=>{var y=Vo(),D=ir(y);{var O=me=>{var it=Vc();X(me,it)},ae=me=>{var it=fs(),Pn=or(it,!0);We(()=>Xt(Pn,i(dt).expired)),X(me,it)},Ze=me=>{var it=fs(),Pn=or(it,!0);We(()=>{ee(it,"title",i(ft)),Xt(Pn,i(dt).error)}),X(me,it)};Fe(D,me=>{!i(ft)&&!u?me(O):!i(ft)&&i(I)===ne.EXPIRED?me(ae,1):me(Ze,-1)})}X(v,y)},$$slots:{default:!0}})},Ru=c=>{var v=Vo(),x=ir(v);ql(x,()=>i(J),y=>{{let D=tt(()=>i(_).codeChallengeDisplay!=="standard");gi(y,{get anchor(){return i(Ue)},get backdrop(){return i(D)},get display(){return i(_).codeChallengeDisplay},onClose:()=>{$t()},get placement(){return i(_).popoverPlacement},role:"dialog",get"aria-label"(){return i(dt).verificationRequired},get dir(){return i(Xr)},get updateUISignal(){return i(A)},children:(O,ae)=>{var Ze=zc(),me=ir(Ze);cs(me,{get audioUrl(){return i(S)},get imageUrl(){return i(re)},onCancel:()=>$t(),onReload:()=>wr(),onSubmit:Ft=>Ss(Ft),get codeChallenge(){return i(J)},get config(){return i(_)},get strings(){return i(dt)}});var it=ye(me,2);{var Pn=Ft=>{pi(Ft,{get logo(){return i(Wn)},get strings(){return i(dt)}})};Fe(it,Ft=>{i(Ea)&&i(_).codeChallengeDisplay!=="standard"&&Ft(Pn)})}X(O,Ze)},$$slots:{default:!0}})}}),X(c,v)};Fe(Tu,c=>{i(ft)||i(I)===ne.EXPIRED||!u?c($u):i(J)&&i(I)===ne.CODE&&c(Ru,1)})}Ne(tn),An(tn,c=>C(Ue,c),()=>i(Ue)),We(c=>{ee(tn,"data-state",i(I)),ee(tn,"data-display",i(_).display||void 0),ee(tn,"data-placement",c),ee(tn,"data-visible",i(fe)||void 0),ee(tn,"dir",i(Xr)),ee($i,"for",i(qr)),tn.dir=tn.dir},[()=>Xc(i(_).display)]),X(e,Cs);var Pu=cn(vu);return o(),Pu}typeof window<"u"&&window.customElements&&!customElements.get("altcha-widget")&&customElements.define("altcha-widget",Tn(Kc,{auto:{type:"String"},challenge:{type:"String"},configuration:{type:"String"},display:{type:"String"},form:{type:"String"},language:{type:"String"},name:{type:"String"},theme:{type:"String"},type:{type:"String"},workers:{type:"Number"}},[],["configure","getConfiguration","getState","hide","log","reset","setState","show","updateUI","verify"]));const ds=`(function() {
  "use strict";
  function assertAlgorithm(algorithm, allowed) {
    if (!allowed.includes(algorithm)) {
      throw new Error(
        \`Unsupported algorithm: \${String(algorithm)}. Expected one of: \${allowed.join(", ")}.\`
      );
    }
  }
  function bufferStartsWith(buffer, prefix) {
    if (prefix.length > buffer.length) {
      return false;
    }
    for (let i = 0; i < prefix.length; i++) {
      if (buffer[i] !== prefix[i]) {
        return false;
      }
    }
    return true;
  }
  function bufferToHex(buffer) {
    return Array.from(new Uint8Array(buffer)).map((b) => b.toString(16).padStart(2, "0")).join("");
  }
  function concatBuffers(a, b) {
    const out = new Uint8Array(a.length + b.length);
    out.set(a, 0);
    out.set(b, a.length);
    return out;
  }
  function hexToBuffer(hex) {
    if (hex.length % 2 !== 0) {
      throw new Error(\`Hex string must have an even length. Got: \${hex}\`);
    }
    if (!/^[0-9a-fA-F]*$/.test(hex)) {
      throw new Error("Hex string contains non-hex characters.");
    }
    const buffer = new Uint8Array(hex.length / 2);
    for (let i = 0; i < buffer.length; i++) {
      buffer[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16);
    }
    return buffer;
  }
  async function delay(ms) {
    await new Promise((resolve) => setTimeout(resolve, ms));
  }
  function timeDuration(start) {
    return Math.floor((performance.now() - start) * 10) / 10;
  }
  var HmacAlgorithm = /* @__PURE__ */ ((HmacAlgorithm2) => {
    HmacAlgorithm2["SHA_256"] = "SHA-256";
    HmacAlgorithm2["SHA_384"] = "SHA-384";
    HmacAlgorithm2["SHA_512"] = "SHA-512";
    return HmacAlgorithm2;
  })(HmacAlgorithm || {});
  Object.values(HmacAlgorithm);
  const MAX_COUNTER = {
    string: Number.MAX_SAFE_INTEGER,
    uint32: 4294967295
  };
  function isValidCounter(n, mode) {
    return Number.isInteger(n) && n >= 0 && n <= MAX_COUNTER[mode];
  }
  class PasswordBuffer {
    constructor(nonce, mode = "uint32") {
      this.nonce = nonce;
      this.mode = mode;
      this.buffer = new Uint8Array(this.nonce.length + this.COUNTER_BYTES);
      this.buffer.set(this.nonce, 0);
      this.dataView = new DataView(this.buffer.buffer);
    }
    nonce;
    mode;
    COUNTER_BYTES = 4;
    buffer;
    dataView;
    encoder = new TextEncoder();
    /**
     * Appends the counter to the nonce buffer.
     * In 'string' mode, encodes the counter as a UTF-8 string.
     * In 'uint32' mode, writes the counter as a big-endian 32-bit integer.
     * Throws a RangeError unless the counter is an integer the mode encodes exactly.
     */
    setCounter(n) {
      if (!isValidCounter(n, this.mode)) {
        throw new RangeError(
          \`counter must be an integer from 0 to \${MAX_COUNTER[this.mode]}. Got: \${n}\`
        );
      }
      if (this.mode === "string") {
        return concatBuffers(this.nonce, this.encoder.encode(n.toString()));
      }
      this.dataView.setUint32(this.nonce.length, n, false);
      return this.buffer;
    }
  }
  function assertKeyPrefix(keyPrefix, keyLength) {
    if (typeof keyPrefix !== "string" || !/^[0-9a-fA-F]+$/.test(keyPrefix)) {
      throw new Error("keyPrefix must be a non-empty hex string.");
    }
    if (keyPrefix.length > keyLength * 2) {
      throw new Error(
        \`keyPrefix (\${keyPrefix.length} hex characters) must not be longer than the key (keyLength: \${keyLength} bytes).\`
      );
    }
  }
  async function solveChallenge(options) {
    const {
      challenge,
      controller,
      counterMode = "uint32",
      counterStart = 0,
      counterStep = 1,
      deriveKey: deriveKey2,
      timeout = 9e4
    } = options;
    const { nonce, keyLength = 32, keyPrefix, salt } = challenge.parameters;
    assertKeyPrefix(keyPrefix, keyLength);
    const nonceBuf = hexToBuffer(nonce);
    const saltBuf = hexToBuffer(salt);
    const keyPrefixHex = keyPrefix.toLowerCase();
    const keyPrefixBuf = keyPrefix.length % 2 === 0 ? hexToBuffer(keyPrefix) : null;
    const password = new PasswordBuffer(nonceBuf, counterMode);
    const start = performance.now();
    let counter = counterStart;
    let iterations = 0;
    let derivedKeyHex = "";
    let lastYield = start;
    while (true) {
      if (controller?.signal.aborted || timeout && iterations % 10 === 0 && performance.now() - start > timeout) {
        return null;
      }
      const { derivedKey } = await deriveKey2(
        challenge.parameters,
        saltBuf,
        password.setCounter(counter)
      );
      if (iterations % 10 === 0 && performance.now() - lastYield > 200) {
        await delay(0);
        lastYield = performance.now();
      }
      if (keyPrefixBuf ? bufferStartsWith(derivedKey, keyPrefixBuf) : bufferToHex(derivedKey).startsWith(keyPrefixHex)) {
        derivedKeyHex = bufferToHex(derivedKey);
        break;
      }
      counter = counter + counterStep;
      iterations = iterations + 1;
    }
    return {
      counter,
      derivedKey: derivedKeyHex,
      time: timeDuration(start)
    };
  }
  function handler(options) {
    const { deriveKey: deriveKey2 } = options;
    let controller = void 0;
    self.onmessage = async (message) => {
      const { challenge, counterMode, counterStart, counterStep, timeout, type } = message.data;
      if (type === "abort") {
        controller?.abort();
      } else if (type === "work") {
        controller = new AbortController();
        let solution;
        try {
          solution = await solveChallenge({
            challenge,
            controller,
            counterStart,
            counterStep,
            deriveKey: deriveKey2,
            counterMode,
            timeout
          });
        } catch (err) {
          return self.postMessage({ error: err });
        }
        self.postMessage(solution);
      }
    };
  }
  async function deriveKey(parameters, salt, password) {
    const { algorithm, cost, keyLength = 32 } = parameters;
    assertAlgorithm(algorithm, ["PBKDF2/SHA-256", "PBKDF2/SHA-384", "PBKDF2/SHA-512"]);
    const passwordKey = await crypto.subtle.importKey(
      "raw",
      password,
      { name: "PBKDF2" },
      false,
      ["deriveBits"]
    );
    const derivedBits = await crypto.subtle.deriveBits(
      {
        name: "PBKDF2",
        salt,
        iterations: cost,
        hash: algorithm.slice("PBKDF2/".length)
      },
      passwordKey,
      keyLength * 8
    );
    return {
      parameters: {},
      derivedKey: new Uint8Array(derivedBits)
    };
  }
  handler({
    deriveKey
  });
})();
`,hs=typeof self<"u"&&self.Blob&&new Blob(["(self.URL || self.webkitURL).revokeObjectURL(self.location.href);",ds],{type:"text/javascript;charset=utf-8"});function mi(e){let t;try{if(t=hs&&(self.URL||self.webkitURL).createObjectURL(hs),!t)throw"";const n=new Worker(t,{name:e==null?void 0:e.name});return n.addEventListener("error",()=>{(self.URL||self.webkitURL).revokeObjectURL(t)}),n}catch{return new Worker("data:text/javascript;charset=utf-8,"+encodeURIComponent(ds),{name:e==null?void 0:e.name})}}const vs=`(function() {
  "use strict";
  function assertAlgorithm(algorithm, allowed) {
    if (!allowed.includes(algorithm)) {
      throw new Error(
        \`Unsupported algorithm: \${String(algorithm)}. Expected one of: \${allowed.join(", ")}.\`
      );
    }
  }
  function bufferStartsWith(buffer, prefix) {
    if (prefix.length > buffer.length) {
      return false;
    }
    for (let i = 0; i < prefix.length; i++) {
      if (buffer[i] !== prefix[i]) {
        return false;
      }
    }
    return true;
  }
  function bufferToHex(buffer) {
    return Array.from(new Uint8Array(buffer)).map((b) => b.toString(16).padStart(2, "0")).join("");
  }
  function concatBuffers(a, b) {
    const out = new Uint8Array(a.length + b.length);
    out.set(a, 0);
    out.set(b, a.length);
    return out;
  }
  function hexToBuffer(hex) {
    if (hex.length % 2 !== 0) {
      throw new Error(\`Hex string must have an even length. Got: \${hex}\`);
    }
    if (!/^[0-9a-fA-F]*$/.test(hex)) {
      throw new Error("Hex string contains non-hex characters.");
    }
    const buffer = new Uint8Array(hex.length / 2);
    for (let i = 0; i < buffer.length; i++) {
      buffer[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16);
    }
    return buffer;
  }
  async function delay(ms) {
    await new Promise((resolve) => setTimeout(resolve, ms));
  }
  function timeDuration(start) {
    return Math.floor((performance.now() - start) * 10) / 10;
  }
  var HmacAlgorithm = /* @__PURE__ */ ((HmacAlgorithm2) => {
    HmacAlgorithm2["SHA_256"] = "SHA-256";
    HmacAlgorithm2["SHA_384"] = "SHA-384";
    HmacAlgorithm2["SHA_512"] = "SHA-512";
    return HmacAlgorithm2;
  })(HmacAlgorithm || {});
  Object.values(HmacAlgorithm);
  const MAX_COUNTER = {
    string: Number.MAX_SAFE_INTEGER,
    uint32: 4294967295
  };
  function isValidCounter(n, mode) {
    return Number.isInteger(n) && n >= 0 && n <= MAX_COUNTER[mode];
  }
  class PasswordBuffer {
    constructor(nonce, mode = "uint32") {
      this.nonce = nonce;
      this.mode = mode;
      this.buffer = new Uint8Array(this.nonce.length + this.COUNTER_BYTES);
      this.buffer.set(this.nonce, 0);
      this.dataView = new DataView(this.buffer.buffer);
    }
    nonce;
    mode;
    COUNTER_BYTES = 4;
    buffer;
    dataView;
    encoder = new TextEncoder();
    /**
     * Appends the counter to the nonce buffer.
     * In 'string' mode, encodes the counter as a UTF-8 string.
     * In 'uint32' mode, writes the counter as a big-endian 32-bit integer.
     * Throws a RangeError unless the counter is an integer the mode encodes exactly.
     */
    setCounter(n) {
      if (!isValidCounter(n, this.mode)) {
        throw new RangeError(
          \`counter must be an integer from 0 to \${MAX_COUNTER[this.mode]}. Got: \${n}\`
        );
      }
      if (this.mode === "string") {
        return concatBuffers(this.nonce, this.encoder.encode(n.toString()));
      }
      this.dataView.setUint32(this.nonce.length, n, false);
      return this.buffer;
    }
  }
  function assertKeyPrefix(keyPrefix, keyLength) {
    if (typeof keyPrefix !== "string" || !/^[0-9a-fA-F]+$/.test(keyPrefix)) {
      throw new Error("keyPrefix must be a non-empty hex string.");
    }
    if (keyPrefix.length > keyLength * 2) {
      throw new Error(
        \`keyPrefix (\${keyPrefix.length} hex characters) must not be longer than the key (keyLength: \${keyLength} bytes).\`
      );
    }
  }
  async function solveChallenge(options) {
    const {
      challenge,
      controller,
      counterMode = "uint32",
      counterStart = 0,
      counterStep = 1,
      deriveKey: deriveKey2,
      timeout = 9e4
    } = options;
    const { nonce, keyLength = 32, keyPrefix, salt } = challenge.parameters;
    assertKeyPrefix(keyPrefix, keyLength);
    const nonceBuf = hexToBuffer(nonce);
    const saltBuf = hexToBuffer(salt);
    const keyPrefixHex = keyPrefix.toLowerCase();
    const keyPrefixBuf = keyPrefix.length % 2 === 0 ? hexToBuffer(keyPrefix) : null;
    const password = new PasswordBuffer(nonceBuf, counterMode);
    const start = performance.now();
    let counter = counterStart;
    let iterations = 0;
    let derivedKeyHex = "";
    let lastYield = start;
    while (true) {
      if (controller?.signal.aborted || timeout && iterations % 10 === 0 && performance.now() - start > timeout) {
        return null;
      }
      const { derivedKey } = await deriveKey2(
        challenge.parameters,
        saltBuf,
        password.setCounter(counter)
      );
      if (iterations % 10 === 0 && performance.now() - lastYield > 200) {
        await delay(0);
        lastYield = performance.now();
      }
      if (keyPrefixBuf ? bufferStartsWith(derivedKey, keyPrefixBuf) : bufferToHex(derivedKey).startsWith(keyPrefixHex)) {
        derivedKeyHex = bufferToHex(derivedKey);
        break;
      }
      counter = counter + counterStep;
      iterations = iterations + 1;
    }
    return {
      counter,
      derivedKey: derivedKeyHex,
      time: timeDuration(start)
    };
  }
  function handler(options) {
    const { deriveKey: deriveKey2 } = options;
    let controller = void 0;
    self.onmessage = async (message) => {
      const { challenge, counterMode, counterStart, counterStep, timeout, type } = message.data;
      if (type === "abort") {
        controller?.abort();
      } else if (type === "work") {
        controller = new AbortController();
        let solution;
        try {
          solution = await solveChallenge({
            challenge,
            controller,
            counterStart,
            counterStep,
            deriveKey: deriveKey2,
            counterMode,
            timeout
          });
        } catch (err) {
          return self.postMessage({ error: err });
        }
        self.postMessage(solution);
      }
    };
  }
  async function deriveKey(parameters, salt, password) {
    const { algorithm, keyLength = 32 } = parameters;
    assertAlgorithm(algorithm, ["SHA-256", "SHA-384", "SHA-512"]);
    const iterations = Math.max(1, parameters.cost);
    let derivedKey = concatBuffers(salt, password);
    for (let i = 0; i < iterations; i++) {
      derivedKey = new Uint8Array(await crypto.subtle.digest(algorithm, derivedKey));
    }
    return {
      parameters: {},
      derivedKey: derivedKey.slice(0, keyLength)
    };
  }
  handler({
    deriveKey
  });
})();
`,ps=typeof self<"u"&&self.Blob&&new Blob(["(self.URL || self.webkitURL).revokeObjectURL(self.location.href);",vs],{type:"text/javascript;charset=utf-8"});function bi(e){let t;try{if(t=ps&&(self.URL||self.webkitURL).createObjectURL(ps),!t)throw"";const n=new Worker(t,{name:e==null?void 0:e.name});return n.addEventListener("error",()=>{(self.URL||self.webkitURL).revokeObjectURL(t)}),n}catch{return new Worker("data:text/javascript;charset=utf-8,"+encodeURIComponent(vs),{name:e==null?void 0:e.name})}}return Nc(`:root {
  --altcha-border-color: var(--altcha-color-neutral);
  --altcha-border-width: 1px;
  --altcha-border-radius: 6px;
  --altcha-color-base: light-dark(oklch(100% 0.00011 271.152), oklch(20.904% 0.00002 271.152));
  --altcha-color-base-content: light-dark(
  	oklch(20.904% 0.00002 271.152),
  	oklch(100% 0.00011 271.152)
  );
  --altcha-color-error: oklch(51.284% 0.20527 28.678);
  --altcha-color-error-content: oklch(100% 0.00011 271.152);
  --altcha-color-neutral: light-dark(oklch(83.591% 0.0001 271.152), oklch(46.04% 0.00005 271.152));
  --altcha-color-neutral-content: light-dark(
  	oklch(46.76% 0.00005 271.152),
  	oklch(100% 0.00011 271.152)
  );
  --altcha-color-primary: oklch(40.279% 0.2449 268.131);
  --altcha-color-primary-content: oklch(100% 0.00011 271.152);
  --altcha-color-success: oklch(55.748% 0.18968 142.511);
  --altcha-color-success-content: oklch(100% 0.00011 271.152);
  --altcha-checkbox-border-color: light-dark(
  	oklch(66.494% 0.00233 15.434),
  	oklch(51.028% 0.00006 271.152)
  );
  --altcha-checkbox-border-radius: 5px;
  --altcha-checkbox-border-width: var(--altcha-border-width);
  --altcha-checkbox-outline: 2px solid var(--altcha-checkbox-outline-color);
  --altcha-checkbox-outline-color: -webkit-focus-ring-color;
  --altcha-checkbox-outline-offset: 2px;
  --altcha-checkbox-size: 22px;
  --altcha-checkbox-transition-duration: var(--altcha-transition-duration);
  --altcha-input-background-color: var(--altcha-color-base);
  --altcha-input-border-radius: 3px;
  --altcha-input-border-width: 1px;
  --altcha-input-color: var(--altcha-color-base-content);
  --altcha-max-width: 320px;
  --altcha-padding: 0.75rem;
  --altcha-popover-arrow-size: 6px;
  --altcha-popover-color: var(--altcha-border-color);
  --altcha-shadow: drop-shadow(3px 3px 6px oklch(0% 0 0 / 0.2));
  --altcha-spinner-color: var(--altcha-color-base-content);
  --altcha-switch-background-color: var(--altcha-color-neutral);
  --altcha-switch-border-radius: calc(infinity * 1px);
  --altcha-switch-height: var(--altcha-checkbox-size);
  --altcha-switch-padding: 0.25rem;
  --altcha-switch-width: calc(var(--altcha-checkbox-size) * 1.75);
  --altcha-switch-toggle-border-radius: 100%;
  --altcha-switch-toggle-color: var(--altcha-color-neutral-content);
  --altcha-switch-toggle-size: calc(
  	var(--altcha-switch-height) - calc(var(--altcha-switch-padding) * 2)
  );
  --altcha-transition-duration: 0.6s;
  --altcha-z-index: 99999999;
  --altcha-z-index-popover: 999999999;
}

@supports (-moz-appearance: none) {
  :root {
    --altcha-checkbox-outline-color: var(--altcha-color-primary);
  }
}
.altcha {
  all: revert-layer;
  display: none;
  font-family: inherit;
  font-size: inherit;
  position: relative;
}
.altcha[data-visible] {
  display: block;
}
.altcha-popover, .altcha-popover * {
  all: revert-layer;
  box-sizing: border-box;
  font-family: inherit;
  font-size: inherit;
  line-height: 1.25;
}
.altcha * {
  all: revert-layer;
  box-sizing: border-box;
  font-family: inherit;
  font-size: inherit;
  line-height: 1.25;
}
.altcha a, .altcha-popover a {
  color: currentColor;
  text-decoration: none;
}
.altcha a:hover, .altcha-popover a:hover {
  color: currentColor;
}
.altcha-main {
  align-items: start;
  background-color: var(--altcha-color-base);
  border: var(--altcha-border-width, 1px) solid var(--altcha-border-color);
  border-radius: var(--altcha-border-radius, 0);
  color: var(--altcha-color-base-content);
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  justify-content: space-between;
  padding: var(--altcha-padding);
  max-width: var(--altcha-max-width, 100%);
}
.altcha-main > * {
  display: flex;
  width: 100%;
}
.altcha-main > *:first-child {
  flex-grow: 1;
}
.altcha-checkbox-wrap {
  align-items: center;
  display: flex;
  flex-direction: row;
  flex-grow: 1;
  gap: 0.5rem;
}
.altcha-checkbox-wrap > * {
  display: flex;
}
.altcha-logo {
  opacity: 0.7;
}
.altcha-footer {
  align-items: center;
  display: flex;
  flex-grow: 1;
  gap: 0.5rem;
  justify-content: flex-end;
  font-size: 0.7rem;
  opacity: 0.7;
}
.altcha-footer p {
  margin: 0;
  padding: 0;
}
.altcha-error {
  font-size: 0.85rem;
}
.altcha-button {
  align-items: center;
  background: var(--altcha-color-primary);
  border: var(--altcha-input-border-width) solid var(--altcha-color-primary);
  border-radius: var(--altcha-input-border-radius);
  color: var(--altcha-color-primary-content);
  cursor: pointer;
  display: flex;
  font-size: 0.9rem;
  gap: 0.5rem;
  padding: 0.35rem;
}
.altcha-button:focus {
  border-color: var(--altcha-color-primary);
  outline: var(--altcha-checkbox-outline);
  outline-offset: var(--altcha-checkbox-outline-offset);
}
.altcha-button > .altcha-spinner, .altcha-button > svg {
  height: 20px;
  width: 20px;
}
.altcha-button-secondary {
  background: transparent;
  border-color: var(--altcha-color-neutral);
  color: var(--altcha-color-neutral-content);
}
.altcha-input {
  background: var(--altcha-input-background-color);
  border: var(--altcha-input-border-width) solid var(--altcha-color-neutral);
  border-radius: var(--altcha-input-border-radius);
  color: var(--altcha-input-color);
  flex-grow: 1;
  font-size: 1rem;
  min-width: 0;
  padding: 0.25rem;
  width: auto;
}
.altcha-input:focus {
  border-color: var(--altcha-color-primary);
  outline: var(--altcha-checkbox-outline);
  outline-offset: var(--altcha-checkbox-outline-offset);
}
.altcha-spinner {
  animation: altcha-rotate 0.6s linear infinite;
  border-radius: 100%;
  border: var(--altcha-checkbox-border-width) solid var(--altcha-spinner-color);
  border-bottom-color: transparent;
  border-right-color: transparent;
  opacity: 0.7;
}
.altcha-popover {
  background-color: var(--altcha-color-base);
  border: var(--altcha-border-width) solid var(--altcha-border-color);
  border-radius: var(--altcha-border-radius);
  color: var(--altcha-color-base-content);
  filter: var(--altcha-shadow);
  position: absolute;
  left: calc(var(--altcha-padding) / 2);
  max-width: calc(var(--altcha-max-width) - var(--altcha-padding));
  top: calc(var(--altcha-padding) + var(--altcha-checkbox-size) + var(--altcha-popover-arrow-size));
  z-index: var(--altcha-z-index-popover);
}
.altcha-popover-arrow {
  border: var(--altcha-popover-arrow-size) solid transparent;
  border-bottom-color: var(--altcha-popover-color);
  content: "";
  height: 0;
  left: calc(var(--altcha-checkbox-size) / 2);
  position: absolute;
  top: calc(var(--altcha-popover-arrow-size) * -2);
  width: 0;
}
.altcha-popover-content {
  max-height: 100dvh;
  overflow: auto;
  padding: var(--altcha-padding);
}
.altcha-popover[data-top=true][data-display=standard] {
  bottom: calc(100% - (var(--altcha-padding) - var(--altcha-popover-arrow-size)));
  top: auto;
}
.altcha-popover[data-top=true][data-display=standard] .altcha-popover-arrow {
  border-bottom-color: transparent;
  border-top-color: var(--altcha-popover-color);
  bottom: calc(var(--altcha-popover-arrow-size) * -2);
  top: auto;
}
.altcha-popover[data-variant=error] {
  --altcha-popover-color: var(--altcha-color-error);
  background-color: var(--altcha-color-error);
  border-color: var(--altcha-color-error);
  color: var(--altcha-color-error-content);
}
.altcha-popover[data-variant=error] .altcha-popover-content {
  padding: calc(var(--altcha-padding) / 1.5) var(--altcha-padding);
}
.altcha-popover[data-display=overlay] {
  animation: altcha-overlay-slidein 0.5s forwards;
  left: 50%;
  position: fixed;
  top: 45%;
  transform: translate(-50%, -50%);
  width: var(--altcha-max-width);
  z-index: var(--altcha-z-index);
}
.altcha-popover[data-display=bottomsheet] {
  animation: altcha-bottomsheet-slideup 0.5s forwards;
  border-bottom-left-radius: 0;
  border-bottom-right-radius: 0;
  border-bottom: 0;
  bottom: -100%;
  left: 50%;
  position: fixed;
  top: auto;
  transform: translate(-50%, 0);
  width: var(--altcha-max-width);
  z-index: var(--altcha-z-index);
}
.altcha-popover[data-display=bottomsheet] .altcha-popover-content {
  padding-bottom: calc(var(--altcha-padding) * 2);
}
.altcha-popover-backdrop {
  background: var(--altcha-color-base-content);
  bottom: 0;
  left: 0;
  opacity: 0.1;
  position: fixed;
  right: 0;
  top: 0;
  transition: opacity 0.5s;
  z-index: var(--altcha-z-index);
}
.altcha-popover-close {
  color: var(--altcha-color-base-content);
  cursor: pointer;
  display: inline-block;
  font-size: 1rem;
  height: 1.25rem;
  line-height: 0.95;
  position: absolute;
  right: 0;
  text-align: center;
  text-shadow: 0 0 1px var(--altcha-color-base);
  top: -1.5rem;
  width: 1.25rem;
  z-index: var(--altcha-z-index);
}
[dir=rtl] .altcha-popover {
  left: auto;
  right: calc(var(--altcha-padding) / 2);
}
[dir=rtl] .altcha-popover-arrow {
  left: auto;
  right: calc(var(--altcha-checkbox-size) / 2);
}
[dir=rtl] .altcha-popover-close {
  left: 0;
  right: auto;
}
.altcha-popover[data-display=bottomsheet] .altcha-footer, .altcha-popover[data-display=overlay] .altcha-footer {
  align-items: center;
  justify-content: center;
  padding-top: 1rem;
  gap: 0.5rem;
}
.altcha-popover[data-display=bottomsheet] .altcha-footer svg, .altcha-popover[data-display=overlay] .altcha-footer svg {
  height: 18px;
  width: 18px;
  vertical-align: middle;
}
.altcha-code-challenge > form {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.altcha-code-challenge-title {
  font-weight: 600;
}
.altcha-code-challenge-text {
  font-size: 0.85rem;
}
.altcha-code-challenge-image {
  background: white;
  border: var(--altcha-input-border-width) solid var(--altcha-color-neutral);
  border-radius: var(--altcha-input-border-radius);
  object-fit: contain;
  height: 50px;
}
.altcha-code-challenge-row {
  display: flex;
  gap: 0.5rem;
}
.altcha-code-challenge-buttons {
  align-items: center;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-top: var(--altcha-padding);
  justify-content: space-between;
}
.altcha-code-challenge-buttons button {
  justify-content: center;
  width: 100%;
}
.altcha-checkbox {
  cursor: pointer;
  height: var(--altcha-checkbox-size);
  position: relative;
  width: var(--altcha-checkbox-size);
}
.altcha-checkbox input {
  appearance: none;
  background: var(--altcha-input-background-color);
  border: var(--altcha-checkbox-border-width, 2px) solid var(--altcha-checkbox-border-color);
  border-radius: var(--altcha-checkbox-border-radius);
  cursor: pointer;
  height: var(--altcha-checkbox-size);
  left: 0;
  margin: 0;
  padding: 0;
  position: absolute;
  top: 0;
  width: var(--altcha-checkbox-size);
}
@supports (hanging-punctuation: first) and (font: -apple-system-body) and (-webkit-appearance: none) {
  .altcha-checkbox input {
    /* Safari-only: fixes focus outline */
  }
  .altcha-checkbox input:focus {
    outline-width: 2px;
    outline-style: solid;
  }
}
.altcha-checkbox input:before {
  border-radius: var(--altcha-checkbox-border-radius);
  content: "";
  width: 100%;
  height: 100%;
  background: var(--altcha-color-neutral);
  display: block;
  transform: scale(0);
}
.altcha-checkbox input:checked {
  background-color: var(--altcha-color-success);
  border-color: var(--altcha-color-success);
}
.altcha-checkbox input:checked::before {
  background-color: var(--altcha-color-success);
  opacity: 0;
  transform: scale(2.2);
  transition: all var(--altcha-checkbox-transition-duration) ease;
  transition-delay: 0.1s;
}
.altcha-checkbox svg {
  --altcha-radio-svg-size: calc(var(--altcha-checkbox-size) * 0.5);
  --altcha-radio-svg-offset: calc(var(--altcha-checkbox-size) * 0.25);
  fill: none;
  left: var(--altcha-radio-svg-offset);
  height: var(--altcha-radio-svg-size);
  opacity: 0;
  position: absolute;
  stroke: currentColor;
  stroke-width: 2;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-dasharray: 16px;
  stroke-dashoffset: 16px;
  top: var(--altcha-radio-svg-offset);
  transform: translate3d(0, 0, 0);
  width: var(--altcha-radio-svg-size);
}
.altcha-checkbox input:checked + svg {
  color: var(--altcha-color-success-content);
  opacity: 1;
  stroke-dashoffset: 0;
  transition: all var(--altcha-checkbox-transition-duration) ease;
  transition-delay: 0.1s;
}
.altcha-checkbox-spinner {
  display: none;
  left: 0;
  height: var(--altcha-checkbox-size);
  position: absolute;
  top: 0;
  width: var(--altcha-checkbox-size);
}
.altcha-checkbox[data-loading=true] input {
  appearance: none;
  opacity: 0;
  pointer-events: none;
}
.altcha-checkbox[data-loading=true] .altcha-checkbox-spinner {
  display: block;
}
.altcha-checkbox-native {
  height: var(--altcha-checkbox-size);
  position: relative;
  width: var(--altcha-checkbox-size);
}
.altcha-checkbox-native input {
  height: var(--altcha-checkbox-size);
  margin: 0;
  width: var(--altcha-checkbox-size);
}
.altcha-checkbox-native-spinner {
  display: none;
  left: 0;
  height: var(--altcha-checkbox-size);
  position: absolute;
  top: 0;
  width: var(--altcha-checkbox-size);
}
.altcha-checkbox-native[data-loading=true] input {
  appearance: none;
  opacity: 0;
  pointer-events: none;
}
.altcha-checkbox-native[data-loading=true] .altcha-checkbox-native-spinner {
  display: block;
}
.altcha-switch {
  align-items: center;
  border-radius: var(--altcha-switch-border-radius);
  background-color: var(--altcha-switch-background-color);
  display: flex;
  height: var(--altcha-switch-height);
  padding: var(--altcha-switch-padding);
  position: relative;
  width: var(--altcha-switch-width);
}
.altcha-switch:focus-within {
  outline: var(--altcha-checkbox-outline);
  outline-offset: var(--altcha-checkbox-outline-offset);
}
.altcha-switch input {
  appearance: none;
  cursor: pointer;
  height: 100%;
  left: 0;
  opacity: 0;
  position: absolute;
  top: 0;
  width: 100%;
}
.altcha-switch-toggle {
  align-items: center;
  background-color: var(--altcha-switch-toggle-color);
  border-radius: var(--altcha-switch-toggle-border-radius);
  cursor: pointer;
  display: flex;
  height: var(--altcha-switch-toggle-size);
  justify-content: center;
  left: var(--altcha-switch-padding);
  position: absolute;
  transition: width 150ms ease-out, left 150ms ease-out;
  width: var(--altcha-switch-toggle-size);
}
.altcha-switch-spinner {
  display: none;
  height: var(--altcha-switch-toggle-size);
  width: var(--altcha-switch-toggle-size);
}
.altcha-switch[data-loading=true] {
  pointer-events: none;
}
.altcha-switch[data-loading=true] .altcha-switch-spinner {
  display: block;
}
.altcha-switch[data-loading=true] .altcha-switch-toggle {
  background-color: transparent;
  left: calc(50% - var(--altcha-switch-toggle-size) / 2);
}
[data-state=verified] .altcha-switch {
  --altcha-switch-background-color: var(--altcha-color-success);
}
[data-state=verified] .altcha-switch-toggle {
  background-color: var(--altcha-color-success-content);
  left: calc(100% - var(--altcha-switch-height) + var(--altcha-switch-padding));
}
[dir=rtl] .altcha-switch-toggle {
  left: calc(100% - var(--altcha-switch-height) + var(--altcha-switch-padding));
}
[dir=rtl][data-state=verified] .altcha-switch-toggle {
  left: var(--altcha-switch-padding);
}
.altcha-floating-arrow {
  border: 6px solid transparent;
  border-bottom-color: var(--altcha-border-color);
  content: "";
  height: 0;
  left: 12px;
  position: absolute;
  top: -12px;
  width: 0;
}
.altcha-overlay-backdrop {
  bottom: 0;
  left: 0;
  position: fixed;
  right: 0;
  top: 0;
  transition: opacity var(--altcha-transition-duration);
  z-index: var(--altcha-z-index);
}
.altcha-overlay-close {
  display: inline-block;
  color: currentColor;
  cursor: pointer;
  font-size: 1rem;
  height: 1rem;
  line-height: 0.85;
  position: absolute;
  right: 0;
  text-align: center;
  text-shadow: 0 0 1px var(--altcha-color-base);
  top: -1.5rem;
  width: 1rem;
  z-index: var(--altcha-z-index);
}
.altcha[data-display=overlay] {
  animation: altcha-overlay-slidein var(--altcha-transition-duration) forwards;
  filter: var(--altcha-shadow);
  left: 50%;
  opacity: 0;
  position: fixed;
  top: 45%;
  transform: translate(-50%, -50%);
  z-index: var(--altcha-z-index);
}
.altcha[data-display=overlay] .altcha-main {
  width: var(--altcha-max-width);
}
.altcha[data-display=floating] {
  display: none;
  filter: var(--altcha-shadow);
  left: var(--altcha-floating-left, -100%);
  position: fixed;
  top: var(--altcha-floating-top, -100%);
  z-index: var(--altcha-z-index);
}
.altcha[data-display=floating] .altcha-main {
  width: var(--altcha-max-width);
}
.altcha[data-display=floating][data-floating-position=top] .altcha-floating-arrow {
  border-bottom-color: transparent;
  border-top-color: var(--altcha-border-color);
  bottom: -12px;
  top: auto;
}
.altcha[data-display=floating][data-visible] {
  display: flex;
}
.altcha[data-display=bar] {
  bottom: -100%;
  filter: var(--altcha-shadow);
  left: 0;
  position: fixed;
  right: 0;
  transition: bottom var(--altcha-transition-duration), top var(--altcha-transition-duration);
  z-index: var(--altcha-z-index);
}
.altcha[data-display=bar] .altcha-main {
  align-items: center;
  border-radius: 0;
  border-width: var(--altcha-border-width) 0 0 0;
  flex-direction: row;
  max-width: 100% !important;
}
.altcha[data-display=bar] .altcha-main > * {
  width: auto;
}
.altcha[data-display=bar][data-placement=top] {
  bottom: auto;
  top: -100%;
}
.altcha[data-display=bar][data-placement=top] .altcha-main {
  border-width: 0 0 var(--altcha-border-width) 0;
}
.altcha[data-display=bar][data-placement=bottom]:not([data-state=unverified]) {
  bottom: 0;
}
.altcha[data-display=bar][data-placement=top]:not([data-state=unverified]) {
  top: 0;
}
.altcha[data-display=invisible] {
  display: none;
}

@keyframes altcha-rotate {
  0% {
    transform: rotate(0deg);
  }
  100% {
    transform: rotate(360deg);
  }
}
@keyframes altcha-bottomsheet-slideup {
  100% {
    bottom: 0;
  }
}
@keyframes altcha-overlay-slidein {
  100% {
    opacity: 1;
    top: 50%;
  }
}`),$altcha.algorithms.set("SHA-256",()=>new bi),$altcha.algorithms.set("SHA-384",()=>new bi),$altcha.algorithms.set("SHA-512",()=>new bi),$altcha.algorithms.set("PBKDF2/SHA-256",()=>new mi),$altcha.algorithms.set("PBKDF2/SHA-384",()=>new mi),$altcha.algorithms.set("PBKDF2/SHA-512",()=>new mi),Ms}Wu();const Ra=()=>{};function Xu(E,k){return E!=E?k==k:E!==k||E!==null&&typeof E=="object"||typeof E=="function"}function Ju(E,k,R){if(E==null)return k(void 0),Ra;const Z=Qu(()=>E.subscribe(k,R));return Z.unsubscribe?()=>Z.unsubscribe():Z}const yr=[];function Zu(E,k=Ra){let R=null;const Z=new Set;function $e(V){if(Xu(E,V)&&(E=V,R)){const ot=!yr.length;for(const Re of Z)Re[1](),yr.push(Re,E);if(ot){for(let Re=0;Re<yr.length;Re+=2)yr[Re][0](yr[Re+1]);yr.length=0}}}function Me(V){$e(V(E))}function P(V,ot=Ra){const Re=[V,ot];return Z.add(Re),Z.size===1&&(R=k($e,Me)||Ra),V(E),()=>{Z.delete(Re),Z.size===0&&R&&(R(),R=null)}}return{set:$e,update:Me,subscribe:P}}function Ta(E){let k;return Ju(E,R=>k=R)(),k}let Li=!1;function Qu(E){var k=Li;try{return Li=!0,E()}finally{Li=k}}function Ds(E){const k={get:R=>Ta(k.store)[R],set:(R,Z)=>{typeof R=="string"?Object.assign(Ta(k.store),{[R]:Z}):Object.assign(Ta(k.store),R),k.store.set(Ta(k.store))},store:Zu(E)};return k}globalThis.$altcha=globalThis.$altcha||{algorithms:new Map,defaults:Ds({}),i18n:Ds({}),instances:new Set,plugins:new Set};const ef={ariaLinkLabel:"Altcha (官方网站)",enterCode:"输入代码",enterCodeAria:"输入您听到的代码。按空格键播放音频。",error:"验证失败。稍后再试。",expired:"验证已过期。请重试。",footer:'由 <a href="https://altcha.org/" target="_blank" rel="noopener" aria-label="Altcha (官方网站，在新标签页中打开)">ALTCHA</a> 保护',getAudioChallenge:"获取音频挑战",label:"我不是机器人",loading:"加载中...",reload:"重新加载",verify:"验证",verificationRequired:"需要验证！",verified:"已验证",verifying:"正在验证...",waitAlert:"正在验证... 请稍等。",cancel:"取消",enterCodeFromImage:"为继续操作，请输入下图中显示的验证码。"};globalThis.$altcha.i18n.set("zh-cn",ef);let ea=null;const Oi=new WeakMap;function tf(){return window.turnstile?Promise.resolve(window.turnstile):ea||(ea=new Promise((E,k)=>{const R=document.createElement("script"),Z=()=>{clearTimeout($e),R.remove(),ea=null,k(new Error("Turnstile 服务无法加载，请稍后重试"))},$e=window.setTimeout(Z,15e3);R.src="https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit",R.async=!0,R.addEventListener("error",Z,{once:!0}),R.addEventListener("load",()=>{clearTimeout($e),window.turnstile?E(window.turnstile):Z()},{once:!0}),document.head.append(R)}),ea)}async function nf(E,k,R,Z,$e){var P;const Me=await tf();if(Z.aborted)throw new DOMException("已取消","AbortError");return(P=Oi.get(E))==null||P(),new Promise((V,ot)=>{let Re=!1,ke;const he=(G,je)=>{clearTimeout(Pe),!Re&&(Re=!0,G?V(G):ot(je??new Error("Turnstile 未完成，请重新验证")))},le=()=>{ke&&Me.remove(ke),Oi.delete(E),Z.removeEventListener("abort",le),he(void 0,new DOMException("已取消","AbortError"))},Pe=window.setTimeout(()=>{ke&&Me.remove(ke),he(void 0,new Error("Turnstile 验证超时，请重试"))},12e4);Z.addEventListener("abort",le,{once:!0}),Oi.set(E,le);try{ke=Me.render(E,{sitekey:k,action:R,language:"zh-CN",callback:G=>he(G),"error-callback":()=>{$e(),he()},"expired-callback":()=>{$e(),he(void 0,new Error("Turnstile 验证已过期，请重试"))}})}catch(G){Z.removeEventListener("abort",le),he(void 0,G instanceof Error?G:new Error("Turnstile 无法显示，请重试"))}})}const rf=ie.forwardRef(function({purpose:k},R){const Z=ie.useRef(null),$e=ie.useRef(async()=>{throw new Error("人机验证尚未就绪")}),Me=ie.useRef(()=>{}),[P,V]=ie.useState(""),[ot,Re]=ie.useState(0);return ie.useImperativeHandle(R,()=>({solve:ke=>$e.current(ke),reset:()=>Me.current()}),[]),ie.useEffect(()=>{const ke=Z.current;if(!ke)return;const he=new AbortController,le=document.createElement("altcha-widget"),Pe=document.createElement("div");le.setAttribute("language","zh-cn"),le.setAttribute("workers","2"),ke.append(le);let G=null,je=null;const ve=async Be=>{if(he.signal.aborted)throw new Error("人机验证已取消");if((je==null?void 0:je.purpose)===Be&&je.expires>Date.now())return je.result;if(G){if(G.purpose===Be)return G.promise;await G.promise.catch(()=>{})}const nn=(async()=>{var ht;V("");const ce=crypto.randomUUID().replaceAll("-",""),be=await _r(`/auth/human/challenge?purpose=${encodeURIComponent(Be)}`,"GET",void 0,{traceId:ce});if(he.signal.aborted)throw new Error("人机验证已取消");let Ae;if(be.provider==="turnstile"){le.remove(),Pe.replaceChildren(),ke.append(Pe);const vt=performance.now(),Ie=await nf(Pe,be.sitekey??"",Be,he.signal,()=>{je=null,V("Turnstile 验证已失效，请重新验证")});if(await new Promise(we=>{var Te;const pe=setTimeout(we,Math.max(0,Number(((Te=be.parameters.data)==null?void 0:Te.minimumSeconds)??2)*1e3-(performance.now()-vt)));he.signal.addEventListener("abort",()=>{clearTimeout(pe),we()},{once:!0})}),he.signal.aborted)throw new Error("人机验证已取消");Ae={altcha:btoa(JSON.stringify({challenge:{signature:be.signature}})),turnstile:Ie,traceId:ce}}else{Pe.remove(),le.isConnected||ke.append(le),le.reset(),await le.configure({challenge:be,auto:"off",workers:2,language:"zh-cn",minDuration:Number(((ht=be.parameters.data)==null?void 0:ht.minimumSeconds)??2)*1e3,humanInteractionSignature:!1});const vt=await le.verify({controller:he});if(!(vt!=null&&vt.payload)||he.signal.aborted)throw new Error("人机验证未完成，请重试");Ae={altcha:vt.payload,traceId:ce}}return je={purpose:Be,result:Ae,expires:Number(be.parameters.expiresAt)*1e3},Ae})();G={purpose:Be,promise:nn};try{return await nn}catch(ce){throw he.signal.aborted||V(Ni(ce,{}).message),ce}finally{(G==null?void 0:G.promise)===nn&&(G=null)}};$e.current=ve,Me.current=()=>{je=null,Re(Be=>Be+1)};const ze=()=>{(le.getState()==="expired"||le.getState()==="error")&&(je=null)};return le.addEventListener("statechange",ze),ve(k).catch(()=>{}),()=>{he.abort(),le.removeEventListener("statechange",ze),le.remove(),Pe.remove()}},[k,ot]),T.jsxs("div",{className:"stack","data-tour":"human-verification",children:[T.jsx("div",{ref:Z,className:"human-verification","aria-label":"人机验证"}),P&&T.jsxs(Jn,{tone:"bad",children:[P,T.jsx(In,{type:"button",variant:"link",onClick:()=>Re(ke=>ke+1),children:"重新验证"})]})]})}),Fs={login:"欢迎回来",reset:"恢复账户",register:"创建账户"},af={login:"登录 Weir，管理你的节点与订阅。",reset:"通过邮箱验证码设置新密码。原有会话会被撤销。",register:"邀请码可附带初始套餐；其余节点与订阅权限由管理员分配。"},of={invalid_email:"email",email_taken:"email",email_suffix_denied:"email",email_disposable:"email",email_mx_missing:"email",email_mx_unavailable:"email",email_verification_required:"email",weak_password:"password",invalid_password:"password",invalid_credentials:"password",invalid_verification_code:"code",invalid_email_code:"code",invalid_code:"code",invalid_second_factor:"code",invalid_access_code:"invite_code",access_code_cooldown:"invite_code",second_factor_required:"code"};function pf({done:E}){var tr,nr,wn,yn,_n,xn,kn,En,Er,Sr;const k=ie.useId(),R=ie.useRef(null),[Z,$e]=Ou(),Me=Z.get("auth"),P=Me==="verify"?"register":Me==="reset"||Me==="register"?Me:"login";Nu({page:"登录",tab:Fs[P]});const V=Mu({queryKey:["auth-policy"],queryFn:()=>_r("/public"),retry:!1}),[ot,Re]=ie.useState(""),[ke,he]=ie.useState(""),[le,Pe]=ie.useState(""),[G,je]=ie.useState(""),[ve,ze]=ie.useState(""),[Be,nn]=ie.useState(""),[ce,be]=ie.useState(!1),[Ae,ht]=ie.useState(null),[vt,Ie]=ie.useState(""),we=ie.useMemo(()=>ju(Ae),[Ae]),pe=z=>ht(Ni(z,of)),[Te,Qe]=ie.useState(0),Le=ie.useRef(null),[rn,Ln]=ie.useState(!1),[pt,Zn]=ie.useState(""),kr=!G&&(P!=="login"||rn||!!((tr=V.data)!=null&&tr.human_login_required)),an=z=>{z instanceof Bu&&(z.code==="human_verification_required"||z.code==="human_verification_invalid")&&Ln(!0),pe(z)},Qn=ie.useRef(null),Ut=ie.useRef(E);Ut.current=E;const[On,on]=ie.useState("");ie.useEffect(()=>{var xt;if(on(""),P!=="login"||G||!((xt=V.data)!=null&&xt.passkey_enabled))return;const z=new AbortController;return Qn.current=z,qu(z.signal).then(Pt=>{Pt&&!z.signal.aborted&&($e({},{replace:!0}),Ut.current())}).catch(Pt=>{z.signal.aborted||on(`通行密钥暂不可用：${Ni(Pt,{}).message}仍可使用密码登录。`)}),()=>{z.abort(),Qn.current===z&&(Qn.current=null)}},[P,G,(nr=V.data)==null?void 0:nr.passkey_enabled,$e]),ie.useEffect(()=>{if(!Te)return;const z=window.setTimeout(()=>Qe(xt=>Math.max(0,xt-1)),1e3);return()=>clearTimeout(z)},[Te]),ie.useEffect(()=>{je(""),ze(""),he(""),Pe(""),ht(null),Ln(!1)},[P]);const _t=P==="register"&&!((wn=V.data)!=null&&wn.registration_enabled)||P==="reset"&&!((yn=V.data)!=null&&yn.password_reset),bn=!!G||P==="reset"||P==="register"&&((_n=V.data)==null?void 0:_n.email_verification);function Nn(z){ce||(je(""),ze(""),ht(null),Ie(""),$e(z==="login"?{}:{auth:z},{replace:!0}))}function aa(){he(""),ze(""),$e({},{replace:!0}),E()}async function Pa(){var z,xt,Pt,It;if(!_t){(z=Qn.current)==null||z.abort(),be(!0),ht(null),Ie("");try{if(G){await _r("/auth/mfa","POST",{challenge:G,code:ve}),aa();return}if(P==="login"){const sn=kr?await((xt=Le.current)==null?void 0:xt.solve("login")):void 0;if(kr&&!sn)throw new Error("请等待人机验证就绪");const Cr=await _r("/auth/login","POST",{email:ot,password:ke,website:pt,altcha:sn==null?void 0:sn.altcha},{traceId:sn==null?void 0:sn.traceId});he(""),Cr.mfa_required&&Cr.challenge?(je(Cr.challenge),ze("")):aa();return}const Sn=P==="reset"?"/auth/reset":"/auth/register",rr=await((Pt=Le.current)==null?void 0:Pt.solve(P));if(!rr)throw new Error("请等待人机验证就绪");await _r(Sn,"POST",{email:ot,password:ke,code:ve,invite_code:P==="register"?Be:void 0,website:pt,altcha:rr.altcha,turnstile:rr.turnstile},{traceId:rr.traceId}),Nn("login"),he(""),Pe(""),ze(""),$e({},{replace:!0}),Ie(P==="reset"?"密码已重设，请使用新密码登录。":"账户创建成功，请登录查看初始套餐与授权。")}catch(Sn){an(Sn)}finally{(It=Le.current)==null||It.reset(),be(!1)}}}async function er(){var z,xt,Pt;if(!(!((z=R.current)!=null&&z.reportValidity())||ce||Te)){be(!0),ht(null),Ie("");try{const It=await((xt=Le.current)==null?void 0:xt.solve("email"));if(!It)throw new Error("请等待人机验证就绪");const Sn=await _r("/auth/email","POST",{email:ot,purpose:P==="reset"?"reset":"register",invite_code:P==="register"?Be:void 0,altcha:It.altcha,turnstile:It.turnstile,website:pt},{traceId:It.traceId});Qe(Sn.retry_after??60),Ie(`若此邮箱符合条件，验证码将发送到邮箱；请检查收件箱与垃圾邮件。验证码 ${Sn.expires_in??600} 秒有效。`)}catch(It){pe(It)}finally{(Pt=Le.current)==null||Pt.reset(),be(!1)}}}return T.jsxs("main",{className:"auth-screen",children:[T.jsx(Du,{demo:!0,centerLon:-35}),T.jsxs("div",{className:"auth-container",children:[T.jsx("a",{href:"/",className:"auth-brand","aria-label":"Weir 首页",children:T.jsx(Fu,{})}),T.jsx("h1",{className:"auth-title",children:G?"验证你的身份":Fs[P]}),T.jsx("p",{className:"auth-subtitle",children:G?"输入身份验证器中的六位验证码，或使用一枚未使用的恢复码。":af[P]}),T.jsx(Uu,{className:"auth-card",size:"lg",children:T.jsxs(Hu,{children:[V.isPending&&T.jsx("p",{className:"auth-hint",role:"status",children:"正在读取登录方式…"}),V.isError&&T.jsxs(Jn,{tone:"bad",children:["登录方式暂时无法读取。",T.jsx(In,{type:"button",variant:"link",onClick:()=>V.refetch(),children:"重新读取"})]}),_t&&!V.isPending?T.jsx("div",{className:"stack",children:T.jsx(Jn,{tone:"warn",role:"status",children:V.isError?"当前账户策略未知，请重新读取后再继续。":P==="register"?"注册暂未开放，请联系管理员创建账户。":"尚未配置邮件服务，无法找回密码。"})}):T.jsxs(Vu,{busy:ce,errors:we,onSubmit:()=>void Pa(),className:"auth-form",children:[T.jsxs("div",{className:"auth-honeypot","aria-hidden":"true",inert:!0,children:[T.jsx("label",{htmlFor:k+"website",children:"网站"}),T.jsx(Pi,{id:k+"website",name:"website",autoComplete:"off",tabIndex:-1,value:pt,onChange:z=>Zn(z.target.value)})]}),!G&&T.jsx(Qr,{label:"邮箱",htmlFor:k+"email",name:"email",children:T.jsx(Pi,{ref:R,id:k+"email",type:"email",autoComplete:P==="login"?"username webauthn":"username",required:!0,value:ot,onChange:z=>Re(z.target.value),placeholder:"you@example.com"})}),!G&&T.jsx(Qr,{label:P==="reset"?"新密码":"密码",htmlFor:k+"password",name:"password",hint:P!=="login"?"至少 6 个字符。":void 0,customError:P!=="login"&&ke?Ku(ke):"",children:T.jsx(Ii,{id:k+"password",autoComplete:P==="login"?"current-password":"new-password",required:!0,value:ke,onChange:z=>he(z.target.value)})}),!G&&(P==="reset"||P==="register")&&T.jsx(Qr,{label:"确认密码",htmlFor:k+"confirm",customError:le&&le!==ke?"两次输入的密码不一致":"",children:T.jsx(Ii,{id:k+"confirm",autoComplete:"new-password",required:!0,value:le,onChange:z=>Pe(z.target.value)})}),P==="register"&&((xn=V.data)==null?void 0:xn.invite_required)&&T.jsx(Qr,{label:"邀请码",name:"invite_code",hint:"由管理员提供，一码一用；发送验证码前会先校验。",children:T.jsx(Ii,{required:!0,minLength:51,maxLength:51,autoComplete:"off",value:Be,onChange:z=>nn(z.target.value.trim())})}),bn&&T.jsxs(Qr,{label:G?"验证码 / 恢复码":"邮箱验证码",htmlFor:k+"code",name:"code",children:[T.jsx(Pi,{id:k+"code",autoComplete:"one-time-code",autoFocus:!!G,required:!0,value:ve,onChange:z=>ze(z.target.value.trim())}),!G&&T.jsx(In,{type:"button",variant:"outline",disabled:Te>0,onClick:er,children:Te?`${Te} 秒后重新发送`:"发送验证码"})]}),kr&&T.jsx(rf,{ref:Le,purpose:P}),Ae&&!Ae.field&&T.jsx(Jn,{tone:"bad",children:Ae.message}),On&&P==="login"&&T.jsx(Jn,{tone:"warn",children:On}),P==="register"&&!!((En=(kn=V.data)==null?void 0:kn.registration_email_suffixes)!=null&&En.length)&&T.jsxs(Jn,{children:["允许的邮箱后缀：",V.data.registration_email_suffixes.join("、")]}),vt&&T.jsx(Jn,{tone:"ok",role:"status",children:vt}),T.jsxs(In,{type:"submit",disabled:V.isPending||!!_t,busy:ce,busyText:"正在验证…",children:[G?"验证并登录":P==="login"?"登录":P==="reset"?"重设密码":"创建账户",T.jsx(Gu,{size:16})]})]},`${P}:${G?"second-factor":"credentials"}`),T.jsx("nav",{className:"auth-links","aria-label":"账户操作",children:G?T.jsxs(In,{type:"button",variant:"link",disabled:ce,onClick:()=>{je(""),ze(""),ht(null)},children:[T.jsx(Ns,{size:14}),"重新登录"]}):P!=="login"?T.jsxs(In,{type:"button",variant:"link",disabled:ce,onClick:()=>Nn("login"),children:[T.jsx(Ns,{size:14}),"返回登录"]}):T.jsxs(T.Fragment,{children:[((Er=V.data)==null?void 0:Er.password_reset)&&T.jsx(In,{type:"button",variant:"link",disabled:ce,onClick:()=>Nn("reset"),children:"忘记密码"}),((Sr=V.data)==null?void 0:Sr.registration_enabled)&&T.jsx(In,{type:"button",variant:"link",onClick:()=>Nn("register"),children:"注册账户"})]})})]})}),T.jsx("p",{className:"auth-footnote",children:"背景是全球公开机房之间的随机航线，不代表任何真实节点或流量。"}),T.jsxs("p",{className:"auth-footer",children:[T.jsx(zu,{size:14}),"账户验证与设备配对独立管理"]})]})]})}export{pf as LoginPage};
