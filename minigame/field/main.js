'use strict';
const THREE = require('three');
const {createFieldPlatform}=require('./platform');
const {createWorld}=require('./world');
const {createHUD}=require('./hud');
const model=require('./model');
const {canStand,move,findPath}=require('./navigation');

function start(options) {
  options=options||{};
  const platform=options.platform||createFieldPlatform();
  const info=platform.info();
  let renderer,world,hud,overlay;
  try {
    const context=platform.native ? platform.canvas.getContext('webgl',{antialias:true,alpha:false}) : undefined;
    if(platform.native && !context)throw new Error('这台设备暂时无法创建三维画面，请更新微信后重试。');
    renderer=new THREE.WebGLRenderer({canvas:platform.canvas,context,antialias:true,alpha:false});
    renderer.setPixelRatio(info.dpr);renderer.setSize(info.width,info.height,false);
    renderer.outputColorSpace=THREE.SRGBColorSpace;
    renderer.toneMapping=THREE.ACESFilmicToneMapping;renderer.toneMappingExposure=1.1;
    renderer.shadowMap.enabled=true;renderer.shadowMap.type=THREE.PCFSoftShadowMap;
    world=createWorld({THREE,renderer,width:info.width,height:info.height,createCanvas:platform.createCanvas});
    hud=createHUD({canvas:platform.hudCanvas,controls:platform.controls});
    hud.resize(info.width,info.height,info.dpr,info.safeTop,info.safeBottom);
    if(platform.native){
      const texture=new THREE.CanvasTexture(platform.hudCanvas); texture.colorSpace=THREE.SRGBColorSpace;texture.generateMipmaps=false;
      texture.minFilter=THREE.LinearFilter;
      const material=new THREE.MeshBasicMaterial({map:texture,transparent:true,depthTest:false,depthWrite:false,toneMapped:false});
      const mesh=new THREE.Mesh(new THREE.PlaneGeometry(2,2),material);
      const scene=new THREE.Scene();scene.add(mesh);
      overlay={scene,camera:new THREE.OrthographicCamera(-1,1,1,-1,0,2),texture,material,mesh};
      overlay.camera.position.z=1;renderer.autoClear=false;
    }
  }catch(e){
    if(world)world.dispose();if(renderer)renderer.dispose();
    platform.fail(e.message||'画面无法加载，请重新打开游戏。');
    return {ready:Promise.resolve(),getState:()=>null,getStatus:()=>({error:e.message}),getControls:()=>[],destroy:()=>platform.destroy()};
  }
  let state,position={x:0,z:7},yaw=0,cameraYaw=0,previewCharacter='lin';
  let frame=null,lastTime=null,lastPaint=0,running=true,destroyed=false;
  let route=null,pending=null,joystickPointer=null,cameraPointer=null,holdPointer=null,dirtyPosition=false,saveBlocked=false,retryAction=null;
  const keys=new Set();
  const consumedPointers=new Set();
  const view={model,state:null,stage:'character',objective:null,report:null,previewCharacter,menu:null,guide:false,busy:false,
    error:'',status:'走进自己的葡萄园',nearest:null,targets:[],joystick:{x:0,y:0},interactionProgress:null,
    onPreviewCharacter(id){if(model.CHARACTERS.some(c=>c.id===id)){previewCharacter=id;view.previewCharacter=id;}},
    onChooseCharacter(id){if(view.busy||saveBlocked)return;commitAction({type:'selectCharacter',characterId:id||previewCharacter});view.menu=null;},
    onCharacterMenu(){if(!view.busy){previewCharacter=state.characterId||previewCharacter;view.previewCharacter=previewCharacter;view.menu='characters';stopMoving();}},
    onCloseMenu(){if(saveBlocked)return;view.menu=null;},
    onHelp(){if(!view.busy){view.menu='help';stopMoving();}},
    onReport(){if(!view.busy){view.menu='report';stopMoving();}},
    onToggleGuide(){if(!view.busy){view.guide=!view.guide;if(view.menu==='help')view.menu=null;}},
    onNavigate(id){navigate(id);},onInteract(){interact(false);},
    onAction(action){perform(action);},
    onNextChapter(){if(view.busy)return;if(commitAction({type:'nextChapter'})){position={x:0,z:5.8};yaw=0;view.menu=null;dirtyPosition=true;}},
    onRetry(){
      if(saveBlocked){load();return;}
      if(view.busy)return;
      if(retryAction){commitAction(retryAction);return;}
      if(state.chapter==='harvest'&&state.rainChallenge.status!=='active'){
        if(commitAction({type:'retry'})){view.menu=null;route=null;}
      }else{view.error='';savePosition();}
    },
    onResetPrompt(){if(!view.busy){view.menu='reset';stopMoving();}},
    onReset(){reset();},onResetConfirm(){reset();},
    onDismissError(){if(!saveBlocked)view.error='';},
  };
  function syncView(){
    view.state=state;view.stage=state?state.stage:'character';view.objective=state?model.getObjective(state):null;view.report=state?model.chapterSummary(state):null;
    view.targets=state?world.getTargets(state).map(t=>({...t,interaction:model.getAvailableInteraction(state,t.id)})):[];
  }
  function packet(next,at){return {schemaVersion:1,state:next,position:{x:at.x,z:at.z},yaw};}
  function write(next,at){platform.write(model.STORAGE_KEY,packet(next,at||position));}
  function load(){
    cancelWork();stopMoving();
    try{
      const saved=platform.read(model.STORAGE_KEY);
      if(saved===null){state=model.createState();position={x:0,z:7};yaw=0;write(state);}
      else{
        if(saved.schemaVersion!==1||!model.validateState(saved.state)||!saved.position||
          !Number.isFinite(saved.position.x)||!Number.isFinite(saved.position.z)||
          !canStand(saved.position.x,saved.position.z,world.getObstacles()))throw new Error('这份下地体验存档无法读取。原存档已保留，请重试或确认重新开始。');
        state=saved.state;position={...saved.position};yaw=Number.isFinite(saved.yaw)?saved.yaw:0;
      }
      saveBlocked=false;retryAction=null;view.error='';view.menu=null;previewCharacter=state.characterId||'lin';view.previewCharacter=previewCharacter;
      syncView();if(state.stage==='complete'||(state.chapter==='harvest'&&state.rainChallenge.status==='failed'))view.menu='report';view.status='摇杆走动，靠近苗木和工具开始干活';
    }catch(e){
      saveBlocked=true;view.error=e.message;view.menu='reset';state=model.createState();syncView();
    }
  }
  function reset(){
    cancelWork();stopMoving();
    const next=model.createState(),at={x:0,z:7};
    try{write(next,at);state=next;position=at;yaw=0;saveBlocked=false;retryAction=null;view.error='';view.menu=null;previewCharacter='lin';view.previewCharacter='lin';syncView();view.status='新的一株葡萄，等你亲手种下';}
    catch(e){view.error=e.message;platform.announce(e.message);}
  }
  function commitAction(action){
    if(saveBlocked)return false;
    try{
      const next=model.applyAction(state,action);write(next);state=next;dirtyPosition=false;retryAction=null;
      view.error='';view.status=(next.lastResult&&next.lastResult.text)||'进度已保存';syncView();
      if(action.type!=='selectCharacter' && ((state.stage==='complete' && action.type!=='deliver') ||
         (state.chapter==='harvest' && state.rainChallenge.status==='failed') ||
         (action.type==='deliver' && state.stage==='complete')))view.menu='report';
      platform.announce(view.status);return true;
    }catch(e){retryAction=e.code==='STORAGE_WRITE'?{...action}:null;view.error=e.message;platform.announce(e.message);return false;}
  }
  function stopMoving(){route=null;keys.clear();view.joystick={x:0,y:0};joystickPointer=null;cameraPointer=null;}
  function cancelWork(keepHold){pending=null;if(!keepHold)holdPointer=null;view.busy=false;view.interactionProgress=null;}
  function targets(){return state?world.getTargets(state):[];}
  function nearest(){
    if(!state||state.stage==='character')return null;
    let best=null;
    for(const t of targets()){
      const distance=Math.hypot(position.x-t.x,position.z-t.z);
      if(distance>(t.radius||1.65))continue;
      const interaction=model.getAvailableInteraction(state,t.id);
      if(!interaction)continue;
      const n={...t,distance,interaction};
      if(!best||(interaction.enabled&&!best.interaction.enabled)||(interaction.enabled===best.interaction.enabled&&distance<best.distance))best=n;
    }
    return best;
  }
  function animationType(action){
    if(action.type==='interact')return ['takeSeedling','dig','plant','support','fillWater','water','name'][state.nursery.step]||'plant';
    return {harvest:'harvest',drain:'drain',protect:'protect',deliver:'pack',wait:'inspect'}[action.type]||action.type;
  }
  function startWork(action,target,duration,hold){
    if(view.busy||saveBlocked||view.menu||state.stage==='character')return;
    const interaction=model.getAvailableInteraction(state,target.id);
    if(!interaction||!interaction.enabled){view.error=interaction?interaction.hint:'先完成当前任务';return;}
    if(Math.hypot(position.x-target.x,position.z-target.z)>(target.radius||1.65)){view.status='先走到'+target.label+'旁边';return;}
    stopMoving();view.error='';view.busy=true;
    yaw=Math.atan2(position.x-target.x,position.z-target.z);
    pending={action:{...action},target:{...target},elapsed:0,duration:duration||1.8,hold:!!hold,type:animationType(action),version:state.version};
    view.interactionProgress=0;view.status=interaction.label+'中…';
  }
  function interact(hold){
    if(view.menu||view.busy||saveBlocked)return;
    const n=nearest();
    if(!n){view.guide=true;view.status='靠近苗木、土地或工具后才能操作';return;}
    if(n.id==='delivery'){view.menu='delivery';stopMoving();return;}
    startWork(n.interaction.action,n,n.interaction.duration,hold);
  }
  function perform(action){
    if(!action||view.busy)return;
    if(action.type==='reset'){reset();return;}
    if(saveBlocked)return;
    if(action.type==='selectCharacter'){view.onChooseCharacter(action.characterId);return;}
    if(action.type==='retry'){view.onRetry();return;}
    if(action.type==='nextChapter'){view.onNextChapter();return;}
    const id=action.type==='deliver'?'delivery':action.type==='harvest'?action.plotId:action.type==='protect'?'shelter':action.type==='wait'?'clock':action.type;
    const target=targets().find(t=>t.id===id);
    if(!target){view.error='请先走到对应的田间位置';return;}
    view.menu=null;startWork(action,target,action.type==='deliver'?1.4:1.8,false);
  }
  function navigate(id){
    if(view.busy||saveBlocked||state.stage==='character')return;
    const target=targets().find(t=>t.id===id);if(!target)return;
    const path=findPath(position,target,world.getObstacles());
    if(!path){view.error='这条路暂时走不过去，请用摇杆绕到旁边';return;}
    route=path;view.menu=null;view.guide=false;view.status='正在走向'+target.label+'，摇杆可随时接管';
  }
  function contains(c,x,y){return x>=c.x&&x<=c.x+c.width&&y>=c.y&&y<=c.y+c.height;}
  function updateJoystick(x,y){
    const r=hud.getJoystickRect?hud.getJoystickRect():{x:82,y:platform.info().height-100,radius:48};
    let dx=(x-r.x)/r.radius,dy=(y-r.y)/r.radius;
    const length=Math.hypot(dx,dy);if(length>1){dx/=length;dy/=length;}
    view.joystick={x:dx,y:dy};route=null;
  }
  platform.onInput(e=>{
    if(e.phase==='blur'){stopMoving();cancelWork();return;}
    if(e.phase==='key'){
      if(e.key==='Escape'&&e.down){cancelWork();view.onCloseMenu();return;}
      if(e.key==='KeyE'){
        if(e.down&&!e.repeat&&!view.busy){holdPointer='keyboard';interact(true);}
        else if(!e.down&&holdPointer==='keyboard'){if(pending&&pending.hold){cancelWork();view.status='已暂停操作，进度与资源保持原样';}holdPointer=null;}return;
      }
      if(e.down){keys.add(e.key);route=null;if(pending)cancelWork();}else keys.delete(e.key);return;
    }
    if(e.phase==='control-start'){consumedPointers.add(e.id);if(!view.busy){holdPointer=e.id;interact(true);}return;}
    if(e.phase==='control-end'){consumedPointers.delete(e.id);if(e.id===holdPointer){if(pending&&pending.hold){cancelWork();view.status='已暂停操作，进度与资源保持原样';}holdPointer=null;}return;}
    if(e.phase==='start'){
      const c=hud.getControls().find(c=>contains(c,e.x,e.y));
      if(c){if(c.id==='field-interact'&&!c.disabled){consumedPointers.add(e.id);if(!view.busy){holdPointer=e.id;interact(true);}}return;}
      if(view.menu||state.stage==='character'||saveBlocked)return;
      const r=hud.getJoystickRect?hud.getJoystickRect():{x:82,y:info.height-100,radius:48};
      if(Math.hypot(e.x-r.x,e.y-r.y)<r.radius+24){joystickPointer=e.id;if(pending)cancelWork();updateJoystick(e.x,e.y);}
      else cameraPointer={id:e.id,x:e.x,y:e.y,drag:0};
    }else if(e.phase==='move'){
      if(e.id===joystickPointer){updateJoystick(e.x,e.y);return;}
      if(cameraPointer&&e.id===cameraPointer.id){const delta=e.x-cameraPointer.x;cameraPointer.drag+=Math.abs(delta);cameraYaw-=delta*0.007;cameraPointer.x=e.x;cameraPointer.y=e.y;}
    }else if(e.phase==='end'||e.phase==='cancel'){
      if(consumedPointers.has(e.id)){consumedPointers.delete(e.id);if(e.id===holdPointer){if(pending&&pending.hold){cancelWork();view.status='已暂停操作，进度与资源保持原样';}holdPointer=null;}return;}
      if(e.id===joystickPointer){view.joystick={x:0,y:0};joystickPointer=null;savePosition();return;}
      if(cameraPointer&&e.id===cameraPointer.id){const moved=cameraPointer.drag;cameraPointer=null;if(moved>8)return;}
      if(e.phase==='end'&&platform.native)hud.hit(e.x,e.y);
    }
  });
  function savePosition(){
    if(!dirtyPosition||saveBlocked)return;
    try{write(state);dirtyPosition=false;}catch(e){view.error=e.message;platform.announce(e.message);}
  }
  function tick(dt){
    let moving=false;
    if(!view.menu&&state.stage!=='character'&&!saveBlocked){
      let dx=view.joystick.x,dz=view.joystick.y;
      if(keys.has('KeyW')||keys.has('ArrowUp'))dz-=1;if(keys.has('KeyS')||keys.has('ArrowDown'))dz+=1;
      if(keys.has('KeyA')||keys.has('ArrowLeft'))dx-=1;if(keys.has('KeyD')||keys.has('ArrowRight'))dx+=1;
      if(route&&route.length){
        const goal=route[0],dist=Math.hypot(goal.x-position.x,goal.z-position.z);
        if(dist<0.16){route.shift();if(!route.length){route=null;savePosition();view.status='已到达，靠近对象开始操作';}}
        else{dx=(goal.x-position.x)/dist;dz=(goal.z-position.z)/dist;}
      }else{
        const oldX=dx;dx=oldX*Math.cos(cameraYaw)+dz*Math.sin(cameraYaw);dz=dz*Math.cos(cameraYaw)-oldX*Math.sin(cameraYaw);
      }
      const magnitude=Math.hypot(dx,dz);
      if(magnitude>0.08&&!pending){
        const before=position;const speed=2.5*dt*Math.min(1,magnitude);
        position=move(position,dx/magnitude*speed,dz/magnitude*speed,world.getObstacles());
        moving=Math.hypot(position.x-before.x,position.z-before.z)>0.0001;
        if(moving){yaw=Math.atan2(-dx,-dz);dirtyPosition=true;}
      }
    }
    if(pending){
      pending.elapsed+=dt;view.interactionProgress=Math.min(1,pending.elapsed/pending.duration);
      if(view.interactionProgress>=1){
        const work=pending;cancelWork(true);
        if(work.version===state.version&&Math.hypot(position.x-work.target.x,position.z-work.target.z)<=(work.target.radius||1.65))commitAction(work.action);
      }
    }
    view.moving=moving;view.nearest=nearest();
    world.update(dt,{characterId:(view.menu==='characters'||state.stage==='character')?previewCharacter:state.characterId,
      position,yaw,cameraYaw,moving,preview:view.menu==='characters'||state.stage==='character',
      action:pending?{type:pending.type,progress:view.interactionProgress}:null,state});
  }
  function draw(timestamp){
    if(!running||destroyed)return;
    const now=Number.isFinite(timestamp)?timestamp:Date.now();
    const dt=lastTime===null?0:Math.min(0.05,Math.max(0,(now-lastTime)/1000));lastTime=now;
    tick(dt);
    if(!lastPaint||now-lastPaint>=32){
      hud.render(view,dt);platform.finishControls();
      if(overlay){overlay.texture.needsUpdate=true;renderer.clear();}
      renderer.render(world.scene,world.camera);
      if(overlay){renderer.clearDepth();renderer.render(overlay.scene,overlay.camera);}
      lastPaint=now;
    }
    frame=platform.frame(draw);
  }
  function resize(i){
    renderer.setPixelRatio(i.dpr);renderer.setSize(i.width,i.height,false);world.resize(i.width,i.height);
    hud.resize(i.width,i.height,i.dpr,i.safeTop,i.safeBottom);if(overlay)overlay.texture.needsUpdate=true;
  }
  platform.onResize(resize);
  platform.onHide(()=>{running=false;platform.cancelFrame(frame);frame=null;savePosition();stopMoving();cancelWork();consumedPointers.clear();lastTime=null;});
  platform.onShow(()=>{if(destroyed)return;running=true;lastTime=null;if(frame===null)frame=platform.frame(draw);});
  load();frame=platform.frame(draw);
  return {
    ready:Promise.resolve(),
    getState(){return JSON.parse(JSON.stringify(state));},
    getStatus(){return {menu:view.menu,busy:view.busy,error:view.error,status:view.status,guide:view.guide,previewCharacter,
      position:{...position},yaw,nearest:view.nearest?{id:view.nearest.id,distance:view.nearest.distance,enabled:view.nearest.interaction.enabled}:null,
      interactionProgress:view.interactionProgress,navigating:!!route,saveBlocked};},
    getControls(){return hud.getControls().map(({id,label,x,y,width,height,disabled})=>({id,label,x,y,width,height,disabled}));},
    getDebug(){return {world:world.getDebug(),renderer:{calls:renderer.info.render.calls,triangles:renderer.info.render.triangles,geometries:renderer.info.memory.geometries,textures:renderer.info.memory.textures},native:platform.native};},
    destroy(){if(destroyed)return;savePosition();destroyed=true;running=false;platform.cancelFrame(frame);platform.destroy();hud.dispose();world.dispose();
      if(overlay){overlay.texture.dispose();overlay.material.dispose();overlay.mesh.geometry.dispose();}renderer.dispose();}
  };
}
module.exports={start};
