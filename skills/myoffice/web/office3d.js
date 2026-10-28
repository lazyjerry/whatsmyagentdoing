// High-quality office: same floor plan and behaviour as the 2D office
// (office.js), rendered in 3D with Kenney's CC0 models (assets/kenney).
// Bubbles and name tags are drawn by the 2D code on a canvas overlay.
// Source file: `npm run build` bundles it with three.js into public/office3d.js.
import * as THREE from 'three';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { MeshoptDecoder } from 'three/addons/libs/meshopt_decoder.module.js';
import { clone as cloneSkinned } from 'three/addons/utils/SkeletonUtils.js';

const CHARACTERS = ['male-a', 'male-b', 'male-c', 'male-d', 'male-e', 'male-f',
  'female-a', 'female-b', 'female-c', 'female-d', 'female-e', 'female-f'];
// The model group says where the session runs (2D uses hair colour for the
// same thing); other platforms draw from the whole set.
const PLATFORM_MODELS = {
  vscode: CHARACTERS.filter((c) => c.startsWith('female')),
  cli: CHARACTERS.filter((c) => c.startsWith('male')),
};
const FURNITURE = ['desk', 'chairDesk', 'computerScreen', 'computerKeyboard', 'computerMouse', 'laptop',
  'pottedPlant', 'plantSmall1', 'plantSmall2', 'plantSmall3', 'kitchenCabinet', 'kitchenCoffeeMachine',
  'kitchenSink', 'kitchenFridgeLarge', 'kitchenMicrowave', 'stoolBar', 'tableRound', 'table',
  'chairModernCushion', 'bookcaseOpen', 'books', 'loungeSofa', 'loungeChairRelax', 'tableCoffee', 'rugRectangle',
  'lampRoundFloor', 'lampSquareTable', 'televisionModern', 'cabinetTelevision', 'trashcan',
  'coatRackStanding', 'radio', 'speaker', 'toaster', 'cardboardBoxClosed'];

const STATUS_COLOR = {
  thinking: 0x8b6be6, tool: 0x3b8ee0, typing: 0x22a874, waiting: 0xe09412, error: 0xde4b40,
};
const CHAR_HEIGHT = 0.92; // world units (≈ tiles)
const WALL_H = 2.4;
const SIT_FORWARD = 0.16;

function skyNow() {
  const h = new Date().getHours() + new Date().getMinutes() / 60;
  if (h >= 7 && h < 17) return { sky: 0xa9d4f2, sun: 1.7, amb: 1.15, night: false };
  if (h >= 5 && h < 7) return { sky: 0xf3c9a8, sun: 1.1, amb: 0.8, night: false };
  if (h >= 17 && h < 19) return { sky: 0xf0a679, sun: 1.0, amb: 0.75, night: false };
  return { sky: 0x1d2747, sun: 0.9, amb: 1.05, night: true };
}

