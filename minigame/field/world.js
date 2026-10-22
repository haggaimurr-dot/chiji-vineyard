'use strict';

const { PLANTING_STEPS } = require('./model');

// Shared WebGL scene. All resources are made locally, including sign textures.
// Gameplay owns movement and persistence; this module only depicts that state.
function createWorld(options) {
  const { THREE, renderer, createCanvas } = options;
  const scene = new THREE.Scene();
  scene.background = new THREE.Color('#D6E3DB');
  scene.fog = new THREE.Fog('#D6E3DB', 21, 57);
  const camera = new THREE.PerspectiveCamera(47, (options.width || 390) / (options.height || 844), 0.1, 100);
  camera.position.set(0, 4.4, 13.6); camera.lookAt(0, 1, 5.1);
  const geometries = [], materials = [], textures = [], characters = new Map();
  const waterOrientation = new THREE.Quaternion();
  const roots = new THREE.Group(); scene.add(roots);
  const shared = {};
  let elapsed = 0, currentState = null, disposed = false, currentCharacter = null, rainLevel = 0;
  const debug = { characterId: 'lin', characterChanges: 0, frames: 0, action: null, moving: false };
  const makeGeometry = geometry => { geometries.push(geometry); return geometry; };
  const mat = (color, extra) => {
    const result = new THREE.MeshStandardMaterial(Object.assign({ color, roughness: 0.91, metalness: 0 }, extra));
    materials.push(result); return result;
  };
  const palette = {
    earth: mat('#B8AD8A'), path: mat('#CDBB91'), soil: mat('#886B48'), moist: mat('#665C41'),
    hole: mat('#4C432F'), grass: mat('#8F9B66'), grassLight: mat('#ADB581'),
    wood: mat('#8A7659'), darkWood: mat('#6B604B'), bamboo: mat('#AFAC77'),
    leaf: mat('#657F42'), leafLight: mat('#95AB60'), darkLeaf: mat('#4F683B'),
    greenGrape: mat('#B1BC69', { roughness: 0.5 }), ripeGrape: mat('#9EAC54', { roughness: 0.48 }),
    damagedGrape: mat('#9E865D'), metal: mat('#799A95', { roughness: 0.6 }),
    steel: mat('#ADB9AC', { roughness: 0.55 }), cream: mat('#E7DDC2'), roof: mat('#95715B'),
    water: mat('#749CA1', { transparent: true, opacity: 0.78, roughness: 0.34 }),
    cloth: mat('#D9DDD1', { side: THREE.DoubleSide, transparent: true, opacity: 0.57 }),
  };
  shared.box = makeGeometry(new THREE.BoxGeometry(1, 1, 1));
  shared.ball = makeGeometry(new THREE.SphereGeometry(1, 12, 8));
  shared.round = makeGeometry(new THREE.SphereGeometry(1, 16, 12));
  shared.cylinder = makeGeometry(new THREE.CylinderGeometry(1, 1, 1, 10));
  shared.cone = makeGeometry(new THREE.ConeGeometry(1, 1, 8));
  shared.circle = makeGeometry(new THREE.CircleGeometry(1, 28));
  shared.torus = makeGeometry(new THREE.TorusGeometry(1, 0.13, 6, 28));
  shared.torso = makeGeometry(new THREE.LatheGeometry([
    new THREE.Vector2(0, 0.09), new THREE.Vector2(0.16, 0.09), new THREE.Vector2(0.174, 0.12),
    new THREE.Vector2(0.181, 0.25), new THREE.Vector2(0.183, 0.39), new THREE.Vector2(0.202, 0.55),
    new THREE.Vector2(0.186, 0.613), new THREE.Vector2(0.08, 0.685), new THREE.Vector2(0, 0.685)
  ], 18));
  const mesh = (geometry, material, position, scale, parent, shadow) => {
    const node = new THREE.Mesh(geometry, material);
    if (position) node.position.set(position[0], position[1], position[2]);
    if (scale) node.scale.set(scale[0], scale[1], scale[2]);
    node.castShadow = shadow !== false; node.receiveShadow = true;
    (parent || roots).add(node); return node;
  };
  const box = (material, x, y, z, sx, sy, sz, parent) => mesh(shared.box, material, [x, y, z], [sx, sy, sz], parent);
  const ball = (material, x, y, z, sx, sy, sz, parent) => mesh(shared.ball, material, [x, y, z], [sx, sy, sz], parent);
  const cylinder = (material, x, y, z, radius, length, parent) => mesh(shared.cylinder, material, [x, y, z], [radius, length, radius], parent);
  function branch(material, a, b, radius, parent) {
    const from = new THREE.Vector3(...a), to = new THREE.Vector3(...b), direction = to.clone().sub(from);
    const node = cylinder(material, 0, 0, 0, radius, direction.length(), parent);
    node.position.copy(from.add(to).multiplyScalar(0.5));
    node.quaternion.setFromUnitVectors(new THREE.Vector3(0, 1, 0), direction.normalize()); return node;
  }
  function flat(material, x, z, radius, parent, y) {
    const node = mesh(shared.circle, material, [x, y || 0.018, z], [radius, radius, radius], parent, false);
    node.rotation.x = -Math.PI / 2; return node;
  }
  const obstacles = [];
  const blocker = (id, x, z, radius) => obstacles.push({ id, type: 'circle', x, z, radius });
  const solid = (id, minX, maxX, minZ, maxZ) => obstacles.push({ id, type: 'aabb', minX, maxX, minZ, maxZ });
  let seed = 7143;
  const random = () => { seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0; return seed / 4294967296; };

  function groundTexture(base, grassy, repeats) {
    if (typeof createCanvas !== 'function') return null;
    let canvas, context;
    try { canvas = createCanvas(); canvas.width = canvas.height = 512; context = canvas.getContext('2d'); }
    catch (_) { return null; }
    if (!context) return null;
    context.fillStyle = base; context.fillRect(0, 0, 512, 512);
    for (let i = 0; i < 1800; i++) {
      const x = random() * 512, y = random() * 512, radius = random() * 2 + 0.35;
      context.fillStyle = i % 3 === 0 ? 'rgba(55,55,36,0.12)' : 'rgba(236,224,189,0.22)';
      context.beginPath();
      if (typeof context.ellipse === 'function') context.ellipse(x, y, radius * 1.65, radius * 0.55, random(), 0, Math.PI * 2);
      else context.arc(x, y, radius, 0, Math.PI * 2);
      context.fill();
    }
    for (let i = 0; i < 120; i++) {
      const x = random() * 512, y = random() * 512;
      context.strokeStyle = grassy ? 'rgba(92,119,62,0.35)' : 'rgba(105,89,62,0.12)'; context.lineWidth = grassy ? 1.4 : 0.7;
      context.beginPath(); context.moveTo(x, y); context.lineTo(x + 2, y - 4 - random() * 6); context.stroke();
    }
    const texture = new THREE.CanvasTexture(canvas); texture.colorSpace = THREE.SRGBColorSpace;
    texture.wrapS = texture.wrapT = THREE.RepeatWrapping; texture.repeat.set(repeats, repeats); textures.push(texture);
    return texture;
  }
  const grassTexture = groundTexture('#A0A783', true, 24);
  const earthTexture = groundTexture('#B9AD8B', false, 5);
  const soilTexture = groundTexture('#8D7556', false, 1.4);
  if (grassTexture) { palette.grass.map = grassTexture; palette.grass.color.set('#FFFFFF'); }
  if (earthTexture) { palette.earth.map = earthTexture; palette.earth.color.set('#FFFFFF'); }
  if (soilTexture) { palette.soil.map = soilTexture; palette.soil.color.set('#FFFFFF'); }

  const ambient = new THREE.HemisphereLight('#FFF4DC', '#7D8669', 2.05); scene.add(ambient);
  const sun = new THREE.DirectionalLight('#FFF1D0', 2.55);
  sun.position.set(-9, 15, 7); sun.castShadow = true;
  sun.shadow.mapSize.set(1024, 1024); Object.assign(sun.shadow.camera, { left: -13, right: 13, top: 15, bottom: -11, near: 1, far: 42 });
  sun.shadow.bias = -0.0005; sun.shadow.normalBias = 0.035; scene.add(sun); scene.add(sun.target);
  if (renderer && renderer.shadowMap) { renderer.shadowMap.enabled = true; renderer.shadowMap.type = THREE.PCFSoftShadowMap; }

  const terrain = mesh(makeGeometry(new THREE.PlaneGeometry(90, 90)), palette.grass, [0, -0.035, 0], null, roots, false);
  terrain.rotation.x = -Math.PI / 2;
  const orchardGround = mesh(makeGeometry(new THREE.PlaneGeometry(16, 18)), palette.earth, [0, -0.019, 0], null, roots, false);
  orchardGround.rotation.x = -Math.PI / 2;
  box(palette.path, 0, -0.01, 0, 2.1, 0.03, 19);
  box(palette.path, 0, -0.008, 4.5, 13.5, 0.03, 1.65);
  box(palette.path, 1.5, -0.005, -1.9, 12.7, 0.03, 1.1);
  const fieldEdge = mesh(shared.circle, palette.grass, [0, -0.05, 0], [23, 23, 23], roots, false); fieldEdge.rotation.x = -Math.PI / 2;

  function tree(x, z, size, distant) {
    const trunk = cylinder(palette.wood, x, size * 0.47, z, size * 0.1, size * 0.96);
    branch(palette.wood, [x, size * 0.6, z], [x - size * 0.35, size * 1.12, z], size * 0.065);
    ball(palette.darkLeaf, x - size * 0.22, size * 1.17, z, size * 0.58, size * 0.69, size * 0.5);
    ball(palette.leaf, x + size * 0.22, size * 1.4, z + size * 0.13, size * 0.63, size * 0.62, size * 0.57);
    ball(palette.leafLight, x - size * 0.12, size * 1.69, z, size * 0.51, size * 0.49, size * 0.51);
    if (!distant) blocker('tree', x, z, size * 0.18);
    return trunk;
  }
  [[-9, 6, 2.7], [9, 2, 2.4], [-9.5, -6, 3.2], [8.8, -9, 3], [-12, -14, 3.7], [11, -15, 3.5], [3, -17, 3.2]].forEach(([x, z, size]) => tree(x, z, size, true));
  const mountainMaterials = ['#899C86', '#A3B29A', '#B7C4AC'].map(color => mat(color));
  for (let i = 0; i < 8; i++) {
    const height = 5 + random() * 6;
    const node = mesh(shared.cone, mountainMaterials[i % 3], [-29 + i * 8, height / 2 - 0.2, -30 - random() * 9], [8 + random() * 5, height, 6 + random() * 5], roots, false);
    node.rotation.y = random() * 0.8;
  }
  const cloudMaterial = mat('#F1F0DF', { transparent: true, opacity: 0.72 });
  const clouds = new THREE.Group(); scene.add(clouds);
  for (let i = 0; i < 6; i++) {
    const x = -19 + i * 7, y = 10 + random() * 5, z = -18 - random() * 11;
    for (let k = 0; k < 3; k++) ball(cloudMaterial, x + k * 1.8, y + (k === 1 ? 0.5 : 0), z, 1.9, 0.58, 0.95, clouds);
  }
  const grassGeometry = makeGeometry(new THREE.ConeGeometry(0.032, 0.26, 3));
  const grass = new THREE.InstancedMesh(grassGeometry, palette.grassLight, 330); roots.add(grass);
  const dummy = new THREE.Object3D();
  for (let i = 0; i < 330; i++) {
    let x = random() * 23 - 11.5, z = random() * 25 - 12;
    if (Math.abs(x) < 7.5 && z > -9 && z < 8) x = Math.sign(x || 1) * (7.7 + random() * 2.8);
    dummy.position.set(x, 0.09, z); dummy.rotation.set(0, random() * Math.PI, random() * 0.15);
    dummy.scale.set(0.7 + random(), 0.5 + random(), 1); dummy.updateMatrix(); grass.setMatrixAt(i, dummy.matrix);
  }
  grass.instanceMatrix.needsUpdate = true;

  function fenceSegment(x1, z1, x2, z2, count, parent) {
    for (let i = 0; i <= count; i++) {
      const t = i / count, x = x1 + (x2 - x1) * t, z = z1 + (z2 - z1) * t;
      cylinder(palette.wood, x, 0.62, z, 0.065, 1.26, parent);
    }
    [0.41, 0.9].forEach(y => branch(palette.bamboo, [x1, y, z1], [x2, y, z2], 0.045, parent));
  }
  const frontBoundary = new THREE.Group(); roots.add(frontBoundary);
  fenceSegment(-8, -8.7, -8, 8.7, 10); fenceSegment(8, -8.7, 8, 8.7, 10);
  fenceSegment(-8, -8.7, 8, -8.7, 12); fenceSegment(-8, 8.7, -1.9, 8.7, 4, frontBoundary); fenceSegment(1.9, 8.7, 8, 8.7, 4, frontBoundary);
  cylinder(palette.darkWood, -2, 1.5, 8.6, 0.11, 3, frontBoundary); cylinder(palette.darkWood, 2, 1.5, 8.6, 0.11, 3, frontBoundary);
  box(palette.wood, 0, 2.87, 8.6, 4.35, 0.36, 0.2, frontBoundary);
  solid('left-fence', -8.3, -7.85, -9, 9); solid('right-fence', 7.85, 8.3, -9, 9);
  solid('back-fence', -8.3, 8.3, -9, -8.5);

  // A practical tool shed and nursery bench, with no external assets.
  const shed = new THREE.Group(); shed.position.set(-6.5, 0, 5.3); roots.add(shed);
  box(palette.cream, 0, 1.15, 0, 2.15, 2.3, 1.9, shed);
  const roof = box(palette.roof, 0, 2.45, 0, 2.55, 0.16, 2.22, shed); roof.rotation.x = -0.09;
  box(palette.darkWood, 0.23, 0.96, 0.975, 0.88, 1.86, 0.075, shed);
  box(palette.metal, -0.59, 1.45, 0.975, 0.45, 0.54, 0.085, shed);
  box(palette.wood, -0.59, 1.45, 1.025, 0.025, 0.55, 0.025, shed);
  branch(palette.wood, [1.18, 0.13, 0.87], [1.34, 1.86, 0.76], 0.04, shed);
  box(palette.steel, 1.18, 0.18, 0.9, 0.25, 0.3, 0.035, shed);
  solid('tool-shed', -7.62, -5.36, 4.32, 6.3);
  const nursery = new THREE.Group(); nursery.position.set(-4, 0, 4); roots.add(nursery);
  box(palette.wood, 0, 0.68, 0, 1.45, 0.13, 0.78, nursery);
  [-0.58, 0.58].forEach(x => [-0.26, 0.26].forEach(z => cylinder(palette.darkWood, x, 0.33, z, 0.045, 0.66, nursery)));
  const potMaterial = mat('#796850');
  for (let i = 0; i < 8; i++) {
    const x = -0.5 + (i % 4) * 0.33, z = -0.18 + Math.floor(i / 4) * 0.32;
    cylinder(potMaterial, x, 0.81, z, 0.13, 0.23, nursery);
    cylinder(palette.leaf, x, 1.05, z, 0.011, 0.38, nursery);
    [-1, 1].forEach(side => { const leaf = ball(palette.leafLight, x + side * 0.085, 1.13, z, 0.11, 0.025, 0.07, nursery); leaf.rotation.z = side * 0.35; });
  }
  solid('seedling-bench', -4.73, -3.27, 3.6, 4.42);
  const tank = cylinder(palette.metal, 5.1, 0.83, 4.05, 0.64, 1.6);
  cylinder(palette.darkWood, 5.1, 1.65, 4.05, 0.67, 0.08);
  branch(palette.steel, [4.55, 0.68, 4.05], [4.1, 0.68, 4.05], 0.04);
  branch(palette.steel, [4.1, 0.68, 4.05], [4.1, 0.49, 4.05], 0.04);
  flat(palette.water, 4.12, 4.05, 0.24, roots, 0.022); blocker('water-tank', 5.1, 4.05, 0.68);
  const wateringCan = new THREE.Group(); roots.add(wateringCan); wateringCan.position.set(4.02, 0.15, 4.66);
  cylinder(palette.metal, 0, 0.1, 0, 0.18, 0.24, wateringCan);
  branch(palette.metal, [0.1, 0.1, 0], [0.4, 0.22, 0], 0.045, wateringCan);
  const handle = mesh(shared.torus, palette.steel, [-0.1, 0.14, 0], [0.17, 0.17, 0.1], wateringCan); handle.rotation.y = Math.PI / 2;

  const youngGarden = new THREE.Group(); youngGarden.position.set(-2, 0, 0); roots.add(youngGarden);
  box(palette.soil, 0, -0.018, 0, 2.3, 0.08, 2.35, youngGarden);
  const wetSoil = flat(palette.moist, 0, 0, 0.71, youngGarden, 0.036); wetSoil.visible = false;
  const hole = flat(palette.hole, 0, 0, 0.36, youngGarden, 0.037); hole.visible = false;
  const rim = mesh(shared.torus, palette.soil, [0, 0.07, 0], [0.4, 0.4, 0.4], youngGarden); rim.rotation.x = Math.PI / 2; rim.visible = false;
  const sprout = new THREE.Group(); youngGarden.add(sprout);
  cylinder(palette.wood, 0, 0.37, 0, 0.021, 0.72, sprout);
  for (let i = 0; i < 6; i++) {
    const a = i * 2.23, y = 0.29 + i * 0.067;
    const leaf = ball(i % 2 ? palette.leaf : palette.leafLight, Math.cos(a) * 0.105, y, Math.sin(a) * 0.11, 0.16, 0.025, 0.09, sprout);
    leaf.rotation.set(Math.sin(a) * 0.3, a, Math.cos(a) * 0.3);
  }
  sprout.visible = false;
  const support = cylinder(palette.bamboo, 0.22, 0.68, 0.08, 0.03, 1.36, youngGarden); support.visible = false;
  const ties = new THREE.Group(); youngGarden.add(ties); ties.visible = false;
  [0.38, 0.68].forEach(y => branch(palette.metal, [0, y, 0], [0.24, y, 0.08], 0.017, ties));
  const namePost = cylinder(palette.darkWood, 0.7, 0.4, 0.58, 0.027, 0.8, youngGarden); namePost.visible = false;
  let nameSign = null, currentName = '';

  function sign(label, x, y, z, size, parent, color) {
    if (typeof createCanvas !== 'function') return null;
    let canvas;
    try { canvas = createCanvas(); if (!canvas) return null; canvas.width = 512; canvas.height = 128; }
    catch (_) { return null; }
    const context = canvas.getContext('2d'); if (!context) return null;
    context.clearRect(0, 0, 512, 128);
    context.fillStyle = '#F5F0E2'; context.fillRect(4, 4, 504, 120);
    context.strokeStyle = '#D7D9C9'; context.lineWidth = 4; context.strokeRect(5, 5, 502, 118);
    context.font = '500 35px "Microsoft YaHei",sans-serif'; context.textAlign = 'center'; context.textBaseline = 'middle';
    context.fillStyle = color || '#315440'; context.fillText(label, 256, 65, 470);
    const texture = new THREE.CanvasTexture(canvas); texture.colorSpace = THREE.SRGBColorSpace; textures.push(texture);
    const material = new THREE.SpriteMaterial({ map: texture, depthWrite: false }); materials.push(material);
    const sprite = new THREE.Sprite(material); sprite.position.set(x, y, z); sprite.scale.set(size || 1.8, (size || 1.8) / 4, 1);
    (parent || roots).add(sprite); return sprite;
  }
  sign('迟记 · 葡萄园', 0, 2.86, 8.68, 3.2, frontBoundary);

  const matureGarden = new THREE.Group(); roots.add(matureGarden);
  const rows = {};
  function matureRow(id, x, z) {
    const group = new THREE.Group(); group.position.set(x, 0, z); matureGarden.add(group);
    box(palette.soil, 0, -0.012, 0, 3.3, 0.09, 1.45, group);
    [-1.76, 1.76].forEach(px => [-0.8, 0.8].forEach(pz => cylinder(palette.wood, px, 1.13, pz, 0.048, 2.3, group)));
    [-0.77, 0, 0.77].forEach(pz => branch(palette.steel, [-1.82, 2.04, pz], [1.82, 2.04, pz], 0.011, group));
    [-1.05, 0, 1.05].forEach(px => {
      branch(palette.darkWood, [px, 0, 0], [px + 0.12, 1.94, 0], 0.042, group);
      branch(palette.wood, [px + 0.08, 1.48, 0], [px - 0.44, 1.95, 0.36], 0.025, group);
      branch(palette.wood, [px + 0.1, 1.68, 0], [px + 0.5, 1.98, -0.4], 0.025, group);
    });
    const foliage = new THREE.Group(); group.add(foliage);
    const leaves = new THREE.InstancedMesh(shared.ball, palette.leaf, 105); foliage.add(leaves);
    for (let i = 0; i < 105; i++) {
      dummy.position.set(random() * 3.24 - 1.62, 1.98 + random() * 0.3, random() * 1.34 - 0.67);
      dummy.rotation.set(random() * 0.7 - 0.35, random() * Math.PI, random() * 0.7 - 0.35);
      dummy.scale.set(0.18 + random() * 0.13, 0.025 + random() * 0.018, 0.1 + random() * 0.07);
      dummy.updateMatrix(); leaves.setMatrixAt(i, dummy.matrix);
    }
    leaves.instanceMatrix.needsUpdate = true; leaves.castShadow = true;
    const grapes = new THREE.InstancedMesh(shared.ball, palette.ripeGrape, 150); group.add(grapes);
    for (let bunch = 0; bunch < 15; bunch++) {
      const gx = -1.42 + (bunch % 5) * 0.7, gz = -0.54 + Math.floor(bunch / 5) * 0.52;
      branch(palette.leaf, [gx, 2.04, gz], [gx, 1.9, gz], 0.008, group);
      for (let berry = 0; berry < 10; berry++) {
        const row = berry < 3 ? 0 : berry < 6 ? 1 : berry < 9 ? 2 : 3;
        const col = berry < 9 ? berry % 3 - 1 : 0;
        dummy.position.set(gx + col * (row === 2 ? 0.041 : 0.055), 1.89 - row * 0.077, gz + (berry % 2 ? 0.026 : -0.028));
        dummy.rotation.set(0, 0, 0); dummy.scale.set(0.045, 0.057, 0.045); dummy.updateMatrix(); grapes.setMatrixAt(bunch * 10 + berry, dummy.matrix);
      }
    }
    grapes.instanceMatrix.needsUpdate = true; grapes.castShadow = true;
    const covers = new THREE.Group(); group.add(covers); covers.visible = false;
    const canopy = box(palette.cloth, 0, 2.49, 0, 3.9, 0.045, 2.03, covers); canopy.rotation.z = 0.08;
    [-1.95, 1.95].forEach(px => [-1, 1].forEach(pz => cylinder(palette.steel, px, 1.25, pz, 0.025, 2.5, covers)));
    rows[id] = { group, foliage, grapes, covers, leaves, harvested: false, damage: 0 };
  }
  matureRow('sun', -3, -4); matureRow('creek', 3, -4); matureRow('slope', -3, -7);
  const coverGrass = new THREE.InstancedMesh(grassGeometry, palette.leafLight, 120); matureGarden.add(coverGrass);
  for (let i = 0; i < 120; i++) {
    const row = Object.values(rows)[i % 3];
    dummy.position.set(row.group.position.x + random() * 3.2 - 1.6, 0.1, row.group.position.z + random() * 1.2 - 0.6);
    dummy.rotation.set(random() * 0.18, random() * Math.PI, random() * 0.3 - 0.15);
    dummy.scale.set(0.9, 0.34 + random() * 0.45, 0.9); dummy.updateMatrix(); coverGrass.setMatrixAt(i, dummy.matrix);
  }
  coverGrass.instanceMatrix.needsUpdate = true;
  const gravelMaterial = mat('#C1B69D');
  const gravel = new THREE.InstancedMesh(shared.ball, gravelMaterial, 70); roots.add(gravel);
  for (let i = 0; i < 70; i++) {
    dummy.position.set((i % 2 ? 1 : -1) * (1.02 + random() * 0.29), 0.008, random() * 17.5 - 8.4);
    dummy.rotation.set(0, random() * Math.PI, 0); dummy.scale.set(0.045 + random() * 0.045, 0.018, 0.025 + random() * 0.02);
    dummy.updateMatrix(); gravel.setMatrixAt(i, dummy.matrix);
  }
  gravel.instanceMatrix.needsUpdate = true;
  matureGarden.visible = false;
  const ditch = box(palette.hole, 5.4, -0.007, -2, 0.48, 0.04, 9.8);
  const ditchWater = box(palette.water, 5.4, 0.011, -2, 0.36, 0.025, 9.3); ditchWater.visible = false;
  const blockedDrain = new THREE.Group(); roots.add(blockedDrain); blockedDrain.position.set(5.35, 0, -1);
  for (let i = 0; i < 6; i++) box(palette.wood, random() * 0.34 - 0.17, 0.04 + i * 0.026, random() * 0.6 - 0.3, 0.55, 0.06, 0.13, blockedDrain).rotation.y = random() * 2;

  const packing = new THREE.Group(); roots.add(packing); packing.position.set(0, 0, 5);
  box(palette.wood, 0, 0.64, 0, 1.3, 0.1, 0.65, packing);
  [-0.54, 0.54].forEach(x => [-0.23, 0.23].forEach(z => cylinder(palette.darkWood, x, 0.29, z, 0.045, 0.58, packing)));
  const crates = new THREE.Group(); packing.add(crates);
  [-0.33, 0.33].forEach(x => {
    box(palette.cream, x, 0.735, 0, 0.56, 0.035, 0.43, crates);
    [-0.21, 0.21].forEach(z => box(palette.cream, x, 0.835, z, 0.56, 0.19, 0.035, crates));
    [-0.23, 0.23].forEach(px => box(palette.wood, x + px, 0.83, 0, 0.03, 0.27, 0.47, crates));
  });
  const packedGrapes = new THREE.InstancedMesh(shared.ball, palette.ripeGrape, 48); packing.add(packedGrapes);
  for (let i = 0; i < 48; i++) {
    dummy.position.set((i < 24 ? -0.33 : 0.33) + (i % 4 - 1.5) * 0.095, 0.89 + Math.floor(i % 24 / 12) * 0.072, (Math.floor(i % 12 / 4) - 1) * 0.11);
    dummy.rotation.set(0, 0, 0); dummy.scale.set(0.056, 0.062, 0.052); dummy.updateMatrix(); packedGrapes.setMatrixAt(i, dummy.matrix);
  }
  packedGrapes.instanceMatrix.needsUpdate = true; packedGrapes.visible = false;
  solid('packing-table', -0.74, 0.74, 4.65, 5.38);
  const clockStand = new THREE.Group(); roots.add(clockStand); clockStand.position.set(5.6, 0, 6.3);
  cylinder(palette.wood, 0, 0.71, 0, 0.05, 1.42, clockStand);
  const clockFace = flat(palette.cream, 0, 0, 0.31, clockStand, 0); clockFace.rotation.x = 0; clockFace.position.set(0, 1.48, 0.035);
  branch(palette.darkWood, [0, 1.48, 0.045], [0, 1.66, 0.045], 0.014, clockStand);
  branch(palette.darkWood, [0, 1.48, 0.045], [0.12, 1.48, 0.045], 0.014, clockStand);
  blocker('clock', 5.6, 6.3, 0.27);

  const targetDefinitions = {
    seedling: { id: 'seedling', label: '挑选苗木', x: -4, z: 4, radius: 1.35 },
    plantingSpot: { id: 'plantingSpot', label: '种下葡萄苗', x: -2, z: 0, radius: 1.15 },
    support: { id: 'support', label: '立杆绑蔓', x: -2, z: 0, radius: 1.15 },
    water: { id: 'water', label: '取水浇灌', x: 4, z: 4, radius: 1.2 },
    nameplate: { id: 'nameplate', label: '给葡萄挂牌', x: -2, z: 0, radius: 1.15 },
    sun: { id: 'sun', label: '朝阳区葡萄', x: -3, z: -4, radius: 1.42 },
    creek: { id: 'creek', label: '溪边区葡萄', x: 3, z: -4, radius: 1.42 },
    slope: { id: 'slope', label: '坡地区葡萄', x: -3, z: -7, radius: 1.42 },
    drain: { id: 'drain', label: '清理排水沟', x: 5, z: -1, radius: 1.25 },
    shelter: { id: 'shelter', label: '架设避雨棚', x: 4, z: -4, radius: 1.15 },
    delivery: { id: 'delivery', label: '分拣装箱', x: 0, z: 5, radius: 1.35 },
    clock: { id: 'clock', label: '查看天色', x: 5, z: 6, radius: 1.1 },
  };
  const markers = {};
  const markerMaterial = mat('#F3E9B4', { transparent: true, opacity: 0.72, depthWrite: false });
  for (const id of Object.keys(targetDefinitions)) {
    const target = targetDefinitions[id], group = new THREE.Group(); group.position.set(target.x, 0, target.z); roots.add(group);
    const ring = mesh(shared.torus, markerMaterial, [0, 0.045, 0], [0.52, 0.52, 0.25], group, false); ring.rotation.x = Math.PI / 2;
    const label = sign(target.label, 0, id === 'sun' || id === 'creek' || id === 'slope' || id === 'shelter' ? 2.94 : 1.98, 0, 1.85, group);
    group.visible = false; markers[id] = { group, ring, label };
  }

  const rainMaterial = mat('#94AEB5', { transparent: true, opacity: 0.48, depthWrite: false });
  const rainGeometry = makeGeometry(new THREE.CylinderGeometry(0.005, 0.005, 0.2, 3));
  const rain = new THREE.InstancedMesh(rainGeometry, rainMaterial, 140); rain.visible = false; roots.add(rain);
  const raindrops = Array.from({ length: 140 }, () => ({ x: random() * 18 - 9, z: random() * 20 - 9, y: random() * 7 }));

  function getTargets(state) {
    if (!state || state.stage === 'character') return [];
    if (state.chapter === 'harvest') return ['sun', 'creek', 'slope', 'drain', 'shelter', 'delivery', 'clock'].map(id => ({ ...targetDefinitions[id] }));
    if (state.stage === 'complete') return [];
    const step = Number(state.nursery && state.nursery.step) || 0;
    const expected = PLANTING_STEPS[step];
    return expected && targetDefinitions[expected.targetId] ? [{ ...targetDefinitions[expected.targetId], label: expected.label }] : [];
  }

  function makeCharacter(id) {
    const styles = {
      lin: { shirt: '#6A8562', trousers: '#726F57', skin: '#D5AB83', hat: '#C9B482', band: '#536749', hair: '#40382B', height: 1.04, female: false },
      xia: { shirt: '#64849A', trousers: '#676D74', skin: '#DCB491', hat: '#D8C49C', band: '#586D7E', hair: '#46372E', height: 0.96, female: true },
      qiao: { shirt: '#A08361', trousers: '#687256', skin: '#D0A780', hat: '#ACAB87', band: '#776646', hair: '#77776C', height: 0.99, female: false },
      chen: { shirt: '#A5757D', trousers: '#CABD9D', skin: '#C99E76', hat: '#899B95', band: '#885F6A', hair: '#39372E', height: 1.01, female: true },
    };
    const style = styles[id] || styles.lin;
    const shirt = mat(style.shirt), pants = mat(style.trousers), skin = mat(style.skin), hat = mat(style.hat), band = mat(style.band), hair = mat(style.hair);
    const dark = mat('#373A2F'), shoes = mat('#665F4D'), sole = mat('#4E4E42'), lip = mat('#A87D66');
    const group = new THREE.Group(); group.scale.setScalar(style.height);
    const body = new THREE.Group(); body.position.y = 0.84; group.add(body);
    const torso = mesh(shared.torso, shirt, [0, 0, 0], [style.female ? 0.94 : 1.03, 1, 0.65], body);
    ball(pants, 0, 0.017, 0, 0.175, 0.11, 0.112, body);
    const belt = cylinder(band, 0, 0.1, 0, 0.176, 0.031, body); belt.scale.z *= 0.64;
    cylinder(skin, 0, 0.69, 0, 0.046, 0.12, body);
    const head = mesh(shared.round, skin, [0, 0.847, -0.008], [0.106, 0.133, 0.104], body);
    // Adult facial proportions: small eyes, a modeled nose and eyebrows, not a doll face.
    [-1, 1].forEach(side => {
      ball(skin, side * 0.107, 0.845, 0, 0.019, 0.032, 0.014, body);
      ball(dark, side * 0.039, 0.865, -0.101, 0.01, 0.009, 0.005, body);
      const brow = box(hair, side * 0.039, 0.888, -0.101, 0.026, 0.006, 0.008, body); brow.rotation.z = side * 0.08;
    });
    ball(skin, 0, 0.841, -0.109, 0.019, 0.03, 0.024, body);
    box(lip, 0, 0.793, -0.101, 0.032, 0.004, 0.006, body);
    if (id === 'qiao') ball(hair, 0, 0.768, -0.072, 0.067, 0.021, 0.049, body);
    ball(hair, 0, 0.933, 0.01, 0.109, 0.078, 0.106, body);
    if (style.female) {
      ball(hair, 0, 0.82, 0.08, 0.105, 0.137, 0.048, body);
      ball(hair, 0.045, 0.758, 0.111, 0.047, 0.065, 0.044, body);
    }
    const brim = cylinder(hat, 0, 0.971, -0.005, id === 'chen' ? 0.146 : 0.196, 0.022, body);
    brim.scale.z *= id === 'chen' ? 1.21 : 1.12;
    cylinder(hat, 0, 1.02, 0, 0.12, 0.095, body);
    cylinder(band, 0, 0.984, 0, 0.123, 0.025, body);
    // Shirt placket, collar, pocket and rolled sleeves give each person field clothing.
    box(band, 0, 0.4, -0.127, 0.016, 0.32, 0.01, body);
    [-1, 1].forEach(side => { const collar = box(shirt, side * 0.048, 0.66, -0.1, 0.076, 0.03, 0.035, body); collar.rotation.z = side * 0.25; });
    box(band, -0.105, 0.48, -0.124, 0.058, 0.075, 0.008, body);
    for (let i = 0; i < 3; i++) ball(palette.cream, 0, 0.56 - i * 0.085, -0.136, 0.006, 0.006, 0.003, body);
    const arms = [], legs = [];
    for (let side of [-1, 1]) {
      ball(shirt, side * 0.173, 0.565, 0, 0.066, 0.073, 0.067, body);
      const arm = new THREE.Group(); arm.position.set(side * 0.19, 0.581, 0); body.add(arm);
      cylinder(shirt, 0, -0.139, 0, 0.062, 0.278, arm);
      cylinder(band, 0, -0.235, 0, 0.064, 0.038, arm);
      const elbow = new THREE.Group(); elbow.position.y = -0.272; arm.add(elbow);
      ball(skin, 0, 0, 0, 0.045, 0.049, 0.045, elbow);
      cylinder(skin, 0, -0.128, 0, 0.041, 0.256, elbow);
      const hand = ball(skin, 0, -0.282, 0, 0.043, 0.065, 0.025, elbow);
      arms.push({ arm, elbow, hand });
      const hip = new THREE.Group(); hip.position.set(side * 0.096, 0.867, 0); group.add(hip);
      ball(pants, 0, -0.02, 0, 0.08, 0.085, 0.078, hip);
      cylinder(pants, 0, -0.203, 0, 0.079, 0.407, hip);
      const knee = new THREE.Group(); knee.position.y = -0.409; hip.add(knee);
      ball(pants, 0, -0.008, 0, 0.069, 0.071, 0.067, knee);
      cylinder(pants, 0, -0.184, 0, 0.059, 0.369, knee);
      box(sole, 0, -0.408, -0.054, 0.12, 0.031, 0.235, knee);
      ball(shoes, 0, -0.376, -0.044, 0.066, 0.057, 0.117, knee);
      legs.push({ hip, knee });
    }
    const toolRoot = new THREE.Group(); toolRoot.position.set(0, -0.265, -0.035); arms[1].elbow.add(toolRoot);
    const shovel = new THREE.Group(); toolRoot.add(shovel);
    cylinder(palette.wood, 0, -0.26, 0, 0.018, 0.62, shovel);
    const blade = mesh(shared.round, palette.steel, [0, -0.604, 0], [0.09, 0.15, 0.022], shovel); blade.rotation.x = 0.12;
    const can = new THREE.Group(); toolRoot.add(can);
    cylinder(palette.metal, 0, -0.14, 0, 0.14, 0.24, can);
    branch(palette.steel, [0.08, -0.08, 0], [0.1, -0.08, -0.34], 0.032, can);
    const canHandle = mesh(shared.torus, palette.steel, [0, 0.015, 0], [0.115, 0.115, 0.03], can); canHandle.rotation.y = Math.PI / 2;
    const scissors = new THREE.Group(); toolRoot.add(scissors);
    [-1, 1].forEach(side => {
      const ring = mesh(shared.torus, band, [side * 0.031, 0, 0], [0.045, 0.065, 0.025], scissors);
      branch(palette.steel, [side * 0.025, -0.065, 0], [side * 0.019, -0.2, 0], 0.012, scissors);
    });
    const carriedSeedling = new THREE.Group(); toolRoot.add(carriedSeedling);
    cylinder(potMaterial, 0, -0.15, 0, 0.11, 0.18, carriedSeedling);
    cylinder(palette.leaf, 0, 0.03, 0, 0.009, 0.27, carriedSeedling);
    ball(palette.leafLight, -0.06, 0.08, 0, 0.09, 0.018, 0.05, carriedSeedling);
    ball(palette.leaf, 0.06, 0.15, 0, 0.09, 0.018, 0.05, carriedSeedling);
    const waterStream = new THREE.Group(); can.add(waterStream);
    waterStream.position.set(0.1, -0.08, -0.34);
    for (let i = 0; i < 6; i++) ball(palette.water, (i % 2 ? 1 : -1) * 0.006, -0.05 - i * 0.095, 0, 0.014, 0.048, 0.014, waterStream);
    batchGroup(body);
    shovel.visible = can.visible = scissors.visible = carriedSeedling.visible = false;
    const character = { id, group, body, arms, legs, shovel, can, scissors, carriedSeedling, waterStream, phase: 0 };
    roots.add(group); characters.set(id, character); return character;
  }

  function animateCharacter(character, dt, input) {
    const { group, body, arms, legs } = character;
    character.phase += dt * (input.moving ? 8.8 : 1.9);
    const action = input.action || {}, progress = Math.max(0, Math.min(1, Number(action.progress) || 0));
    const kind = String(action.type || '');
    const expected = PLANTING_STEPS[Number(currentState && currentState.nursery && currentState.nursery.step) || 0];
    const fillingWater = /fillWater/i.test(kind) || /water/i.test(kind) && expected && expected.targetId === 'water';
    const digging = /dig|plant|hole|support|tie|name|drain/.test(kind) || /water/i.test(kind) && !fillingWater;
    const harvesting = /harvest|cut|pick/.test(kind);
    const working = !!kind;
    const envelope = working ? Math.sin(Math.PI * progress) : 0;
    const gait = input.moving && !working ? 1 : 0, swing = Math.sin(character.phase);
    group.rotation.y = input.preview ? Math.PI - 0.13 : input.yaw || 0;
    body.position.y = 0.84 - (digging ? 0.31 : 0.04) * envelope + Math.abs(swing) * 0.019 * gait;
    body.rotation.x = digging ? -0.44 * envelope : harvesting ? -0.1 * envelope : -0.04 * gait;
    body.rotation.z = Math.sin(character.phase * 0.5) * 0.014 * gait;
    legs.forEach(({ hip, knee }, i) => {
      hip.position.y = 0.867 - (digging ? 0.31 : 0.04) * envelope;
      hip.rotation.x = (i ? -swing : swing) * 0.38 * gait + (digging ? 1.08 : 0.1) * envelope;
      knee.rotation.x = Math.max(0, (i ? swing : -swing)) * 0.55 * gait - (digging ? 1.45 : 0.1) * envelope;
    });
    arms.forEach(({ arm, elbow }, i) => {
      const reach = harvesting ? 1.75 : /dig|hole|drain/.test(kind) ? 0.42 : 0.85;
      arm.rotation.x = (i ? swing : -swing) * 0.35 * gait + (working ? reach * envelope : 0);
      arm.rotation.z = (i ? -1 : 1) * (0.055 + envelope * 0.04);
      elbow.rotation.x = 0.1 + (working ? /dig|hole|drain/.test(kind) ? 0.1 : 0.53 : 0.07) * envelope;
    });
    if (working) arms[1].arm.rotation.x += Math.sin(progress * Math.PI * 6) * 0.19 * envelope;
    character.shovel.visible = /dig|hole|drain/.test(kind);
    const carryingSeedling = currentState && currentState.chapter === 'planting' && currentState.nursery.step >= 1 && currentState.nursery.step < 3;
    const carryingWater = currentState && currentState.chapter === 'planting' && currentState.nursery.hasWater && !currentState.nursery.watered;
    character.can.visible = !!(/water/i.test(kind) || carryingWater); character.can.rotation.x = fillingWater ? 0 : -0.3 * envelope;
    character.scissors.visible = harvesting || /prune/.test(kind);
    character.carriedSeedling.visible = carryingSeedling || /seedling|plant|takeSeedling/.test(kind) && !/dig|hole/.test(kind);
    if (character.carriedSeedling.visible && !working) { arms[1].arm.rotation.x = 0.4; arms[1].elbow.rotation.x = 0.7; }
    if (carryingWater && !working) { arms[1].arm.rotation.x = 0.12; arms[1].elbow.rotation.x = 0.1; }
    character.waterStream.visible = /water/i.test(kind) && !fillingWater && envelope > 0.1;
    if (input.preview) character.shovel.visible = character.can.visible = character.scissors.visible = character.carriedSeedling.visible = false;
    // The emitter stays at the spout while its drops follow gravity rather
    // than inheriting the farmer's wrist, elbow and tilted watering can.
    character.can.getWorldQuaternion(waterOrientation);
    character.waterStream.quaternion.copy(waterOrientation.invert());
    character.waterStream.children.forEach((drop, i) => { drop.position.y = -0.05 - ((elapsed * 0.95 + i * 0.1) % 0.61); });
  }

  function rowState(state, id) {
    const rows = state && (state.rainChallenge && state.rainChallenge.plots || state.rows || state.vines || state.plots);
    if (Array.isArray(rows)) return rows.find(row => row.id === id) || {};
    return rows && rows[id] || {};
  }
  function update(dt, input) {
    if (disposed) return;
    input = input || {}; dt = Math.max(0, Math.min(0.1, Number(dt) || 0)); elapsed += dt;
    currentState = input.state || currentState;
    const state = currentState || {}, id = input.characterId || state.characterId || 'lin';
    if (!currentCharacter || currentCharacter.id !== id) {
      if (currentCharacter) currentCharacter.group.visible = false;
      currentCharacter = characters.get(id) || makeCharacter(id); currentCharacter.group.visible = true;
      debug.characterId = id; debug.characterChanges++;
    }
    const choosing = !!input.preview || state.stage === 'character';
    const position = choosing ? { x: 0, z: 2 } : input.position || { x: 0, z: 7 };
    currentCharacter.group.position.set(Number(position.x) || 0, 0, Number(position.z) || 0);
    animateCharacter(currentCharacter, dt, { ...input, preview: choosing });
    const step = Number(state.nursery && state.nursery.step) || 0;
    const mature = state.chapter === 'harvest';
    const nurseryState = state.nursery || {};
    youngGarden.visible = true; youngGarden.position.set(mature ? -5.3 : -2, 0, mature ? 0.8 : 0);
    matureGarden.visible = true;
    const plantingProgress = input.action && input.action.type === 'plant' && input.action.progress > 0.72;
    hole.visible = rim.visible = !!(!nurseryState.planted && (step >= 2 || input.action && input.action.type === 'dig' && input.action.progress > 0.25));
    sprout.visible = !!nurseryState.planted || !!plantingProgress;
    support.visible = ties.visible = !!nurseryState.supported;
    wetSoil.visible = !!nurseryState.watered;
    namePost.visible = !!(state.completed && state.completed.planting);
    if (state.completed && state.completed.planting && (nurseryState.name || '我的葡萄') !== currentName) {
      currentName = state.nursery && state.nursery.name || '我的葡萄';
      if (nameSign) { youngGarden.remove(nameSign); nameSign.material.map.dispose(); nameSign.material.dispose(); }
      nameSign = sign(currentName, 0.7, 0.88, 0.58, 0.84, youngGarden);
    }
    if (nameSign) nameSign.visible = !!(state.completed && state.completed.planting);
    for (const rowId of Object.keys(rows)) {
      const row = rows[rowId], data = rowState(state, rowId);
      row.harvested = !!(data.harvested || data.picked || (state.harvested && state.harvested.includes && state.harvested.includes(rowId)));
      row.damage = Number(data.damage || data.loss || 0);
      row.grapes.visible = mature && !row.harvested;
      row.grapes.material = row.damage > 20 ? palette.damagedGrape : palette.ripeGrape;
      row.foliage.rotation.z = Math.sin(elapsed * 1.6 + row.group.position.x) * 0.008;
      row.covers.visible = !!(data.shelter || data.covered || state.facilities && state.facilities.shelter || state.rainChallenge && state.rainChallenge.protected);
    }
    const challenge = state.rainChallenge || {};
    packedGrapes.visible = !!(state.inventory && state.inventory.length);
    const raining = mature && Number(challenge.clock) >= Number(challenge.rainAt || 6);
    rainLevel += ((raining ? 1 : 0) - rainLevel) * Math.min(1, dt * 2);
    rain.visible = raining; ditchWater.visible = raining;
    blockedDrain.visible = !(state.drainCleared || challenge.drainCleared || challenge.drained || challenge.drainage);
    if (raining) {
      raindrops.forEach((drop, i) => {
        dummy.position.set(drop.x, (drop.y - elapsed * 5.8 % 7 + 7) % 7, drop.z);
        dummy.rotation.set(0, 0, -0.13); dummy.scale.set(1, 1, 1); dummy.updateMatrix(); rain.setMatrixAt(i, dummy.matrix);
      }); rain.instanceMatrix.needsUpdate = true;
    }
    sun.intensity = 2.55 - rainLevel * 1.35;
    scene.background.set('#D6E3DB').lerp(new THREE.Color('#AEBEC0'), rainLevel * 0.75);
    scene.fog.color.copy(scene.background);
    clouds.position.x = Math.sin(elapsed * 0.02) * 0.8;
    const activeTargets = getTargets(state), visible = new Set(activeTargets.map(target => target.id));
    for (const markerId of Object.keys(markers)) {
      const marker = markers[markerId]; marker.group.visible = visible.has(markerId);
      if (marker.group.visible) {
        const target = targetDefinitions[markerId], distance = Math.hypot(target.x - position.x, target.z - position.z);
        const pulse = distance <= target.radius + 0.4 ? 1 + Math.sin(elapsed * 3) * 0.08 : 1;
        marker.ring.scale.set(0.52 * pulse, 0.52 * pulse, 0.25);
      }
    }
    const blend = Math.min(1, dt * 4.5);
    // Camera follows from the garden gate side; every direction is walkable
    // without rotating the player's screen or turning the controls around.
    const cameraYaw = Number(input.cameraYaw) || 0;
    const portraitPreview = choosing && camera.aspect < 0.78;
    const targetPosition = choosing ? new THREE.Vector3(position.x + (portraitPreview ? 0.25 : 0.55), 2.19, position.z + (portraitPreview ? 6.8 : 4.3))
      : new THREE.Vector3(position.x + Math.sin(cameraYaw) * 6.2, 3.55, position.z + Math.cos(cameraYaw) * 6.2);
    camera.position.lerp(targetPosition, blend || 1);
    // Foreground entrance beams must not block the following camera when it
    // backs beyond the entrance. It remains visible when viewed from inside.
    frontBoundary.visible = camera.position.z < 8.3;
    if (choosing) camera.lookAt(position.x, portraitPreview ? -0.38 : 1.02, position.z - 0.15);
    else camera.lookAt(position.x - Math.sin(cameraYaw) * 1.55, 0.95, position.z - Math.cos(cameraYaw) * 1.55);
    debug.frames++; debug.action = input.action ? input.action.type : null; debug.moving = !!input.moving;
  }
  function resize(w, h) { camera.aspect = Math.max(1, Number(w) || 390) / Math.max(1, Number(h) || 844); camera.updateProjectionMatrix(); }
  function batchGroup(parent, excluded) {
    const batches = new Map();
    parent.children.slice().forEach(node => {
      if (!node.isMesh || node.isInstancedMesh || excluded && excluded.has(node)) return;
      const key = node.geometry.uuid + ':' + node.material.uuid + ':' + node.castShadow + ':' + node.receiveShadow;
      if (!batches.has(key)) batches.set(key, []);
      batches.get(key).push(node);
    });
    for (const nodes of batches.values()) {
      if (nodes.length < 2) continue;
      const instance = new THREE.InstancedMesh(nodes[0].geometry, nodes[0].material, nodes.length);
      instance.castShadow = nodes[0].castShadow; instance.receiveShadow = nodes[0].receiveShadow;
      nodes.forEach((node, i) => { node.updateMatrix(); instance.setMatrixAt(i, node.matrix); parent.remove(node); });
      instance.instanceMatrix.needsUpdate = true; parent.add(instance);
    }
  }
  function dispose() {
    if (disposed) return; disposed = true;
    scene.traverse(node => {
      if (node.isInstancedMesh && typeof node.dispose === 'function') node.dispose();
      if (node.shadow) {
        if (node.shadow.map) node.shadow.map.dispose();
        if (node.shadow.mapPass) node.shadow.mapPass.dispose();
      }
    });
    geometries.forEach(geometry => geometry.dispose()); textures.forEach(texture => texture.dispose());
    materials.forEach(material => material.dispose()); scene.clear(); characters.clear();
  }
  batchGroup(roots, new Set([terrain, fieldEdge, ditchWater]));
  [clouds, shed, nursery, packing, crates, clockStand, frontBoundary].forEach(parent => batchGroup(parent));
  Object.values(rows).forEach(row => { batchGroup(row.group); batchGroup(row.covers); });
  update(0, { characterId: 'lin', position: { x: 0, z: 7 }, state: { stage: 'character', chapter: 'planting', nursery: { step: 0 } } });
  return {
    scene, camera, resize, update, getTargets, getObstacles: () => obstacles.map(value => ({ ...value })), dispose,
    getDebug: () => ({ ...debug, camera: camera.position.toArray(), position: currentCharacter && currentCharacter.group.position.toArray(),
      characterCount: characters.size, geometries: geometries.length, materials: materials.length,
      planted: sprout.visible, supports: support.visible, wetSoil: wetSoil.visible,
      rows: Object.fromEntries(Object.entries(rows).map(([id, row]) => [id, { harvested: row.harvested, damage: row.damage, sheltered: row.covers.visible }])) }),
  };
}

module.exports = { createWorld };
