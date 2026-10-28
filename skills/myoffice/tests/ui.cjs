// Browser integration checks with fixed sample sessions (no private logs).
// Requires Google Chrome; WMAD_HEADED=1 opens visible test windows.
const assert = require('node:assert/strict');
const { chromium } = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { spawn } = require('node:child_process');
process.env.WMAD_NO_UPDATE_CHECK = '1';
// The page is served by the Go binary that scripts/installSkill.sh puts in bin/.
const binary = path.join(__dirname, '..', 'bin', process.platform === 'win32' ? 'whatsmyagentdoing.exe' : 'whatsmyagentdoing');
const screenshots = process.env.WMAD_SCREENSHOTS;
async function screenshot(page, name) {
  if (!screenshots) return;
  fs.mkdirSync(screenshots, { recursive: true });
  await page.screenshot({ path: path.join(screenshots, name + '.png') });
}
const checks=[];
const check=(name)=>{checks.push(name);console.log('PASS',name)};
const sessions = [
 {key:'claude:one', id:'one', agent:'claude',project:'claude-project',status:'thinking',platform:'vscode',title:'Build script',workspace:'/demo/workspace'},
 {key:'codex:two', id:'two', agent:'codex',project:'codex-project',status:'waiting',platform:'cli',promptKind:'notification'},
 {key:'gemini:three', id:'three', agent:'gemini',project:'gemini-project',status:'tool',activity:'Read: index.js',platform:'cli'},
 {key:'claude:done', id:'done', agent:'claude',project:'finished-project',status:'done',platform:'sdk',prompt:'/myoffice',promptKind:'command'},
 {key:'codex:sub', id:'sub', agent:'codex',project:'sub-project',status:'thinking',subagent:true,platform:'vscode'},
 {key:'opencode:four', id:'four', agent:'opencode',project:'opencode-project',status:'typing',platform:'cli'},
].map(s=>({...s,lastEventAt:Date.now(),turnStartedAt:Date.now()-1000,cwd:'/demo/'+s.project}));
async function stable(page){
 await page.waitForFunction(()=>typeof office!=='undefined' && office?.framesShown>4);
 await page.evaluate(s=>{window.__events?.close();onData({now:Date.now(),sessions:s})},sessions);
}
async function openSettings(page) {
 if (!(await page.locator('#settingsMenu').getAttribute('open') !== null)) await page.click('#settingsMenu > summary');
}
async function badges(target){
 return target.evaluate(()=>{
  const found=[];const original=office.agentBadge;
  office.agentBadge=function(a,...rest){found.push(a.short);return original.call(this,a,...rest)};
  office.frame(performance.now());office.agentBadge=original;return found;
 });
}
async function drawnLogos(target) {
 await target.waitForFunction(()=>Object.values(OFFICE_AGENT_BADGES).every(b=>b.image.complete&&b.image.naturalWidth>0));
 return target.evaluate(()=>{
  const found=new Set();const ctx=office.ctx;const original=ctx.drawImage;
  ctx.drawImage=function(img,...args){
   for(const [key,badge] of Object.entries(OFFICE_AGENT_BADGES)) if(img===badge.image) found.add(key);
   return original.call(this,img,...args);
  };
  try {office.frame(performance.now());} finally {ctx.drawImage=original;}
  return [...found].sort();
 });
}
(async()=>{
 if (!fs.existsSync(binary)) { console.error(`missing ${binary}: run scripts/build.sh && scripts/installSkill.sh from the repo root`); process.exitCode = 1; return; }
 const server = spawn(binary, ['--demo', '--no-open', '--port', String(40000 + Math.floor(Math.random() * 20000))], { stdio: ['ignore', 'pipe', 'inherit'] });
 const base = await new Promise((resolve, reject) => {
  let out = '';
  server.stdout.on('data', d => { out += d; const m = out.match(/http:\/\/localhost:(\d+)/); if (m) resolve(`http://127.0.0.1:${m[1]}`); });
  server.once('exit', code => reject(new Error(`dashboard exited before listening (${code})`)));
 });
 let browser;
 const errors=[];
 try {
  const translations = { window: {} };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../public/i18n.js'), 'utf8'), translations);
  const keys = Object.keys(translations.window.I18N.en).sort();
  for (const lang of Object.values(translations.window.I18N)) assert.deepEqual(Object.keys(lang).sort(), keys);
  check('all nine languages provide every UI string');
  browser=await chromium.launch({headless:process.env.WMAD_HEADED !== '1',channel:'chrome',ignoreDefaultArgs:['--disable-background-timer-throttling','--disable-backgrounding-occluded-windows','--disable-renderer-backgrounding'],args:['--window-size=1280,960']});
  const context=await browser.newContext({viewport:{width:1280,height:960},locale:'en-US'});
  await context.addInitScript(sessions=>{
   window.EventSource=class {constructor(){window.__events=this;setTimeout(()=>{if(!this.closed)this.onmessage?.({data:JSON.stringify({now:Date.now(),sessions})})},0)}close(){this.closed=true}};
   window.__notifications=[];
   window.Notification=class {static permission='granted';constructor(title,options){window.__notifications.push({title,options})}};
  },sessions);
  context.on('page',p=>p.on('pageerror',e=>errors.push(String(e))));
  const page=await context.newPage();await page.goto(base);await page.waitForFunction(()=>officeKind!==null);
  assert.equal(await page.evaluate(()=>state.quality),'high');
  assert.equal(await page.locator('header > .controls > #qualSeg').count(),0);
  assert.equal(await page.locator('#settingsPanel #qualSeg').count(),1);
  check('2D/3D lives in the settings panel and defaults to 3D');
  // The 2D checks below run first; the 3D ones switch over later.
  await page.goto(base+'/?quality=mid');await stable(page);
  check('a busy agent with a warning walks to the meeting room and keeps its chips');
  // Where people go: waiting → phone booth queue, warned → meeting room
  // (alone: seated; two or more: arguing), finished → sofas, interrupted (like idle) → pantry.
  const crowd=[
   {key:'q:1',id:'q1',agent:'claude',status:'waiting',lastEventAt:Date.now()-3000},
   {key:'q:2',id:'q2',agent:'codex',status:'waiting',lastEventAt:Date.now()-2000},
   {key:'q:3',id:'q3',agent:'gemini',status:'waiting',lastEventAt:Date.now()-1000},
   {key:'m:1',id:'m1',agent:'claude',status:'tool',alerts:['longTurn'],startedAt:1},
   {key:'f:1',id:'f1',agent:'claude',status:'done',lastEventAt:Date.now()},
   {key:'f:2',id:'f2',agent:'codex',status:'done',lastEventAt:Date.now()-3600000},
   {key:'i:1',id:'i1',agent:'gemini',status:'interrupted',lastEventAt:Date.now()-7200000},
  ].map(s=>({platform:'cli',project:s.id,...s}));
  await page.evaluate(s=>onData({now:Date.now(),sessions:s}),crowd);
  const where=()=>page.evaluate(()=>Object.fromEntries([...office.people.values()].map(p=>[p.key,{mode:p.mode,act:office.stateOf(p).act,pose:office.stateOf(p).pose,zone:p.poi&&p.poi.zone,i:p.spot?office.plan.booth.indexOf(p.spot):-1,desk:!!p.desk}])));
  await page.waitForFunction(()=>[...office.people.values()].every(p=>!p.path.length&&(p.mode!=='wander'||p.poi)),{},{timeout:20000});
  let w=await where();
  assert.deepEqual([w['q:1'].i,w['q:2'].i,w['q:3'].i],[0,1,2]);
  assert.deepEqual([w['q:1'].act,w['q:2'].act],['phone','queue']);
  assert.equal(w['q:1'].desk,false);
  assert.deepEqual([w['m:1'].mode,w['m:1'].act,w['m:1'].pose],['meet','meeting','sit']);
  assert.deepEqual([w['f:1'].zone,w['f:2'].zone],['sofa','sofa']);
  assert.equal(['pantry','lounge'].includes(w['i:1'].zone),true);
  await page.evaluate(s=>onData({now:Date.now(),sessions:[...s.filter(x=>x.key!=='q:1'),{...s[3],key:'m:2',id:'m2',startedAt:2}]}),crowd);
  await page.waitForFunction(()=>[...office.people.values()].every(p=>!p.path.length),{},{timeout:20000});
  w=await where();
  assert.deepEqual([w['q:2'].i,w['q:3'].i],[0,1]);
  assert.deepEqual([w['m:1'].act,w['m:2'].act,w['m:1'].pose],['argue','argue','stand']);
  check('waiting agents queue at the phone booth, warned ones argue in the meeting room, finished ones rest on the sofas');
  const zoneAt=id=>page.evaluate(id=>{office.frame(performance.now());const h=office.hit.find(h=>h.key==='zone:'+id);const r=office.canvas.getBoundingClientRect();return {x:r.left+(h.x0+h.x1)/2,y:r.top+(h.y0+h.y1)/2}},id);
  // A real click on the board (nobody sits at the desks now), the rest straight through the callback.
  const board=await zoneAt('whiteboard');
  await page.mouse.click(board.x,board.y);
  assert.equal(await page.locator('#tip .zone .zone-title').innerText(),'Desks & sales board');
  for(const [id,title] of [['booth','Phone booth'],['tv','Sofa corner'],['fridge','Pantry'],['meeting','Meeting room']]){
   const at=await zoneAt(id);
   await page.evaluate(({id,at})=>officeOpts.onZone(id,at),{id,at});
   assert.equal(await page.locator('#tip .zone .zone-title').innerText(),title);
  }
  await page.evaluate(()=>officeOpts.onHover(null,{x:0,y:0},true));
  assert.equal(await page.locator('#tip').isVisible(),false);
  check('both boards, the phone booth, TV and fridge explain their area when clicked');
  await page.evaluate(s=>onData({now:Date.now(),sessions:s}),sessions);await stable(page);
  await page.waitForFunction(()=>[...office.people.values()].every(p=>!p.leaving),{},{timeout:30000});
  assert.match(await page.title(),/Agents Office$/);
  assert.equal(await page.locator('h1').count(),0);
  assert.equal(await page.evaluate(()=>document.querySelector('#summary').getBoundingClientRect().left>document.querySelector('.controls').getBoundingClientRect().right),true);
  await page.waitForSelector('#chkBtn');
  assert.match(await page.locator('#chkBtn').getAttribute('data-ver'),/^v\d/);
  assert.match(await page.locator('#clock').innerText(),/\d/);
  check('no title row: controls left, counts right, version on the check button, clock in the corner');
  assert.deepEqual(await page.locator('#summary button.chip').evaluateAll(bs=>bs.map(b=>b.dataset.g)),['needsYou','error','working','finished','idle']);
  assert.match(await page.locator('#summary button.chip[data-g="finished"]').innerText(),/1$/);
  await page.click('#summary button.chip[data-g="finished"]');
  assert.equal(await page.locator('#summary button.chip[data-g="finished"]').getAttribute('aria-pressed'),'false');
  assert.match(await page.locator('#summary button.chip[data-g="finished"]').innerText(),/1$/);
  assert.equal(await page.evaluate(()=>office.people.get('claude:done')?.leaving),true);
  await openSettings(page);
  assert.equal(await page.locator('#statusToggles input[data-g="finished"]').isChecked(),false);
  await page.check('#statusToggles input[data-g="finished"]');
  assert.equal(await page.locator('#summary button.chip[data-g="finished"]').getAttribute('aria-pressed'),'true');
  assert.equal(await page.evaluate(()=>office.people.get('claude:done')?.leaving),false);
  await page.click('#viewSeg button[data-v="list"]');
  await page.click('#summary button.chip[data-g="working"]');
  assert.equal(await page.locator('#main section h2').evaluateAll(hs=>hs.map(h=>h.textContent.replace(/\s*\d+$/,''))).then(a=>a.includes('Working')),false);
  await page.click('#summary button.chip[data-g="working"]');
  await page.click('#viewSeg button[data-v="office"]');await stable(page);
  check('status counts are switches: they hide the group in the office and list, keep the number, and mirror the settings checkboxes');
  assert.equal(await page.locator('#showAgent').isChecked(),true);
  assert.deepEqual([...new Set(await badges(page))].sort(),['Cl','Cx','G','Oc']);
  assert.deepEqual(await drawnLogos(page),['claude','codex','gemini','opencode']);
  assert.equal(await page.locator('#agentLegend img').evaluateAll(imgs=>imgs.length===4&&imgs.every(img=>img.complete&&img.naturalWidth>0)),true);
  check('all official local logos load and draw in 2D and the legend');
  await openSettings(page);await page.check('#showSub');
  assert.equal((await badges(page)).length,6);
  check('2D badges identify all four providers and subagents without duplicate activity badges');
  assert.equal(await page.evaluate(()=>office.label({project:'p',promptKind:'command'})),'⌨️ p');
  assert.equal(await page.evaluate(()=>office.label({project:'p',promptKind:'notification'})),'🔔 p');
  assert.equal(await page.evaluate(()=>office.sublabel({platform:'vscode'})),'VS Code');
  assert.equal(await page.evaluate(()=>office.sublabel({})),'');
  assert.equal(await page.evaluate(()=>[...office.people.values()].every(p=>OFFICE_PLATFORM_HAIR[p.s.platform].includes(p.hair))),true);
  await openSettings(page);
  assert.equal(await page.locator('#agentToggles input:checked, #platformToggles input:checked').count(),7);
  assert.equal(await page.locator('#agentToggles input[data-v=all], #platformToggles input[data-v=all]').count(),0);
  await page.uncheck('#platformToggles input[data-v=cli]');await page.uncheck('#platformToggles input[data-v=other]');
  assert.equal(await page.locator('#activeFilter').innerText(),'VS Code');
  await page.click('#viewSeg [data-v=list]');
  assert.deepEqual(await page.locator('.card .platform').allInnerTexts(),['VS Code','VS Code']);
  await openSettings(page);await page.check('#platformToggles input[data-v=other]');await page.uncheck('#platformToggles input[data-v=vscode]');
  assert.deepEqual(await page.locator('.card .platform').allInnerTexts(),['SDK']);
  await page.reload();await page.waitForFunction(()=>state.loaded);
  await page.evaluate(s=>{__events.close();onData({now:Date.now(),sessions:s})},sessions);
  assert.deepEqual(await page.locator('.card .platform').allInnerTexts(),['SDK']);
  await openSettings(page);await page.check('#platformToggles input[data-v=vscode]');await page.check('#platformToggles input[data-v=cli]');
  assert.equal(await page.locator('#activeFilter').isVisible(),false);
  assert.equal(await page.locator('.card').count(),6);
  assert.deepEqual(await page.locator('.card .kind').allInnerTexts(),['🔔 Background task','⌨️ Slash command']);
  assert.equal(await page.locator('.card .meta').count(),0);
  await page.click('#viewSeg [data-v=office]');await stable(page);
  check('platform multi-select narrows the list and persists, cards name the platform and prompt kind, 2D hair and labels follow platform and prompt kind');
  await page.evaluate(()=>officeOpts.onHover(state.data.sessions.find(s=>s.key==='codex:two'),{x:40,y:40},true));
  assert.equal(await page.locator('#tip .kind').innerText(),'🔔 Background task');
  assert.deepEqual(await page.locator('#tip .meta dt').allInnerTexts(),['Folder','Last activity','Session']);
  assert.deepEqual(await page.locator('#tip .meta dd').allInnerTexts(),['/demo/codex-project','now','two']);
  await page.keyboard.press('Escape');assert.equal(await page.locator('#tip').isVisible(),false);
  check('the hover card tags the prompt kind and lists folder, times and ids under the preview');
  await page.evaluate(()=>officeOpts.onHover(state.data.sessions.find(s=>s.key==='claude:one'),{x:40,y:40},true));
  assert.equal(await page.locator('#tip .title').innerText(),'Build script');
  assert.deepEqual(await page.locator('#tip .meta dt').allInnerTexts(),['Folder','Workspace','Last activity','Session']);
  assert.deepEqual(await page.locator('#tip .foot .copy').allInnerTexts(),['Copy resume cmd','Copy title']);
  assert.equal(await page.locator('#tip .foot .copy').nth(1).getAttribute('data-copy'),'Build script');
  assert.equal(await page.locator('#tip .foot .open').getAttribute('href'),'vscode://anthropic.claude-code/open?session=one');
  assert.equal(await page.evaluate(()=>vscodeLink({agent:'codex',platform:'vscode',id:'t1'})),'<a class="open" href="vscode://openai.chatgpt/local/t1">Open in VS Code</a>');
  assert.equal(await page.evaluate(()=>vscodeLink({agent:'gemini',platform:'vscode',id:'g1'})),'');
  assert.equal(await page.evaluate(()=>resumeCmd({agent:'gemini',tool:'agy',id:'c1',cwd:'/w'})),"cd '/w' && agy --conversation 'c1'");
  assert.equal(await page.evaluate(()=>resumeCmd({agent:'gemini',id:'g1',sessionId:'s1'})),"gemini --resume 's1'");
  assert.equal(await page.evaluate(()=>resumeCmd({agent:'opencode',id:'o1',cwd:'/w'})),"cd '/w' && opencode --session 'o1'");
  await page.keyboard.press('Escape');
  await page.click('#viewSeg [data-v=list]');
  assert.equal(await page.locator('.grid .card .open').count(),1);
  assert.equal(await page.locator('.grid .card .meta').count(),0);
  await page.click('#viewSeg [data-v=office]');await stable(page);
  check('the card shows the IDE title, a copy-title button, the workspace row and a VS Code deep link for VS Code Claude sessions');
  const warned=[{...sessions[0],key:'claude:warn',id:'warn',status:'tool',activity:'Bash: make',alerts:['compact','longTurn'],contextTokens:330000,compactAt:367000,turnStartedAt:Date.now()-75*60000,turnStartApprox:true}];
  await page.evaluate(s=>onData({now:Date.now(),sessions:s}),warned);
  await page.waitForFunction(()=>[...office.people.values()].some(p=>p.key==='claude:warn'&&p.mode==='meet'&&!p.path.length),{},{timeout:15000});
  assert.deepEqual(await page.evaluate(()=>{
   const found=[];const ctx=office.ctx;const original=ctx.fillText;
   ctx.fillText=function(text,...args){if(/^(ctx \d+%|\d+m|\d+h\d+m)$/.test(text))found.push(text);return original.call(this,text,...args)};
   try {office.frame(performance.now());} finally {ctx.fillText=original;}
   return found.sort();
  }),['75m','ctx 90%']);
  assert.deepEqual((await page.locator('#legend .alert-key').allInnerTexts()).map(t=>t.replace(/\s+/g,' ')),['ctx % Near auto-compact','min Long turn']);
  await page.evaluate(()=>officeOpts.onHover(state.data.sessions[0],{x:40,y:40},true));
  assert.deepEqual((await page.locator('#tip .alert').allInnerTexts()).map(t=>t.replace(/\s+/g,' ')),['ctx 90% Near auto-compact','75m Long turn']);
  assert.equal(await page.locator('#tip .meta dd').nth(3).innerText(),'90% (330k / 367k)');
  await page.keyboard.press('Escape');
  await page.click('#viewSeg [data-v=list]');
  assert.deepEqual((await page.locator('.grid .card .alert').allInnerTexts()).map(t=>t.replace(/\s+/g,' ')),['ctx 90% Near auto-compact','75m Long turn']);
  assert.match(await page.locator('.grid .card .time').innerText(),/^≥ 1h 15m/);
  await page.click('#viewSeg [data-v=office]');await stable(page);
  check('warnings draw as numeric chips in the office and show on cards, the hover card and the legend; an approximate turn start reads ≥');
  await openSettings(page);await page.uncheck('#showAgent');assert.equal((await badges(page)).length,0);
  assert.equal(await page.locator('#agentLegend').isVisible(),false);
  await page.reload();await stable(page);assert.equal(await page.locator('#showAgent').isChecked(),false);
  await openSettings(page);await page.check('#showAgent');check('badge toggle hides markers and legend and persists across reload');
  await openSettings(page);await page.click('#qualSeg [data-v=high]');
  await page.waitForFunction(()=>officeKind==='high'&&office?.loaded&&office.framesShown>4,{},{timeout:30000});
  assert.deepEqual([...new Set(await badges(page))].sort(),['Cl','Cx','G','Oc']);
  await screenshot(page, 'office-3d');
  assert.deepEqual(await drawnLogos(page),['claude','codex','gemini','opencode']);
  check('3D renders the same official provider logos');
  assert.deepEqual(await page.evaluate(()=>[...office.people.values()].map(p=>[p.s.platform,office.modelName(p).split('-')[0]]).filter(([pf])=>pf==='vscode'||pf==='cli').map(([pf,g])=>`${pf}:${g}`).sort()),['cli:male','cli:male','cli:male','vscode:female','vscode:female']);
  check('3D picks the model group from the platform');
  // Trackpad: two-finger scrolling pans, a pinch (Chrome: Ctrl+wheel) zooms, arrow keys pan, a resize keeps the pan.
  await page.keyboard.press('Escape');await page.evaluate(()=>document.activeElement?.blur());
  const cam=()=>page.evaluate(()=>({zoom:office.camera.zoom,x:office.controls.target.x,y:office.controls.target.y,z:office.controls.target.z}));
  await page.mouse.move(640,520);
  let cam0=await cam();await page.mouse.wheel(0,200);await page.waitForTimeout(150);let cam1=await cam();
  assert.equal(cam1.zoom,cam0.zoom);assert.notDeepEqual([cam1.x,cam1.y,cam1.z],[cam0.x,cam0.y,cam0.z]);
  await page.keyboard.down('Control');await page.mouse.wheel(0,-200);await page.keyboard.up('Control');await page.waitForTimeout(400);
  assert.ok((await cam()).zoom>cam1.zoom);
  assert.equal(await page.evaluate(()=>scrollY),0);
  cam0=await cam();await page.keyboard.down('ArrowRight');await page.waitForTimeout(400);await page.keyboard.up('ArrowRight');cam1=await cam();
  assert.notDeepEqual([cam1.x,cam1.y,cam1.z],[cam0.x,cam0.y,cam0.z]);
  await page.evaluate(()=>office.resize());await page.waitForTimeout(100);
  const cam2=await cam();assert.deepEqual([cam2.x,cam2.y,cam2.z].map(v=>v.toFixed(3)),[cam1.x,cam1.y,cam1.z].map(v=>v.toFixed(3)));
  await page.evaluate(()=>office.fitCamera3D(true));
  check('3D: two-finger scroll and arrow keys pan, a pinch zooms, and a resize keeps the pan');
  // Grow past the desks and back twice; the first round also uploads the shared textures of models not yet on screen.
  const actors0=await page.evaluate(()=>office.actors.size);
  const textures=[];
  for(let round=0;round<2;round++){
   for(const extra of [16,0]){
    const f=await page.evaluate(n=>{office.update([...visible(),...Array.from({length:n},(_,i)=>({key:'w:'+i,id:'w'+i,agent:'claude',status:'error',platform:'cli',project:'p'+i}))]);return office.framesShown},extra);
    await page.waitForFunction(f=>office.framesShown>f+10,f);
   }
   assert.equal(await page.evaluate(()=>office.plan.desks.length),8);
   await page.waitForFunction(n=>office.actors.size===n,actors0,{timeout:30000});
   textures.push(await page.evaluate(()=>office.gl.info.memory.textures));
  }
  assert.equal(textures[1],textures[0]);
  check('3D keeps animating when the floor plan regrows and shrinks, and frees what it drops');
  await page.click('#pipBtn');await page.waitForFunction(()=>!!popout.win);
  const pip=context.pages().find(p=>p!==page);assert.ok(pip);
  const frame=await (async()=>{for(let i=0;i<100;i++){const f=pip.frames().find(f=>f.url().includes('?pip=1'));if(f)return f;await new Promise(r=>setTimeout(r,50))}throw Error('No compact frame')})();
  await frame.waitForFunction(()=>office?.loaded&&office.framesShown>4);
  await frame.evaluate(s=>{window.__events?.close();onData({now:Date.now(),sessions:s})},sessions);
  assert.deepEqual(await drawnLogos(frame),['claude','codex','gemini','opencode']);
  assert.equal(await page.evaluate(()=>office.visible),false);
  assert.equal(await frame.evaluate(()=>state.quality),'high');
  assert.equal(await frame.locator('#pipBtn').isVisible(),false);
  assert.equal(await frame.locator('#qualSeg').isVisible(),false);
  await pip.setViewportSize({width:480,height:400});
  await frame.waitForFunction(()=>!document.getElementById('office').classList.contains('loading'));await page.waitForTimeout(400);await screenshot(pip, 'pip-3d');
  assert.equal(await frame.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  check('real Document PiP opens 3D at compact size and suspends the main office');
  await openSettings(page);await page.uncheck('#showAgent');await frame.waitForFunction(()=>!state.showAgent&&!office.showAgent);
  await openSettings(page);await page.selectOption('#langSel','zh-TW');await frame.waitForFunction(()=>langCode==='zh-TW');
  await openSettings(page);for(const a of ['claude','gemini','opencode'])await page.uncheck(`#agentToggles input[data-v=${a}]`);await frame.waitForFunction(()=>state.hiddenAgents.length===3);
  await frame.click('#viewSeg [data-v=list]');await page.waitForFunction(()=>state.view==='list');
  assert.equal(await frame.locator('section').count(),2);
  assert.equal(await frame.locator('.card[data-s=done]').count(),0);
  assert.equal(await frame.locator('.card .agent.claude').count(),0);
  assert.equal(await frame.locator('.card .agent img').evaluateAll(imgs=>imgs.length>0&&imgs.every(img=>img.complete&&img.naturalWidth>0)),true);
  await screenshot(pip, 'pip-list');
  check('preferences synchronize in both directions; compact list shows only attention/working sessions');
  const next=sessions.map(s=>s.key==='codex:sub'?{...s,status:'done',subagent:false}:s);
  await frame.evaluate(s=>onData({now:Date.now(),sessions:s}),next);
  await page.evaluate(s=>onData({now:Date.now(),sessions:s}),next);
  assert.equal(await frame.evaluate(()=>__notifications.length),0);
  assert.equal(await page.evaluate(()=>__notifications.length),1);
  check('only the main page emits completion notifications');
  await openSettings(page);for(const a of ['claude','gemini','opencode'])await page.check(`#agentToggles input[data-v=${a}]`);
  await frame.click('#viewSeg [data-v=office]');await page.waitForFunction(()=>state.view==='office');
  await openSettings(page);await page.check('#showAgent');await page.click('#qualSeg [data-v=mid]');
  await frame.waitForFunction(()=>officeKind==='mid'&&office.framesShown>4);
  await page.waitForFunction(()=>state.quality==='mid');
  const foreground=await context.newPage();await foreground.goto('about:blank');await foreground.bringToFront();
  const before=await frame.evaluate(()=>({frames:office.framesShown,visible:document.visibilityState}));
  await new Promise(r=>setTimeout(r,1100));
  const after=await frame.evaluate(()=>({frames:office.framesShown,visible:document.visibilityState}));
  assert.ok(after.frames>before.frames,JSON.stringify({before,after}));
  await foreground.close();check('pop-out keeps animating while another tab is foreground');
  await pip.close();await page.waitForFunction(()=>!popout.win&&office.visible);
  await page.bringToFront();await page.waitForFunction(()=>office.framesShown>4);
  await page.click('#pipBtn');await page.waitForFunction(()=>!!popout.win);
  await page.click('#pipBtn');await page.waitForFunction(()=>!popout.win&&office.visible);
  check('native close and button close restore the main view; reopening works');
  await page.evaluate(()=>{documentPictureInPicture.requestWindow=()=>Promise.reject(new Error('Test rejection'))});
  await page.click('#pipBtn');await page.waitForFunction(()=>!popout.busy&&!document.getElementById('pipError').hidden);
  assert.equal(await page.evaluate(()=>office.visible),true);
  assert.equal(await page.locator('#pipBtn').isEnabled(),true);
  check('request failure gives a translated error and leaves the dashboard usable');
  // Force only the capability branch; rendering and MediaStream remain real.
  const fallback=await context.newPage();
  await fallback.addInitScript(()=>Object.defineProperty(window,'documentPictureInPicture',{value:undefined}));
  await fallback.goto(base+'/?view=office&quality=high');await stable(fallback);
  await fallback.waitForFunction(()=>office.loaded&&office.framesShown>4);
  await fallback.click('#pipBtn');
  await fallback.waitForFunction(()=>!!popout.stream&&!popout.busy&&document.getElementById('pipVideo').videoWidth>0);
  assert.equal(await fallback.locator('#pipPreview').isVisible(),true);
  const colors=await fallback.evaluate(()=>{
   const data=popout.captureContext.getImageData(0,0,960,540).data;const values=new Set();
   for(let i=0;i<data.length;i+=160)values.add(`${data[i]},${data[i+1]},${data[i+2]}`);
   window.__stream=popout.stream;return values.size;
  });
  assert.ok(colors>20,'preview includes rendered 3D content rather than a blank frame');
  await openSettings(fallback);await fallback.click('#qualSeg [data-v=mid]');await fallback.waitForFunction(()=>officeKind==='mid'&&office.framesShown>4);
  assert.equal(await fallback.evaluate(()=>popout.stream===__stream),true);
  await fallback.click('#closePreview');await fallback.waitForFunction(()=>!popout.stream);
  assert.equal(await fallback.evaluate(()=>__stream.getTracks().every(t=>t.readyState==='ended')),true);
  assert.equal(await fallback.evaluate(()=>document.getElementById('pipVideo').srcObject),null);
  check('video fallback captures 3D + overlay, survives renderer changes and releases media tracks on stop');
  await fallback.click('#viewSeg [data-v=list]');assert.equal(await fallback.locator('#pipBtn').isVisible(),false);
  check('view-only fallback is offered only for office mode');
  const compact=await context.newPage();await compact.goto(base+'/?pip=1&view=list');
  await compact.waitForFunction(()=>state.loaded);
  await compact.evaluate(()=>{__events.close();onData({now:Date.now(),sessions:[]})});
  assert.match(await compact.locator('#main').innerText(),/目前沒有|No agents/);
  await compact.reload();assert.equal(await compact.evaluate(()=>PIP),true);
  check('empty compact list explains the quiet state and retains compact mode after reload');
  await page.bringToFront();
  await openSettings(page);
  await page.locator('#settingsMenu > summary').press('Escape');
  assert.equal(await page.locator('#settingsPanel').isVisible(),false);
  assert.equal(await page.locator('#settingsMenu > summary').evaluate(el=>el===document.activeElement),true);
  await openSettings(page);await page.click('body',{position:{x:5,y:5}});
  assert.equal(await page.locator('#settingsPanel').isVisible(),false);
  for (const width of [320,390,768,1280]) {
   await page.setViewportSize({width,height:800});
   for (const colorScheme of ['light','dark']) {
    await page.emulateMedia({colorScheme});
    await openSettings(page);
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
    const panel=await page.locator('#settingsPanel').boundingBox();
    assert.ok(panel.x>=0 && panel.x+panel.width<=width);
    await page.locator('#settingsMenu > summary').press('Escape');
    await screenshot(page,`toolbar-${width}-${colorScheme}`);
   }
  }
  check('settings close with Escape/outside click, restore focus, and fit narrow and wide screens in both themes');
  assert.deepEqual(errors,[]);check('no uncaught browser errors');
  console.log('TOTAL',checks.length);
 } finally {
  await browser?.close();
  server.kill();
 }
})().catch(e=>{console.error(e);process.exitCode=1});