export class Office3D extends window.Office {
  constructor(root, opts) {
    super(root, opts);
    this.compact = !!opts?.compact;
    this.canvas.classList.add('office-overlay');
    try {
      this.gl = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    } catch (e) {
      // No WebGL here; the page falls back to the 2D office.
      this.loadError = 'WebGL unavailable';
      this.markReady(false);
      return;
    }
    this.gl.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
    this.gl.shadowMap.enabled = true;
    this.gl.shadowMap.type = THREE.PCFSoftShadowMap;
    this.gl.outputColorSpace = THREE.SRGBColorSpace;
    this.gl.toneMapping = THREE.ACESFilmicToneMapping;
    this.gl.toneMappingExposure = 1.15;
    this.gl.domElement.className = 'office-gl';
    root.insertBefore(this.gl.domElement, this.canvas);

    this.scene = new THREE.Scene();
    this.camera = new THREE.OrthographicCamera(-10, 10, 10, -10, 0.1, 200);
    this.controls = new OrbitControls(this.camera, this.canvas);
    this.controls.enableDamping = true;
    this.controls.minPolarAngle = 0.35;
    this.controls.maxPolarAngle = 1.25;
    this.controls.minZoom = 0.6;
    this.controls.maxZoom = 5;
    this.controls.screenSpacePanning = true;
    this.controls.zoomToCursor = true;
    // Arrow keys pan the view while held, like a right-button drag. Keys
    // typed into a form control stay with it.
    this.keys = new Set();
    this.onKey = (e) => {
      if (!/^Arrow(Up|Down|Left|Right)$/.test(e.key)) return;
      if (e.type === 'keyup') { this.keys.delete(e.key); return; }
      if (!this.visible || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
      if (e.target.closest?.('input, select, textarea, [contenteditable], details[open]')) return;
      this.keys.add(e.key);
      e.preventDefault();
    };
    this.onBlur = () => this.keys.clear();
    window.addEventListener('keydown', this.onKey);
    window.addEventListener('keyup', this.onKey);
    window.addEventListener('blur', this.onBlur);
    // Two-finger scrolling on a trackpad pans; only a pinch zooms. Chrome
    // and Firefox send a pinch as a wheel with ctrlKey, which goes on to
    // OrbitControls; any other wheel is caught here before it gets there.
    // A mouse wheel therefore pans too, and zooms with Ctrl held.
    this.onWheel = (e) => {
      if (e.ctrlKey || !this.loaded) return;
      e.preventDefault();
      e.stopPropagation();
      const px = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? this.ch : 1;
      const k = (this.camera.right - this.camera.left) / this.camera.zoom / this.cw;
      this.panBy(e.deltaX * px * k, -e.deltaY * px * k);
    };
    // Safari sends a trackpad pinch as gesture events instead.
    this.onGesture = (e) => {
      e.preventDefault();
      if (e.type === 'gesturestart') { this.pinchZoom = this.camera.zoom; return; }
      if (!this.pinchZoom) return;
      this.camera.zoom = THREE.MathUtils.clamp(this.pinchZoom * e.scale, this.controls.minZoom, this.controls.maxZoom);
      this.camera.updateProjectionMatrix();
    };
    root.addEventListener('wheel', this.onWheel, { capture: true, passive: false });
    for (const t of ['gesturestart', 'gesturechange']) root.addEventListener(t, this.onGesture);

    this.hemi = new THREE.HemisphereLight(0xfff8ee, 0x8a7a66, 1.0);
    this.scene.add(this.hemi);
    this.sun = new THREE.DirectionalLight(0xfff1dc, 1.6);
    this.sun.castShadow = true;
    this.sun.shadow.mapSize.set(2048, 2048);
    this.sun.shadow.bias = -0.0006;
    this.sun.shadow.normalBias = 0.02;
    this.scene.add(this.sun, this.sun.target);

    this.actors = new Map();     // person key -> { obj, mixer, actions, current, head, cup }
    this.screens = new Map();    // desk -> emissive screen mesh
    this.blinkers = [];
    this.loaded = false;
    this.clock = new THREE.Clock();
    this.load().catch((e) => {
      this.loadError = String(e && e.message || e);
      console.error('3D office failed to load:', e);
      this.markReady(false);
    });
    this.resize();
  }

  destroy() {
    super.destroy();
    window.removeEventListener('keydown', this.onKey);
    window.removeEventListener('keyup', this.onKey);
    window.removeEventListener('blur', this.onBlur);
    this.root.removeEventListener('wheel', this.onWheel, { capture: true });
    for (const t of ['gesturestart', 'gesturechange']) this.root.removeEventListener(t, this.onGesture);
    if (!this.gl) return;
    this.controls.dispose();
    this.gl.dispose();
    this.gl.domElement.remove();
  }

  // ------------------------------------------------------------ assets

  async load() {
    const loader = new GLTFLoader();
    loader.setMeshoptDecoder(MeshoptDecoder);
    const get = (url) => new Promise((ok, fail) => loader.load(url, ok, undefined, fail));
    const [chars, furn] = await Promise.all([
      Promise.all(CHARACTERS.map((c) => get(`/assets/kenney/characters/character-${c}.glb`))),
      Promise.all(FURNITURE.map((f) => get(`/assets/kenney/furniture/${f}.glb`).then((g) => [f, g.scene]))),
    ]);
    this.chars = chars;
    this.models = Object.fromEntries(furn);
    for (const m of Object.values(this.models)) {
      m.traverse((o) => { if (o.isMesh) { o.castShadow = true; o.receiveShadow = true; } });
    }
    this.loaded = true;
  }

  // Place a furniture model so its footprint fills (x, y, w, d) on the floor
  // plan, standing on height y0. rot turns it (0 = model's own front faces +z).
  // With `stretch` the model is scaled per axis to exactly w × h × d.
  place(name, x, y, w, d, { rot = 0, y0 = 0, h = null, stretch = false, group = this.room } = {}) {
    const src = this.models[name];
    if (!src) return null;
    const m = src.clone(true);
    m.rotation.y = rot;
    m.updateMatrixWorld(true);
    let box = new THREE.Box3().setFromObject(m);
    const size = box.getSize(new THREE.Vector3());
    if (stretch) {
      const turned = Math.abs(Math.sin(rot)) > 0.5;
      m.scale.set((turned ? d : w) / (turned ? size.z : size.x), h / size.y, (turned ? w : d) / (turned ? size.x : size.z));
    } else {
      m.scale.setScalar(h ? h / size.y : Math.min(w / size.x, d / size.z));
    }
    m.updateMatrixWorld(true);
    box = new THREE.Box3().setFromObject(m);
    const c = box.getCenter(new THREE.Vector3());
    m.position.set(x + w / 2 - c.x, y0 - box.min.y, y + d / 2 - c.z);
    group.add(m);
    return m;
  }

  boxMesh(x, y, z, w, d, h, color, opts = {}) {
    const mat = opts.mat || new THREE.MeshStandardMaterial({ color, roughness: opts.rough ?? 0.8, metalness: opts.metal ?? 0, ...(opts.emissive ? { emissive: opts.emissive, emissiveIntensity: opts.ei ?? 1 } : {}) });
    const m = new THREE.Mesh(new THREE.BoxGeometry(w, h, d), mat);
    m.position.set(x + w / 2, z + h / 2, y + d / 2);
    m.castShadow = opts.shadow !== false;
    m.receiveShadow = true;
    (opts.group || this.room).add(m);
    return m;
  }

  glassMat() {
    return this._glass || (this._glass = new THREE.MeshPhysicalMaterial({
      color: 0xcfe6f5, transparent: true, opacity: 0.22, roughness: 0.05, metalness: 0, depthWrite: false,
    }));
  }

  textPlane(text, bg, w, h) {
    const cv = document.createElement('canvas');
    cv.width = 256; cv.height = Math.round(256 * h / w);
    const c = cv.getContext('2d');
    c.fillStyle = bg; c.fillRect(0, 0, cv.width, cv.height);
    c.fillStyle = '#fff';
    c.font = `800 ${Math.round(cv.height * 0.22)}px ui-sans-serif, system-ui, sans-serif`;
    c.textAlign = 'center'; c.textBaseline = 'middle';
    c.fillText(text, cv.width / 2, cv.height / 2);
    const tex = new THREE.CanvasTexture(cv);
    tex.colorSpace = THREE.SRGBColorSpace;
    return new THREE.Mesh(new THREE.PlaneGeometry(w, h), new THREE.MeshStandardMaterial({ map: tex, roughness: 0.9 }));
  }

  // ------------------------------------------------------------ build room

  build() {
    const plan = this.plan;
    if (this.room) {
      this.scene.remove(this.room);
      // The floor plan regrows whenever the number of workers crosses a threshold; free the old room on the GPU.
      this.room.traverse((o) => {
        o.geometry?.dispose();
        for (const m of [].concat(o.material || [])) { m.map?.dispose(); m.emissiveMap?.dispose(); m.dispose(); }
      });
    }
    this.room = new THREE.Group();
    this.scene.add(this.room);
    this.screens.clear();
    this.blinkers = [];
    // Props from the old room must not be animated once the plan regrows.
    this.arcadeScreen = null;
    this.tvScreen = null;
    this.lampLight = null;
    const { W, D, OW, MR, PY } = plan;
    const dark = this.pal.dark;

    // floors
    const floor = (x, y, w, d, color, tex) => {
      const mat = new THREE.MeshStandardMaterial({ color, roughness: 0.9, map: tex || null });
      const m = new THREE.Mesh(new THREE.PlaneGeometry(w, d), mat);
      m.rotation.x = -Math.PI / 2;
      m.position.set(x + w / 2, 0, y + d / 2);
      m.receiveShadow = true;
      this.room.add(m);
    };
    const planks = this.patternTexture((c, s) => {
      c.fillStyle = dark ? '#4a3f35' : '#e3cfb0'; c.fillRect(0, 0, s, s);
      c.strokeStyle = dark ? 'rgba(0,0,0,.25)' : 'rgba(120,90,50,.18)'; c.lineWidth = 2;
      for (let i = 0; i <= 4; i++) { c.beginPath(); c.moveTo(0, i * s / 4); c.lineTo(s, i * s / 4); c.stroke(); }
      for (let i = 0; i < 4; i++) { const x = ((i * 37) % 4 + 0.5) * s / 4; c.beginPath(); c.moveTo(x, i * s / 4); c.lineTo(x, (i + 1) * s / 4); c.stroke(); }
    });
    const tiles = this.patternTexture((c, s) => {
      c.fillStyle = dark ? '#3d4044' : '#f4f4f1'; c.fillRect(0, 0, s, s);
      c.fillStyle = dark ? '#35383c' : '#e3e4e0'; c.fillRect(0, 0, s / 2, s / 2); c.fillRect(s / 2, s / 2, s / 2, s / 2);
    });
    planks.repeat.set(OW / 2, D / 2);
    floor(0, 0, OW, D, 0xffffff, planks);
    floor(OW, 0, W - OW, MR, dark ? 0x33404d : 0xc9d3dc);
    tiles.repeat.set((W - OW), (PY - MR));
    floor(OW, MR, W - OW, PY - MR, 0xffffff, tiles);
    const planks2 = planks.clone(); planks2.needsUpdate = true; planks2.repeat.set((W - OW) / 2, (D - PY) / 2);
    floor(OW, PY, W - OW, D - PY, 0xffffff, planks2);

    // walls: back (z=0) with windows, left (x=0) with a door
    const wallCol = dark ? 0x4a4640 : 0xf4efe6;
    const wallMat = new THREE.MeshStandardMaterial({ color: wallCol, roughness: 0.95 });
    const T = 0.12;
    let cx = -T;
    const sky = skyNow();
    this.windowMat = new THREE.MeshStandardMaterial({ color: sky.sky, emissive: sky.sky, emissiveIntensity: 0.6 });
    for (const wx of plan.windows) {
      this.boxMesh(cx, -T, 0, wx - cx, T, WALL_H, 0, { mat: wallMat });
      this.boxMesh(wx, -T, 0, 1.5, T, 0.85, 0, { mat: wallMat });
      this.boxMesh(wx, -T, 2.0, 1.5, T, WALL_H - 2.0, 0, { mat: wallMat });
      const glass = new THREE.Mesh(new THREE.PlaneGeometry(1.5, 1.15), this.windowMat);
      glass.position.set(wx + 0.75, 1.425, -T / 2);
      this.room.add(glass);
      this.boxMesh(wx + 0.73, -0.02, 0.85, 0.04, 0.04, 1.15, dark ? 0x2b2926 : 0xcfc5b3);
      this.boxMesh(wx, -0.02, 0.83, 1.5, 0.1, 0.04, dark ? 0x2b2926 : 0xcfc5b3);
      cx = wx + 1.5;
    }
    this.boxMesh(cx, -T, 0, W - cx, T, WALL_H, 0, { mat: wallMat });
    const dy = plan.door.y;
    this.boxMesh(-T, -T, 0, T, dy - 0.6 + T, WALL_H, 0, { mat: wallMat });
    this.boxMesh(-T, dy + 0.6, 0, T, D - dy - 0.6, WALL_H, 0, { mat: wallMat });
    this.boxMesh(-T, dy - 0.6, 1.8, T, 1.2, WALL_H - 1.8, 0, { mat: wallMat });
    this.boxMesh(-T + 0.02, dy - 0.55, 0, 0.06, 1.1, 1.78, dark ? 0x5c4431 : 0xa8825a);
    this.boxMesh(0, dy - 0.6, 0, 0.9, 1.2, 0.01, dark ? 0x5a3b2e : 0xb9654a, { shadow: false });
    // meeting room whiteboard
    const wb = this.textPlane('', '#fdfdfb', 2.6, 1.15);
    wb.position.set(OW + 3.7, 1.4, 0.005);
    this.room.add(wb);
    this.whiteboardScribbles(OW + 3.7);
    const sign = this.textPlane('MEETING', dark ? '#3a4a5a' : '#8ea3b8', 1.3, 0.3);
    sign.position.set(OW + 1.1, 2.15, 0.005);
    this.room.add(sign);

    // desks
    for (const dk of plan.desks) this.buildDesk(dk);

    for (const p of plan.props) this.buildProp(p);
    this.fitCamera3D(true);
  }

  patternTexture(paint) {
    const cv = document.createElement('canvas');
    cv.width = cv.height = 128;
    paint(cv.getContext('2d'), 128);
    const t = new THREE.CanvasTexture(cv);
    t.wrapS = t.wrapT = THREE.RepeatWrapping;
    t.colorSpace = THREE.SRGBColorSpace;
    return t;
  }

  whiteboardScribbles(cx) {
    const g = new THREE.Group();
    const line = (pts, color) => {
      const geo = new THREE.BufferGeometry().setFromPoints(pts.map(([x, y]) => new THREE.Vector3(x, y, 0)));
      g.add(new THREE.Line(geo, new THREE.LineBasicMaterial({ color })));
    };
    line([[-1.1, 0.35], [-0.6, 0.35]], 0x3b8ee0);
    line([[-1.1, 0.2], [-0.3, 0.2]], 0x3b8ee0);
    line([[0.0, 0.3], [0.4, 0.3], [0.4, 0.05], [0.0, 0.05], [0.0, 0.3]], 0xc96442);
    line([[0.4, 0.17], [0.65, 0.0]], 0xc96442);
    line([[0.65, 0.1], [1.05, 0.1], [1.05, -0.15], [0.65, -0.15], [0.65, 0.1]], 0xc96442);
    line([[-1.1, -0.35], [-0.9, -0.1], [-0.7, -0.3], [-0.5, -0.05], [-0.3, -0.25], [-0.1, 0.0]], 0x5f9a4c);
    g.position.set(cx, 1.4, 0.01);
    this.room.add(g);
  }

  buildDesk(dk) {
    const { x, y } = dk;
    this.place('desk', x, y, 1.4, 0.8, { h: 0.75, rot: dk.side === 's' ? 0 : Math.PI });
    const top = 0.75;
    const back = dk.side === 's' ? y + 0.12 : y + 0.48;
    const rot = dk.side === 's' ? 0 : Math.PI;
    const mon = this.place('computerScreen', x + 0.4, back, 0.6, 0.2, { y0: top, h: 0.5, rot });
    const kb = dk.side === 's' ? y + 0.45 : y + 0.2;
    this.place('computerKeyboard', x + 0.45, kb, 0.5, 0.15, { y0: top, rot });
    this.place('computerMouse', x + 1.0, kb, 0.08, 0.12, { y0: top, rot });
    const h = (Math.round(x * 10) * 31 + Math.round(y * 10) * 17) >>> 0;
    if (h % 3 === 0) this.place('plantSmall' + (1 + (h % 3)), x + 0.05, y + 0.3, 0.25, 0.25, { y0: top, h: 0.3 });
    if (h % 4 === 1) this.place('lampSquareTable', x + 1.1, back, 0.25, 0.25, { y0: top, h: 0.35 });
    // chair, facing the desk
    const seat = dk.seat;
    this.place('chairDesk', seat.x - 0.3, seat.y - 0.3, 0.6, 0.6, { h: 0.9, rot: dk.side === 's' ? Math.PI : 0 });
    // The monitor's own screen quad lights up in the worker's status colour.
    if (mon) {
      let quad = null;
      mon.traverse((o) => { if (o.isMesh && o.geometry.attributes.position.count === 4) quad = o; });
      if (quad) {
        quad.material = new THREE.MeshStandardMaterial({ color: 0x1b1e24, emissive: 0x000000, roughness: 0.35 });
        this.screens.set(dk, quad);
      }
    }
  }

  buildProp(p) {
    const dark = this.pal.dark;
    switch (p.t) {
      case 'panel':
        this.boxMesh(p.x, p.y, 0, p.w, p.d, p.h, new THREE.Color(p.color).getHex(), { rough: 1 });
        break;
      case 'rack': {
        this.boxMesh(p.x, p.y, 0, 0.7, 0.6, 1.9, 0x2a2d33, { metal: 0.3, rough: 0.5 });
        for (let r = 0; r < 7; r++) {
          for (let k = 0; k < 4; k++) {
            const led = this.boxMesh(p.x + 0.12 + k * 0.13, p.y + 0.6, 1.7 - r * 0.22, 0.05, 0.01, 0.03, 0x113322,
              { emissive: k % 3 === 0 ? 0xffb347 : 0x5cf08a, ei: 1, shadow: false });
            this.blinkers.push({ m: led, r, k });
          }
        }
        break;
      }
      case 'printer':
        this.boxMesh(p.x, p.y, 0, 0.75, 0.55, 0.55, dark ? 0x6e533b : 0xa8825a);
        this.boxMesh(p.x + 0.05, p.y + 0.05, 0.55, 0.65, 0.45, 0.28, 0xe8e8e8);
        this.boxMesh(p.x + 0.15, p.y + 0.35, 0.83, 0.45, 0.15, 0.02, 0xffffff);
        this.place('cardboardBoxClosed', p.x - 0.7, p.y + 0.05, 0.5, 0.5, { h: 0.35 });
        break;
      case 'plant':
        this.place('pottedPlant', p.x - 0.3, p.y - 0.3, 0.6, 0.6, { h: 1.0 });
        break;
      case 'booth': {
        const { x, y } = p;
        const felt = new THREE.MeshStandardMaterial({ color: dark ? 0x3c4a5a : 0x9fb3c8, roughness: 1 });
        this.boxMesh(x, y - 0.05, 0, 1.3, 0.05, 2.0, 0, { mat: felt });
        this.boxMesh(x - 0.05, y, 0, 0.05, 1.3, 2.0, 0, { mat: felt });
        this.boxMesh(x + 1.25, y, 0, 0.05, 1.3, 2.0, 0, { mat: this.glassMat(), shadow: false });
        this.boxMesh(x, y + 1.25, 0, 0.85, 0.05, 2.0, 0, { mat: this.glassMat(), shadow: false });
        this.boxMesh(x - 0.05, y - 0.05, 2.0, 1.4, 1.4, 0.06, dark ? 0x4a5868 : 0xb5c4d3);
        this.place('stoolBar', x + 0.45, y + 0.4, 0.4, 0.4, { h: 0.5 });
        const l = new THREE.PointLight(0xffd9a0, 0.6, 2.5);
        l.position.set(x + 0.65, 1.85, y + 0.65);
        this.room.add(l);
        break;
      }
      case 'glass':
        if (p.w === 0) this.boxMesh(p.x - 0.03, p.y, 0, 0.06, p.d, 2.1, 0, { mat: this.glassMat(), shadow: false });
        else this.boxMesh(p.x, p.y - 0.03, 0, p.w, 0.06, 2.1, 0, { mat: this.glassMat(), shadow: false });
        if (p.w === 0) this.boxMesh(p.x - 0.04, p.y, 0, 0.08, p.d, 0.12, dark ? 0x2b2926 : 0xcfc5b3);
        else this.boxMesh(p.x, p.y - 0.04, 0, p.w, 0.08, 0.12, dark ? 0x2b2926 : 0xcfc5b3);
        break;
      case 'meetingTable':
        this.place('table', p.x, p.y, p.w, p.d, { h: 0.75 });
        this.place('laptop', p.x + 1.2, p.y + 0.45, 0.45, 0.35, { y0: 0.75 });
        break;
      case 'chair':
        this.place('chairModernCushion', p.x - 0.28, p.y - 0.28, 0.56, 0.56, { h: 0.85, rot: p.face === 'front' ? 0 : Math.PI });
        break;
      case 'counter': {
        const n = Math.max(1, Math.round(p.w / 0.7));
        const u = p.w / n;
        for (let i = 0; i < n; i++) {
          const ux = p.x + i * u;
          const isSink = Math.abs(ux + u / 2 - (p.x + 3.15)) < u / 2;
          this.place(isSink ? 'kitchenSink' : 'kitchenCabinet', ux, p.y, u, p.d, { h: 0.9, stretch: true });
        }
        this.place('kitchenMicrowave', p.x + p.w - 0.75, p.y + 0.1, 0.55, 0.4, { y0: 0.9, h: 0.32 });
        this.place('toaster', p.x + 0.2, p.y + 0.2, 0.3, 0.2, { y0: 0.9, h: 0.18 });
        break;
      }
      case 'coffee':
        this.place('kitchenCoffeeMachine', p.x, p.y, 0.45, 0.4, { y0: 0.9, h: 0.5 });
        break;
      case 'fridge':
        this.place('kitchenFridgeLarge', p.x, p.y, 0.85, 0.75, { h: 1.95, stretch: true });
        break;
      case 'cooler': {
        this.boxMesh(p.x, p.y, 0, 0.42, 0.42, 0.95, 0xe8ecef, { rough: 0.4 });
        const bottle = new THREE.Mesh(new THREE.CylinderGeometry(0.15, 0.15, 0.42, 16),
          new THREE.MeshPhysicalMaterial({ color: 0x8ec5f2, transparent: true, opacity: 0.65, roughness: 0.1 }));
        bottle.position.set(p.x + 0.21, 0.95 + 0.21, p.y + 0.21);
        this.room.add(bottle);
        break;
      }
      case 'sink':
        break; // part of the counter
      case 'roundTable':
        this.place('tableRound', p.x - 0.45, p.y - 0.45, 0.9, 0.9, { h: 0.75 });
        break;
      case 'barStool':
        this.place('stoolBar', p.x - 0.2, p.y - 0.2, 0.4, 0.4, { h: 0.5 });
        break;
      case 'shelf': {
        const n = Math.max(1, Math.round(p.w / 0.9));
        const u = p.w / n;
        for (let i = 0; i < n; i++) {
          const b = this.place('bookcaseOpen', p.x + i * u, p.y, u, p.d, { h: 1.45, stretch: true });
          if (!b) continue;
          // book stacks on each shelf
          b.updateMatrixWorld(true);
          const ray = new THREE.Raycaster();
          for (let k = 0; k < 2; k++) {
            const bx = p.x + i * u + 0.2 + k * (u - 0.4) * 0.6;
            ray.set(new THREE.Vector3(bx + 0.1, 1.6, p.y + p.d / 2), new THREE.Vector3(0, -1, 0));
            for (const hit of ray.intersectObject(b, true)) {
              if (hit.face && hit.face.normal.y > 0.5 && hit.point.y < 1.4) {
                this.place('books', bx, p.y + 0.06, 0.25, p.d - 0.12, { y0: hit.point.y, h: 0.14 + ((i + k) % 3) * 0.03 });
              }
            }
          }
        }
        break;
      }
      case 'couch':
        if (p.rot) this.place('loungeSofa', p.x - 0.1, p.y - 0.15, p.w + 0.2, p.d + 0.35, { h: 0.8, stretch: true, rot: p.rot });
        else this.place('loungeSofa', p.x - 0.1, p.y - 0.15, p.w + 0.2, p.d + 0.2, { h: 0.8, stretch: true });
        break;
      case 'board': {
        // the sales board on the left wall, same drawing as the 2D office
        this.boxMesh(0, p.y - 0.06, p.z - 0.06, 0.04, p.w + 0.12, p.h + 0.12, 0xb9bec5, { metal: 0.4, rough: 0.4 });
        this.boxMesh(0, p.y + 0.4, p.z - 0.1, 0.1, p.w * 0.5, 0.03, 0x9aa0a6);
        const cv = document.createElement('canvas');
        cv.width = 1024; cv.height = 384;
        window.OFFICE_DRAW_BOARD(cv.getContext('2d'));
        const tex = new THREE.CanvasTexture(cv);
        tex.colorSpace = THREE.SRGBColorSpace;
        tex.anisotropy = 4;
        const m = new THREE.Mesh(new THREE.PlaneGeometry(p.w, p.h), new THREE.MeshStandardMaterial({ map: tex, roughness: 0.7 }));
        m.rotation.y = Math.PI / 2;
        m.position.set(0.045, p.z + p.h / 2, p.y + p.w / 2);
        this.room.add(m);
        break;
      }
      case 'tv':
        // wall-mounted TV over a low cabinet; the screen slowly changes colour
        this.place('cabinetTelevision', 0.05, p.y + 0.1, 0.45, p.w - 0.2, { h: 0.45, stretch: true, rot: Math.PI / 2 });
        this.place('speaker', 0.12, p.y + 0.15, 0.25, 0.25, { y0: 0.45, h: 0.3 });
        this.boxMesh(0, p.y, 0.95, 0.06, p.w, 0.9, 0x111214, { metal: 0.3, rough: 0.4 });
        this.tvScreen = this.boxMesh(0.06, p.y + 0.06, 1.0, 0.005, p.w - 0.12, 0.8, 0x000000, { emissive: 0x3b8ee0, ei: 0.7, shadow: false });
        break;
      case 'coffeeTable':
        this.place('tableCoffee', p.x, p.y, p.w, p.d, { h: 0.35 });
        this.place('radio', p.x + 1.0, p.y + 0.2, 0.3, 0.18, { y0: 0.35, h: 0.18 });
        break;
      case 'beanbag': {
        const m = new THREE.Mesh(new THREE.SphereGeometry(0.42, 20, 14), new THREE.MeshStandardMaterial({ color: p.color, roughness: 0.95 }));
        m.scale.set(1, 0.5, 1);
        m.position.set(p.x, 0.2, p.y);
        m.castShadow = m.receiveShadow = true;
        this.room.add(m);
        break;
      }
      case 'arcade': {
        const { x, y } = p;
        this.boxMesh(x, y, 0, 0.7, 0.7, 1.75, 0x3a2a6a, { rough: 0.6 });
        this.arcadeScreen = this.boxMesh(x + 0.1, y + 0.7, 1.0, 0.5, 0.01, 0.45, 0x000000, { emissive: 0xff44aa, ei: 1, shadow: false });
        this.boxMesh(x, y + 0.7, 1.55, 0.7, 0.02, 0.17, 0xffcf4a, { emissive: 0xffcf4a, ei: 0.6, shadow: false });
        this.boxMesh(x, y + 0.7, 0.8, 0.7, 0.2, 0.08, 0x4a3a8a);
        break;
      }
      case 'lamp':
        this.place('lampRoundFloor', p.x - 0.25, p.y - 0.25, 0.5, 0.5, { h: 1.7 });
        this.lampLight = new THREE.PointLight(0xffd9a0, 0, 4);
        this.lampLight.position.set(p.x, 1.6, p.y);
        this.room.add(this.lampLight);
        break;
      case 'rug':
        this.place('rugRectangle', p.x, p.y, p.w, p.d, { y0: 0.004 });
        break;
    }
  }

  // ------------------------------------------------------------ camera

  fitCamera3D(reset) {
    const { W, D } = this.plan;
    const center = new THREE.Vector3(W / 2, 0.6, D / 2);
    const fresh = reset || !this.fitted;
    if (fresh) {
      const dist = 40;
      this.camera.position.set(center.x + dist * 0.62, center.y + dist * 0.58, center.z + dist * 0.62);
      this.controls.target.copy(center);
      this.camera.zoom = this.compact ? 1 : 1.25;
      this.fitted = true;
    }
    this.camera.lookAt(this.controls.target);
    this.camera.updateMatrixWorld(true);
    // Fit the room's corners into the view.
    const inv = this.camera.matrixWorldInverse;
    let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
    for (const x of [0, W]) for (const y of [0, this.plan.viewH || WALL_H]) for (const z of [0, D]) {
      const v = new THREE.Vector3(x, y, z).applyMatrix4(inv);
      minX = Math.min(minX, v.x); maxX = Math.max(maxX, v.x); minY = Math.min(minY, v.y); maxY = Math.max(maxY, v.y);
    }
    const aspect = this.cw / this.ch;
    let w = (maxX - minX) * 1.06, h = (maxY - minY) * 1.1;
    // Leave room for screen-space badges and bubbles in a small PiP window.
    if (this.compact) h *= this.ch / Math.max(80, this.ch - 48);
    if (w / h > aspect) h = w / aspect; else w = h * aspect;
    // A fresh fit slides the camera so the room sits on the view axis;
    // later refits (a resize) only change the size and keep any pan.
    if (fresh) {
      const cxv = (minX + maxX) / 2, cyv = (minY + maxY) / 2, m = this.camera.matrixWorld;
      const shift = new THREE.Vector3().setFromMatrixColumn(m, 0).multiplyScalar(cxv)
        .add(new THREE.Vector3().setFromMatrixColumn(m, 1).multiplyScalar(cyv));
      this.camera.position.add(shift);
      this.controls.target.add(shift);
    }
    Object.assign(this.camera, { left: -w / 2, right: w / 2, top: h / 2, bottom: -h / 2 });
    this.camera.updateProjectionMatrix();
    // Sun casts shadows over the whole room.
    this.sun.position.set(center.x - 6, 14, center.z - 3);
    this.sun.target.position.copy(center);
    const sc = this.sun.shadow.camera;
    const r = Math.max(W, D) * 0.75;
    Object.assign(sc, { left: -r, right: r, top: r, bottom: -r, near: 1, far: 50 });
    sc.updateProjectionMatrix();
  }

  // Arrow keys: half the visible width per second.
  panByKeys(dt) {
    const k = this.keys;
    const dx = (k.has('ArrowRight') ? 1 : 0) - (k.has('ArrowLeft') ? 1 : 0);
    const dy = (k.has('ArrowUp') ? 1 : 0) - (k.has('ArrowDown') ? 1 : 0);
    if (!dx && !dy) return;
    const step = ((this.camera.right - this.camera.left) / this.camera.zoom) * 0.5 * dt;
    this.panBy(dx * step, dy * step);
  }

  // Slide the camera and its target along the screen axes by world units.
  panBy(dx, dy) {
    const c = this.camera;
    const move = new THREE.Vector3().setFromMatrixColumn(c.matrix, 0).multiplyScalar(dx)
      .add(new THREE.Vector3().setFromMatrixColumn(c.matrix, 1).multiplyScalar(dy));
    c.position.add(move);
    this.controls.target.add(move);
  }

  resize() {
    super.resize();
    if (!this.gl) return;
    this.gl.setSize(this.cw, this.ch, false);
    this.gl.domElement.style.width = this.cw + 'px';
    this.gl.domElement.style.height = this.ch + 'px';
    if (this.room) this.fitCamera3D(false);
  }

  // ------------------------------------------------------------ actors

  modelName(p) {
    const pool = PLATFORM_MODELS[p.s.platform] || CHARACTERS;
    return pool[p.h % pool.length];
  }

  actorFor(p) {
    let a = this.actors.get(p.key);
    if (a) return a;
    const gltf = this.chars[CHARACTERS.indexOf(this.modelName(p))];
    const obj = cloneSkinned(gltf.scene);
    obj.traverse((o) => { if (o.isMesh) { o.castShadow = true; o.receiveShadow = true; } });
    const box = new THREE.Box3().setFromObject(obj);
    const s = CHAR_HEIGHT * (p.s && p.s.subagent ? 0.8 : 1) / (box.max.y - box.min.y);
    obj.scale.setScalar(s);
    const mixer = new THREE.AnimationMixer(obj);
    const actions = {};
    for (const clip of gltf.animations) actions[clip.name] = mixer.clipAction(clip);
    let head = null, armR = null;
    obj.traverse((o) => { if (o.name === 'head') head = o; if (o.name === 'arm-right') armR = o; });
    const prop = new THREE.Group();
    const cup = new THREE.Mesh(new THREE.CylinderGeometry(0.05, 0.04, 0.1, 10), new THREE.MeshStandardMaterial({ color: 0xffffff }));
    const book = new THREE.Mesh(new THREE.BoxGeometry(0.22, 0.28, 0.04), new THREE.MeshStandardMaterial({ color: 0xc96442 }));
    const phone = new THREE.Mesh(new THREE.BoxGeometry(0.07, 0.12, 0.012), new THREE.MeshStandardMaterial({ color: 0x111111, emissive: 0x8fd3ff, emissiveIntensity: 0.6 }));
    prop.add(cup, book, phone);
    this.scene.add(obj, prop);
    a = { obj, mixer, actions, current: null, head, headRest: head ? head.rotation.x : 0, armR, prop, cup, book, phone, yaw: 0, last: { x: p.x, y: p.y } };
    this.actors.set(p.key, a);
    return a;
  }

  // Clones share the model's geometry and materials; only the bone textures and the props are their own.
  dropActor(a) {
    this.scene.remove(a.obj, a.prop);
    a.mixer.stopAllAction();
    a.obj.traverse((o) => { if (o.isSkinnedMesh) o.skeleton.dispose(); });
    for (const m of [a.cup, a.book, a.phone]) { m.geometry.dispose(); m.material.dispose(); }
  }

  play(a, name) {
    if (a.current === name) return;
    const next = a.actions[name] || a.actions.idle;
    if (!next) return;
    next.reset().setEffectiveWeight(1).fadeIn(0.25).play();
    if (a.current && a.actions[a.current]) a.actions[a.current].fadeOut(0.25);
    a.current = name;
  }

  facing(p, st) {
    if (st.pose === 'walk') return null;
    if (p.mode === 'work' && p.desk) {
      if (p.desk.yaw != null) return p.desk.yaw;
      return p.desk.side === 'n' ? 0 : Math.PI;
    }
    const q = p.mode === 'booth' || p.mode === 'meet' ? p.spot : p.poi;
    if (!q) return null;
    if (q.yaw != null) return q.yaw;
    return st.face === 'back' ? Math.PI : 0;
  }

  syncActors(dt) {
    for (const [k, a] of this.actors) {
      if (!this.people.has(k)) { this.dropActor(a); this.actors.delete(k); }
    }
    for (const p of this.people.values()) {
      const a = this.actorFor(p);
      const st = this.stateOf(p);
      p.pose = st.pose;
      // Arguing switches between shaking the head and jabbing a hand.
      const argue = Math.sin(this.t / 1000 * 1.3 + p.phase) > 0 ? 'emote-no' : 'interact-right';
      const anim = {
        walk: 'walk', wave: 'emote-yes', coffee: 'holding-right', water: 'holding-right', read: 'holding-both',
        phone: 'holding-right', arcade: 'interact-right', fridge: 'interact-left', sink: 'interact-right',
        argue,
      }[st.pose === 'walk' ? 'walk' : st.act] || (st.pose === 'sit' ? 'sit' : 'idle');
      this.play(a, anim);
      a.mixer.update(dt);

      // position + facing
      const dx = p.x - a.last.x, dy = p.y - a.last.y;
      let want = this.facing(p, st);
      if (want == null && Math.hypot(dx, dy) > 0.001) want = Math.atan2(dx, dy);
      if (want != null) {
        let d = want - a.yaw;
        d = Math.atan2(Math.sin(d), Math.cos(d));
        a.yaw += d * Math.min(1, dt * 10);
      }
      a.last = { x: p.x, y: p.y };
      a.obj.rotation.y = a.yaw;
      const seatY = st.pose === 'sit' ? this.seatHeight(p.x, p.y, st.sitZ) : 0;
      // The sit pose's torso leans back past its origin; slide the body
      // forward so the back rests against the backrest instead of in it.
      const fwd = st.pose === 'sit' ? SIT_FORWARD : 0;
      a.obj.position.set(p.x + Math.sin(a.yaw) * fwd, seatY, p.y + Math.cos(a.yaw) * fwd);
      if (a.head) {
        // looking down at the phone
        if (st.act === 'scroll') { a.head.rotation.x = a.headRest + 0.35; a.bowed = true; }
        else if (a.bowed) { a.head.rotation.x = a.headRest; a.bowed = false; }
      }
      // hand-held props
      a.cup.visible = st.act === 'coffee' || st.act === 'water' || (st.pose !== 'walk' && p.poi && p.poi.prop === 'cup');
      a.book.visible = st.act === 'read';
      a.phone.visible = st.act === 'scroll';
      if (a.armR && (a.cup.visible || a.book.visible || a.phone.visible)) {
        a.obj.updateMatrixWorld(true);
        const hand = new THREE.Vector3(0, -0.4, 0.15);
        a.armR.localToWorld(hand);
        a.prop.position.copy(hand);
        a.prop.rotation.y = a.yaw;
        a.cup.material.color.set(st.act === 'water' ? 0x9fd3ff : 0xffffff);
      }
    }
  }

  // Highest surface below `limit` under (x, z). Sampled at the point and
  // four spots around it so a gap in a seat doesn't read as the floor.
  // World matrices must be current.
  surfaceBelow(x, z, limit, from = 1.2) {
    if (!this.room) return 0;
    const ray = new THREE.Raycaster();
    const down = new THREE.Vector3(0, -1, 0);
    let best = 0;
    for (const [dx, dz] of [[0, 0], [0.09, 0], [-0.09, 0], [0, 0.09], [0, -0.09]]) {
      ray.set(new THREE.Vector3(x + dx, from, z + dz), down);
      for (const h of ray.intersectObject(this.room, true)) {
        if (h.point.y < limit && h.point.y > best) best = h.point.y;
      }
    }
    return best;
  }

  // Kenney's `sit` clip is a sit-on-the-floor pose (seat at the model's
  // origin), so lift the character to whatever surface is under the seat
  // point: chair cushion, stool, sofa or beanbag. Measured once per spot.
  seatHeight(x, y, fallback) {
    const key = `${x.toFixed(2)},${y.toFixed(2)}`;
    this.seats = this.seats || new Map();
    if (this.seats.has(key)) return this.seats.get(key);
    const best = this.surfaceBelow(x, y, 0.75);
    const v = best > 0.05 ? best : fallback;
    this.seats.set(key, v);
    return v;
  }

  // ------------------------------------------------------------ frame

  draw() {
    const c = this.ctx;
    c.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
    c.clearRect(0, 0, this.cw, this.ch);
    if (!this.loaded) {
      c.fillStyle = this.pal.text;
      c.globalAlpha = 0.7;
      c.font = '600 14px ui-sans-serif, system-ui, sans-serif';
      c.textAlign = 'center';
      c.fillText(this.loadError ? `3D failed: ${this.loadError}` : '', this.cw / 2, this.ch / 2);
      c.globalAlpha = 1;
      return false;
    }
    if (this.room !== undefined && this.builtPlan === this.plan) {
      // fine
    } else {
      this.build();
      this.builtPlan = this.plan;
      this.room.updateMatrixWorld(true);
      this.seats = new Map();
      for (const a of this.actors.values()) this.dropActor(a);
      this.actors.clear();
    }
    const dt = Math.min(0.05, this.clock.getDelta());
    const t = this.t / 1000;

    // lighting follows the time of day
    const sky = skyNow();
    this.sun.intensity = sky.sun;
    this.hemi.intensity = sky.amb;
    if (this.windowMat) {
      this.windowMat.color.setHex(sky.sky);
      this.windowMat.emissive.setHex(sky.sky);
    }
    if (this.lampLight) this.lampLight.intensity = sky.night ? 2.2 : 0;

    // screens show who is working and on what
    const occupant = new Map();
    for (const p of this.people.values()) if (p.mode === 'work' && p.desk && !p.path.length) occupant.set(p.desk, p);
    for (const [dk, scr] of this.screens) {
      const p = occupant.get(dk);
      const col = p ? STATUS_COLOR[p.s.status] : null;
      if (col) {
        const pulse = p.s.status === 'thinking' ? 0.5 + 0.35 * Math.sin(t * 3 + p.phase) : 0.8 + 0.2 * Math.sin(t * 9 + p.phase);
        scr.material.emissive.setHex(col);
        scr.material.emissiveIntensity = pulse;
      } else {
        scr.material.emissive.setHex(0x000000);
      }
    }
    for (const b of this.blinkers) {
      const on = Math.sin(this.t / (180 + b.r * 37 + b.k * 53) + b.r * b.k) > 0.2 && (b.r * 4 + b.k) < Math.max(4, this.deskOf.size * 3);
      b.m.material.emissiveIntensity = on ? 1.2 : 0.05;
    }
    if (this.arcadeScreen) this.arcadeScreen.material.emissive.setHSL((t * 0.05) % 1, 0.7, 0.5);
    if (this.tvScreen) this.tvScreen.material.emissive.setHSL((t / 30) % 1, 0.55, 0.28);

    this.syncActors(dt);
    this.panByKeys(dt);
    this.controls.update();
    this.gl.render(this.scene, this.camera);

    // overlay: bubbles + name tags, reusing the 2D drawing code
    this.hit = [];
    const v = new THREE.Vector3();
    this.zoneHits((x, y, z) => {
      v.set(x, z, y).project(this.camera);
      return { x: (v.x + 1) / 2 * this.cw, y: (1 - v.y) / 2 * this.ch };
    });
    const heads = [];
    for (const p of this.people.values()) {
      const a = this.actors.get(p.key);
      if (!a) continue;
      const st = this.stateOf(p);
      const scale = p.s.subagent ? 0.8 : 1;
      const top = st.pose === 'sit' ? this.seatHeight(p.x, p.y, st.sitZ) + CHAR_HEIGHT * 0.8 * scale : CHAR_HEIGHT * scale;
      v.set(p.x, top + 0.05, p.y).project(this.camera);
      const sx = (v.x + 1) / 2 * this.cw, sy = (1 - v.y) / 2 * this.ch;
      const foot = new THREE.Vector3(p.x, 0, p.y).project(this.camera);
      const fy = (1 - foot.y) / 2 * this.ch;
      heads.push([p, { x: sx, y: sy, act: st.act }, v.z]);
      this.hit.push({ key: p.key, x0: sx - 18, x1: sx + 18, y0: sy - 24, y1: fy + 4 });
    }
    heads.sort((a, b) => b[2] - a[2]);
    for (const [p, at] of heads) this.bubble(p, at);
    if (this.offDuty > 0) {
      c.font = '600 11px ui-sans-serif, system-ui, -apple-system, "PingFang TC", sans-serif';
      c.textAlign = 'left';
      c.fillStyle = this.pal.text;
      c.globalAlpha = 0.65;
      c.fillText(`+${this.offDuty} ${this.offDutyLabel}`, 12, this.ch - 12);
      c.globalAlpha = 1;
    }
  }

  pointer(e, click) {
    // Let OrbitControls drags through without flickering the tooltip.
    if (e.buttons && !click) return;
    super.pointer(e, click);
  }
}
