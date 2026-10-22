'use strict';
// Three's animation cleanup expects a frame context. This adapter adds no DOM.
if (typeof self === 'undefined' && typeof GameGlobal !== 'undefined') {
  GameGlobal.self = {
    requestAnimationFrame: fn => typeof requestAnimationFrame === 'function' ? requestAnimationFrame(fn) : setTimeout(() => fn(Date.now()), 33),
    cancelAnimationFrame: id => typeof cancelAnimationFrame === 'function' ? cancelAnimationFrame(id) : clearTimeout(id)
  };
}
const {start}=require('./main');
module.exports={start};
