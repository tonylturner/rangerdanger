let _=i.prototype,k="string"==typeof TURBOPACK_CHUNK_BASE_PATH?TURBOPACK_CHUNK_BASE_PATH:"/containd/_next/",P=new Map;
async function j(e,t,r){let n;if("string"==typeof r)return function(e,t,r){return M(e,t,r)}(e,t,x(r));let o=r.included||[],l=o.map(e=>!!P.has(e)||$.get(e));if(l.length>0&&l.every(e=>e))return void await Promise.all(l);for(let l of(n=M(e,t,x(r.path)),o))$.has(l)||$.set(l,n);await n}_.l=function(e){return j(o.Parent,this.m.id,e)};
function x(e,t=k){let r=T.test(e)?e.split("/").map(encodeURIComponent).join("/"):e;return`${t}${r}${n}`}
