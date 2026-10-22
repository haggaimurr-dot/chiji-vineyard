'use strict';
const BOUNDS = { minX: -7.5, maxX: 7.5, minZ: -7.8, maxZ: 9 };
const CELL = 0.4, BODY = 0.28;
function canStand(x, z, obstacles) {
  if (!Number.isFinite(x) || !Number.isFinite(z)) return false;
  if (x < BOUNDS.minX || x > BOUNDS.maxX || z < BOUNDS.minZ || z > BOUNDS.maxZ) return false;
  return !(obstacles || []).some(o => {
    if (Number.isFinite(o.radius)) return Math.hypot(x - o.x, z - o.z) < o.radius + BODY;
    if (Number.isFinite(o.minX)) return x > o.minX - BODY && x < o.maxX + BODY && z > o.minZ - BODY && z < o.maxZ + BODY;
    if (Number.isFinite(o.width)) return Math.abs(x - o.x) < o.width / 2 + BODY && Math.abs(z - o.z) < o.depth / 2 + BODY;
    return false;
  });
}
function move(position, dx, dz, obstacles) {
  const next = { ...position };
  // Separate axes allow sliding along fences without walking through them.
  if (canStand(next.x + dx, next.z, obstacles)) next.x += dx;
  if (canStand(next.x, next.z + dz, obstacles)) next.z += dz;
  return next;
}
function findPath(position, target, obstacles) {
  const nx = Math.ceil((BOUNDS.maxX - BOUNDS.minX) / CELL) + 1;
  const nz = Math.ceil((BOUNDS.maxZ - BOUNDS.minZ) / CELL) + 1;
  const point = (x,z) => ({x:BOUNDS.minX+x*CELL,z:BOUNDS.minZ+z*CELL});
  const cell = p => ({x:Math.max(0,Math.min(nx-1,Math.round((p.x-BOUNDS.minX)/CELL))),z:Math.max(0,Math.min(nz-1,Math.round((p.z-BOUNDS.minZ)/CELL)))});
  const start = cell(position), key = (x,z) => z*nx+x;
  const queue = [start], previous = new Map([[key(start.x,start.z), null]]);
  let end = null;
  // The target can be a solid object: stop in its interaction area.
  const reach = Math.max(0.65, Math.min(1.05, (target.radius || 1.6) - 0.25));
  for (let head=0;head<queue.length;head++) {
    const c=queue[head], p=point(c.x,c.z);
    if (Math.hypot(p.x-target.x,p.z-target.z)<=reach && canStand(p.x,p.z,obstacles)) {end=c;break;}
    for (const [dx,dz] of [[0,-1],[-1,0],[1,0],[0,1]]) {
      const x=c.x+dx,z=c.z+dz,k=key(x,z);
      if(x<0||z<0||x>=nx||z>=nz||previous.has(k))continue;
      const q=point(x,z);if(!canStand(q.x,q.z,obstacles))continue;
      previous.set(k,key(c.x,c.z));queue.push({x,z});
    }
  }
  if (!end) return null;
  const path=[];
  for (let k=key(end.x,end.z);k!==null;k=previous.get(k)) path.push(point(k%nx,Math.floor(k/nx)));
  path.reverse(); if(path.length>1)path.shift();
  return path;
}
module.exports={BOUNDS,canStand,move,findPath};
