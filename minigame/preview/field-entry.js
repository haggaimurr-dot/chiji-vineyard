'use strict';
const {start}=require('../field/main');
window.vineyardField=start();
window.addEventListener('pagehide',()=>window.vineyardField.destroy(),{once:true});
