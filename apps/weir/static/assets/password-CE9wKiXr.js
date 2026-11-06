function t(r){const e=Array.from(r).length,n=new TextEncoder().encode(r).length;return e<6||n>72?"密码至少 6 个字符，且不能超过 72 字节。":""}export{t as a};
