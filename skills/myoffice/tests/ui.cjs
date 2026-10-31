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
 const server = spawn(binary, ['--demo', '--no-open', '--port', String(40000 + Math.floor(Math.random() * 20000)), '--tv-dir', path.join(require('node:os').tmpdir(), 'wmad-no-tv-' + process.pid)], { stdio: ['ignore', 'pipe', 'inherit'] });
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
  // Where people go: waiting → hotline desk queue, warned → meeting room
  // (alone: seated; two or more: arguing), finished → lounge seats, then the TV, interrupted (like idle) → pantry.
  const crowd=[
   {key:'q:1',id:'q1',agent:'claude',status:'waiting',lastEventAt:Date.now()-3000},
   {key:'q:2',id:'q2',agent:'codex',status:'waiting',lastEventAt:Date.now()-2000},
   {key:'q:3',id:'q3',agent:'gemini',status:'waiting',lastEventAt:Date.now()-1000},
   {key:'m:1',id:'m1',agent:'claude',status:'tool',alerts:['longTurn'],startedAt:1},
   {key:'f:1',id:'f1',agent:'claude',status:'done',lastEventAt:Date.now()},
   {key:'f:2',id:'f2',agent:'codex',status:'done',lastEventAt:Date.now()-3600000},
   ...[3,4,5,6].map(n=>({key:'f:'+n,id:'f'+n,agent:'claude',status:'done',lastEventAt:Date.now()-n*60000})),
   {key:'i:1',id:'i1',agent:'gemini',status:'interrupted',lastEventAt:Date.now()-7200000},
  ].map(s=>({platform:'cli',project:s.id,...s}));
  await page.evaluate(s=>onData({now:Date.now(),sessions:s}),crowd);
  const where=()=>page.evaluate(()=>Object.fromEntries([...office.people.values()].map(p=>[p.key,{mode:p.mode,act:office.stateOf(p).act,pose:office.stateOf(p).pose,zone:p.poi&&p.poi.zone,i:p.spot?office.plan.booth.indexOf(p.spot):-1,desk:!!p.desk}])));
  await page.waitForFunction(()=>[...office.people.values()].every(p=>!p.path.length&&(p.mode!=='wander'||p.poi)),{},{timeout:20000});
  let w=await where();
  assert.deepEqual([w['q:1'].i,w['q:2'].i,w['q:3'].i],[0,1,2]);
  assert.deepEqual([w['q:1'].act,w['q:1'].pose,w['q:2'].act],['type','sit','queue']);
  assert.equal(w['q:1'].desk,false);
  assert.deepEqual([w['m:1'].mode,w['m:1'].act,w['m:1'].pose],['meet','meeting','sit']);
  // Five lounge seats (sofa: scrolling, beanbags: music) fill first; the sixth watches TV.
  const fin=Object.keys(w).filter(k=>k.startsWith('f:')).map(k=>w[k]);
  assert.equal(fin.filter(v=>v.zone==='lounge'&&['scroll','beanbag'].includes(v.act)).length,5);
  assert.deepEqual(fin.filter(v=>v.zone!=='lounge').map(v=>v.act),['tv']);
  assert.equal(['pantry','lounge'].includes(w['i:1'].zone),true);
  await page.evaluate(s=>onData({now:Date.now(),sessions:[...s.filter(x=>x.key!=='q:1'),{...s[3],key:'m:2',id:'m2',startedAt:2}]}),crowd);
  await page.waitForFunction(()=>[...office.people.values()].every(p=>!p.path.length),{},{timeout:20000});
  w=await where();
  assert.deepEqual([w['q:2'].i,w['q:3'].i],[0,1]);
  assert.deepEqual([w['m:1'].act,w['m:2'].act,w['m:1'].pose],['argue','argue','stand']);
  check('waiting agents queue at the hotline desk, warned ones argue in the meeting room, finished ones fill the lounge seats before the TV');
  const zoneAt=id=>page.evaluate(id=>{office.frame(performance.now());const h=office.hit.find(h=>h.key==='zone:'+id);const r=office.canvas.getBoundingClientRect();return {x:r.left+(h.x0+h.x1)/2,y:r.top+(h.y0+h.y1)/2}},id);
  // A real click on the board (nobody sits at the desks now), the rest straight through the callback.
  const board=await zoneAt('whiteboard');
  await page.mouse.click(board.x,board.y);
  assert.equal(await page.locator('#tip .zone .zone-title').innerText(),'Desks & sales board');
  for(const [id,title] of [['booth','Hotline desk'],['tv','Sofa corner'],['fridge','Pantry'],['meeting','Meeting room']]){
   const at=await zoneAt(id);
   await page.evaluate(({id,at})=>officeOpts.onZone(id,at),{id,at});
   assert.equal(await page.locator('#tip .zone .zone-title').innerText(),title);
  }
  await page.evaluate(()=>officeOpts.onHover(null,{x:0,y:0},true));
  assert.equal(await page.locator('#tip').isVisible(),false);
  check('both boards, the hotline desk, TV and fridge explain their area when clicked');
  // The boss is clickable too: per agent, who is working, on an error or waiting on you (finished ones left out),
  // then turns, completion and tool calls for 5 h / 24 h / this week from /api/stats (made-up numbers under --demo).
  assert.equal(await page.evaluate(()=>{office.frame(performance.now());return office.hit.some(h=>h.key==='zone:boss')}),true);
  const bossAt=await zoneAt('boss');
  await page.evaluate(at=>officeOpts.onZone('boss',at),bossAt);
  assert.equal(await page.locator('#tip .boss .zone-title').innerText(),'The boss’s report');
  assert.deepEqual(await page.locator('#tip .boss li').evaluateAll(els=>els.map(e=>e.textContent.replace(/\s+/g,' ').trim())),['ClaudeWorking 2','CodexWaiting for you 1','GeminiWaiting for you 1']);
  await page.waitForFunction(()=>document.querySelectorAll('#tip .boss tbody tr').length===3,{},{timeout:10000});
  assert.deepEqual(await page.locator('#tip .boss tbody tr').evaluateAll(els=>els.map(e=>[...e.children].map(c=>c.textContent))),[['Last 5 h','14','85% 11/13','96'],['Last 24 h','52','86% 44/51','410'],['This week','187','86% 160/186','1,530']]);
  await page.evaluate(()=>officeOpts.onHover(null,{x:0,y:0},true));
  check('clicking the boss shows who is busy per agent and the 5 h / 24 h / week stats');
  // With nothing in the --tv-dir folder the TV plays the four built-in clips, frame after frame, and moves on to the next.
  await page.waitForFunction(()=>office.tvPlayer.ready&&office.tvPlayer.clips.length===4,{},{timeout:20000});
  assert.ok((await page.evaluate(()=>office.tvPlayer.clips)).every(c=>c.startsWith('/assets/tv/')));
  const tv0=await page.evaluate(()=>office.tvPlayer.version);
  await page.waitForFunction(v=>office.tvPlayer.version>v+3,tv0,{timeout:5000});
  const clip0=await page.evaluate(()=>office.tvPlayer.i);
  await page.evaluate(()=>office.tvPlayer.next());
  await page.waitForFunction(i=>office.tvPlayer.i===(i+1)%4&&office.tvPlayer.dec&&office.tvPlayer.frame>0,clip0,{timeout:5000});
  check('the TV plays the built-in public-domain GIFs one after another');
  // The boss erases and rewrites the sales board, then the meeting board; each board moves to its next edition.
  const inRound='(office.boss.task?.board||office.boss.tasks.some(t=>t.board))';
  await page.waitForFunction(`!${inRound}`,{},{timeout:45000});
  const ed0=await page.evaluate(()=>[office.boards.sales.ed,office.boards.meet.ed]);
  await page.evaluate(()=>{office.boss.next=0});
  await page.waitForFunction(()=>office.bossAct()==='erase'&&office.boss.task.board==='sales',{},{timeout:20000});
  await page.waitForFunction(()=>office.bossAct()==='write'&&office.boss.task.board==='meet',{},{timeout:40000});
  await page.waitForFunction(`!${inRound}`,{},{timeout:30000});
  assert.deepEqual(await page.evaluate(()=>[office.boards.sales.ed,office.boards.meet.ed,office.boards.sales.prog,office.boards.meet.prog]),[ed0[0]+1,ed0[1]+1,null,null]);
  // The meeting board turns its page every few seconds; the fridge puffs now and then.
  const page0=await page.evaluate(()=>office.boards.meet.page);
  await page.waitForFunction(p=>office.boards.meet.page!==p,page0,{timeout:8000});
  assert.ok(await page.evaluate(()=>office.fridgePuff(0)===0&&office.fridgePuff(10000)===null&&office.fridgeSmoke(0,0,0.5).length>0));
  check('the boss rewrites both whiteboards each round, the meeting board turns pages and the fridge puffs');
  // Between rounds he talks to finished people in turn; with none of them he stands behind someone at a desk for 4–8 s.
  await page.waitForFunction(()=>office.bossAct()==='talk',{},{timeout:30000});
  assert.equal(await page.evaluate(()=>office.people.get(office.boss.task.visit).s.status),'done');
  // Hold off the next board round so it can't start before he gets to the desk.
  await page.evaluate(()=>{office.boss.next=Date.now()+120000});
  await page.evaluate(()=>onData({now:Date.now(),sessions:[{key:'w:1',id:'w1',agent:'claude',project:'w1',status:'tool',platform:'cli',lastEventAt:Date.now(),startedAt:1}]}));
  await page.waitForFunction(()=>office.bossAct()==='watch',{},{timeout:30000});
  const watch=await page.evaluate(()=>{const t=office.boss.task,p=office.people.get(t.visit);return {mode:p.mode,dur:t.dur,behind:Math.sign(office.boss.y-p.desk.seat.y)===(p.desk.side==='n'?-1:1)}});
  assert.equal(watch.mode,'work');assert.ok(watch.dur>=4000&&watch.dur<=8000);assert.equal(watch.behind,true);
  check('between rounds the boss talks to finished agents, or else watches over a working one\'s shoulder');
  // Nobody left: he flips the switch by the door and runs out; the office stays on screen in the dark, and he skips his rounds.
  await page.evaluate(()=>onData({now:Date.now(),sessions:[]}));
  await page.waitForFunction(()=>office.bossAct()==='switch',{},{timeout:30000});
  await page.waitForFunction(()=>office.boss.out&&!office.lightsOn,{},{timeout:30000});
  assert.equal(await page.locator('#officeWrap').isVisible(),true);
  assert.equal(await page.locator('#main .empty').count(),0);
  await page.evaluate(()=>{office.boss.next=0});await page.waitForTimeout(300);
  assert.deepEqual(await page.evaluate(()=>[office.boss.out,office.boss.tasks.length,office.bossAct()]),[true,0,'out']);
  check('with no agents the boss turns the lights off and leaves, and the dark office replaces the empty message');
  await page.evaluate(s=>onData({now:Date.now(),sessions:s}),sessions);await stable(page);
  assert.deepEqual(await page.evaluate(()=>[office.lightsOn,office.boss.out]),[true,false]);
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
  assert.equal(await page.locator('#viewSeg').isVisible(),false);
  assert.equal(await page.locator('#officeWrap').isVisible(),true);
  assert.equal(await page.evaluate(()=>document.getElementById('main').getBoundingClientRect().top>=document.getElementById('officeWrap').getBoundingClientRect().bottom),true);
  assert.ok(await page.locator('#main section .card').count()>0);
  check('the main page has no office/list switch: the office sits on top with the list under it');
  const officeH=()=>page.evaluate(()=>document.getElementById('office').getBoundingClientRect().height);
  const h0=await officeH();const bar=await page.locator('#officeResize').boundingBox();
  await page.mouse.move(bar.x+bar.width/2,bar.y+bar.height/2);await page.mouse.down();
  await page.mouse.move(bar.x+bar.width/2,bar.y+bar.height/2-150,{steps:5});await page.mouse.up();
  assert.ok(Math.abs(await officeH()-(h0-150))<=2);
  assert.equal(await page.evaluate(()=>JSON.parse(localStorage.getItem('wmad.officeHeight'))),Math.round(h0-150));
  await page.locator('#officeResize').dblclick();
  assert.equal(await officeH(),h0);
  assert.equal(await page.evaluate(()=>JSON.parse(localStorage.getItem('wmad.officeHeight'))),null);
  check('dragging the bar under the office resizes it and remembers the height; double-click resets');
  await page.click('#summary button.chip[data-g="working"]');
  assert.equal(await page.locator('#main section h2').evaluateAll(hs=>hs.map(h=>h.textContent.replace(/\s*\d+$/,''))).then(a=>a.includes('Working')),false);
  await page.click('#summary button.chip[data-g="working"]');
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
  check('platform multi-select narrows the list and persists, cards name the platform and prompt kind, 2D hair and labels follow platform and prompt kind');
  // The fixture's lastEventAt dates from start-up; reset it so "now" doesn't depend on how long the run took.
  await page.evaluate(s=>onData({now:Date.now(),sessions:s.map(x=>({...x,lastEventAt:Date.now()}))}),sessions);
  await page.evaluate(()=>officeOpts.onHover(state.data.sessions.find(s=>s.key==='codex:two'),{x:40,y:40},false));
  assert.equal(await page.locator('#tip .kind').innerText(),'🔔 Background task');
  assert.deepEqual(await page.locator('#tip .meta dt').allInnerTexts(),['Folder','Last activity','Session']);
  assert.deepEqual(await page.locator('#tip .meta dd').allInnerTexts(),['/demo/codex-project','now','two']);
  await page.keyboard.press('Escape');assert.equal(await page.locator('#tip').isVisible(),false);
  check('the hover card tags the prompt kind and lists folder, times and ids under the preview');
  await page.evaluate(()=>officeOpts.onHover(state.data.sessions.find(s=>s.key==='claude:one'),{x:40,y:40},false));
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
  assert.equal(await page.locator('.grid .card .open').count(),1);
  assert.equal(await page.locator('.grid .card .meta').count(),0);
  check('the card shows the IDE title, a copy-title button, the workspace row and a VS Code deep link for VS Code Claude sessions');
  await page.evaluate(s=>onData({now:Date.now(),sessions:s.map(x=>x.key==='opencode:four'?{...x,reply:'Halfway there'}:x)}),sessions);
  await page.evaluate(()=>officeOpts.onHover(state.data.sessions.find(s=>s.key==='opencode:four'),{x:40,y:40},false));
  assert.equal(await page.locator('#tip .r').innerText(),'Halfway there');
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('.grid .card[data-key="opencode:four"] .r').count(),0);
  check('the office card shows what a busy agent has written so far; the list card still hides it');
  // A click pins the office card; its Details button (and the one on each list card) opens the dialog with the full text, whatever the status.
  await page.route('**/api/session?*',r=>{const key=new URL(r.request().url()).searchParams.get('key');r.fulfill({json:{key,prompt:'Line one\nLine two',reply:key==='codex:two'?null:'Full reply\nsecond line'}})});
  await page.evaluate(()=>officeOpts.onHover(state.data.sessions.find(s=>s.key==='opencode:four'),{x:40,y:40},true));
  assert.equal(await page.locator('#tip.pinned').isVisible(),true);
  assert.equal(await page.locator('#detail').isVisible(),false);
  await page.click('#tip [data-detail]');
  await page.waitForFunction(()=>document.getElementById('dReply').textContent==='Full reply\nsecond line');
  assert.equal(await page.locator('#tip').isVisible(),false);
  assert.equal(await page.locator('#dCard .card[data-s="typing"]').count(),1);
  assert.equal(await page.locator('#dPrompt').innerText(),'Line one\nLine two');
  assert.equal(await page.locator('#dReplyLabel').innerText(),'Reply so far');
  assert.equal(await page.locator('#dMeta .meta dt').first().innerText(),'Folder');
  assert.equal(await page.locator('#dCard .foot .copy').isVisible(),false);
  assert.deepEqual(await page.locator('#dActs .btn').allInnerTexts(),['Copy resume cmd']);
  await page.keyboard.press('Escape');assert.equal(await page.locator('#detail').isVisible(),false);
  await page.click('.grid .card[data-key="codex:two"] .status');assert.equal(await page.locator('#detail').isVisible(),false);
  await page.click('.grid .card[data-key="codex:two"] [data-detail]');
  await page.waitForFunction(()=>document.getElementById('dPrompt').textContent==='Line one\nLine two');
  assert.equal(await page.locator('#dCard .card[data-s="waiting"]').count(),1);
  assert.equal(await page.locator('#dReplyLabel').innerText(),'Reply');
  assert.equal(await page.locator('#dReply.none').innerText(),'Nothing yet');
  await page.mouse.click(5,5);assert.equal(await page.locator('#detail').isVisible(),false);
  await page.click('.grid .card[data-key="claude:one"] [data-detail]');
  assert.deepEqual(await page.locator('#dActs .btn').allInnerTexts(),['Copy resume cmd','Copy title','Open in VS Code']);
  assert.equal(await page.locator('#dActs a.btn').getAttribute('href'),'vscode://anthropic.claude-code/open?session=one');
  assert.deepEqual(await page.locator('#dActs button').evaluateAll(bs=>bs.map(b=>b.type)),['button','button']);
  await page.click('#detail .d-actions > [data-t=close]');assert.equal(await page.locator('#detail').isVisible(),false);
  await page.unroute('**/api/session?*');
  check('clicking a person pins its card; Details on that card or a list card opens a dialog with the full text and the card actions, for busy, waiting and thinking agents alike');
  assert.equal(await page.locator('header > .controls > #settingsMenu + #refreshOffice').innerText(),'Refresh');
  const crew=await page.evaluate(()=>{window.__oldOffice=office;return [...office.people.keys()].sort()});
  await page.click('#refreshOffice');
  await page.waitForFunction(()=>office!==window.__oldOffice&&office.framesShown>4);
  assert.deepEqual(await page.evaluate(()=>[...office.people.keys()].sort()),crew);
  assert.equal(await page.evaluate(()=>state.view),'office');
  check('Refresh, right after Settings, rebuilds only the office scene with the same people');
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
  await page.evaluate(()=>officeOpts.onHover(state.data.sessions[0],{x:40,y:40},false));
  assert.deepEqual((await page.locator('#tip .alert').allInnerTexts()).map(t=>t.replace(/\s+/g,' ')),['ctx 90% Near auto-compact','75m Long turn']);
  assert.equal(await page.locator('#tip .meta dd').nth(3).innerText(),'90% (330k / 367k)');
  await page.keyboard.press('Escape');
  assert.deepEqual((await page.locator('.grid .card .alert').allInnerTexts()).map(t=>t.replace(/\s+/g,' ')),['ctx 90% Near auto-compact','75m Long turn']);
  assert.match(await page.locator('.grid .card .time').innerText(),/^≥ 1h 15m/);
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
  await page.waitForFunction(()=>office.tvPicture?.visible&&office.tvVersion>=0,{},{timeout:10000});
  const tv3=await page.evaluate(()=>office.tvVersion);
  await page.waitForFunction(v=>office.tvVersion>v,tv3,{timeout:5000});
  check('3D shows the TV picture and keeps it moving');
  assert.deepEqual(await page.evaluate(()=>[!!office.boss3d,Object.keys(office.boardTex).sort(),office.smoke.sprites.length]),[true,['meet','sales'],10]);
  assert.ok(await page.evaluate(()=>{const s=office.boss3d.bone.torso.scale;return s.x>1.2&&s.x<2&&s.z>1.2&&s.z<2}));
  await page.waitForFunction(()=>office.hit.some(h=>h.key==='zone:boss'),{},{timeout:10000});
  check('3D has the plump boss, both whiteboard textures and the fridge puffs');
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
  // The 3D zoom range comes from config.go through /api/config; the page itself never zooms.
  const range=await page.evaluate(()=>fetch('/api/config').then(r=>r.json()).then(c=>[c.office3d.minZoom,c.office3d.maxZoom]));
  assert.deepEqual(await page.evaluate(()=>[office.controls.minZoom,office.controls.maxZoom]),range);
  for(const dy of [-5000,-5000,5000,5000]){
    await page.keyboard.down('Control');await page.mouse.wheel(0,dy);await page.keyboard.up('Control');await page.waitForTimeout(300);
    const z=(await cam()).zoom;assert.ok(z>=range[0]-1e-6&&z<=range[1]+1e-6,`zoom ${z} outside ${range}`);
  }
  await page.evaluate(()=>office.fitCamera3D(true));
  assert.deepEqual(await page.evaluate(()=>{
    const wheel=new WheelEvent('wheel',{deltaY:-100,ctrlKey:true,bubbles:true,cancelable:true});document.body.dispatchEvent(wheel);
    const key=new KeyboardEvent('keydown',{key:'=',ctrlKey:true,bubbles:true,cancelable:true});document.body.dispatchEvent(key);
    return [wheel.defaultPrevented,key.defaultPrevented,/user-scalable=no/.test(document.querySelector('meta[name=viewport]').content)];
  }),[true,true,true]);
  check('3D zoom stays within config.go\'s range and the page itself never zooms');
  // Panning stops at the room's edges: scrolling further moves nothing, and the view stays within the room's bounds.
  const inRoom=()=>page.evaluate(()=>{const c=office.camera,b=office.roomInView(),hw=(c.right-c.left)/2/c.zoom,hh=(c.top-c.bottom)/2/c.zoom;
    const ok=(lo,hi,h)=>0>=Math.min(lo+h,hi-h)-1e-3&&0<=Math.max(lo+h,hi-h)+1e-3;return ok(b.minX,b.maxX,hw)&&ok(b.minY,b.maxY,hh)});
  for(const [dx,dy] of [[0,20000],[0,-20000],[20000,0],[-20000,0]]){
    await page.mouse.wheel(dx,dy);await page.waitForTimeout(150);cam0=await cam();
    await page.mouse.wheel(dx,dy);await page.waitForTimeout(150);cam1=await cam();
    assert.deepEqual([cam1.x,cam1.y,cam1.z].map(v=>v.toFixed(3)),[cam0.x,cam0.y,cam0.z].map(v=>v.toFixed(3)));
    assert.ok(await inRoom());
  }
  await page.evaluate(()=>office.fitCamera3D(true));
  check('3D: panning stops at the room edges');
  // Settings → Lock zoom holds the zoom but still pans; Lock view goes to zoom 1 at the default angle and the wheel scrolls the page.
  const lockSetting=async(id,on)=>{await openSettings(page);await page.setChecked('#'+id,on);await page.keyboard.press('Escape');await page.mouse.move(640,520);};
  await lockSetting('lockZoom',true);
  cam0=await cam();await page.keyboard.down('Control');await page.mouse.wheel(0,-400);await page.keyboard.up('Control');await page.waitForTimeout(300);
  assert.equal((await cam()).zoom,cam0.zoom);
  await page.mouse.wheel(0,200);await page.waitForTimeout(150);cam1=await cam();
  assert.notDeepEqual([cam1.x,cam1.y,cam1.z],[cam0.x,cam0.y,cam0.z]);
  await lockSetting('lockZoom',false);
  await lockSetting('lockView',true);
  assert.equal(await page.isDisabled('#lockZoom'),true);
  cam0=await cam();assert.equal(cam0.zoom,1);
  assert.equal(await page.evaluate(()=>office.controls.enabled),false);
  await page.evaluate(()=>office.fitCamera3D(true));
  assert.deepEqual(Object.values(await cam()).map(v=>v.toFixed(3)),Object.values(cam0).map(v=>v.toFixed(3)));
  await page.mouse.wheel(0,200);await page.keyboard.down('ArrowRight');await page.waitForTimeout(300);await page.keyboard.up('ArrowRight');
  assert.deepEqual(await cam(),cam0);
  assert.equal(await page.evaluate(()=>{const w=new WheelEvent('wheel',{deltaY:100,bubbles:true,cancelable:true});office.canvas.dispatchEvent(w);return w.defaultPrevented}),false);
  assert.equal(await page.evaluate(()=>[office.canvas.style.touchAction,JSON.parse(localStorage.getItem('wmad.lockView'))].join()),'pan-y,true');
  await page.evaluate(()=>scrollTo(0,0));
  await lockSetting('lockView',false);
  assert.equal(await page.evaluate(()=>office.controls.enabled&&office.canvas.style.touchAction==='none'),true);
  await page.evaluate(()=>office.fitCamera3D(true));
  check('Settings: Lock zoom holds the zoom, Lock view shows the whole room at the default angle and lets the wheel scroll the page');
  // Grow past the desks and back twice; the first round also uploads the shared textures of models not yet on screen.
  const actors0=await page.evaluate(()=>office.actors.size);
  const textures=[];
  for(let round=0;round<2;round++){
   for(const extra of [16,0]){
    const f=await page.evaluate(n=>{office.update([...visible(),...Array.from({length:n},(_,i)=>({key:'w:'+i,id:'w'+i,agent:'claude',status:'error',platform:'cli',project:'p'+i}))]);return office.framesShown},extra);
    await page.waitForFunction(f=>office.framesShown>f+10,f);
   }
   assert.equal(await page.evaluate(()=>office.plan.desks.length),16);
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
  assert.equal(await page.locator('#officeWrap').isVisible(),false);
  assert.ok(await page.locator('#main section .card').count()>0);
  assert.equal(await frame.locator('#viewSeg').isVisible(),true);
  assert.equal(await frame.evaluate(()=>state.quality),'high');
  assert.equal(await frame.locator('#pipBtn').isVisible(),false);
  assert.equal(await frame.locator('#qualSeg').isVisible(),false);
  await pip.setViewportSize({width:480,height:400});
  await frame.waitForFunction(()=>!document.getElementById('office').classList.contains('loading'));await page.waitForTimeout(400);await screenshot(pip, 'pip-3d');
  assert.equal(await frame.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  check('real Document PiP opens 3D at compact size, keeps its office/list switch, and leaves the main page with just the list');
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
  await page.waitForFunction(()=>{const e=document.getElementById('pipError');return e.hidden&&!e.textContent},null,{timeout:9000});
  check('the pop-out error collapses on its own after 7 seconds');
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
  await fallback.evaluate(()=>{state.view='list';syncControls()});assert.equal(await fallback.locator('#pipBtn').isVisible(),true);
  check('the main page always shows the office, so the video fallback stays offered whatever view the PiP last picked');
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
  // New chat: the button opens a dialog with the agents the server found; a bad folder is refused; the demo never starts a real agent; Settings → Terminal is saved.
  const launch=await page.evaluate(()=>fetch('/api/launch').then(r=>r.json()));
  if(launch.supported&&launch.agents.length){
   await page.click('#launchBtn');
   assert.equal(await page.locator('#launchDlg').isVisible(),true);
   assert.deepEqual(await page.$$eval('#launchAgent option',o=>o.map(x=>x.value)),launch.agents);
   const cwds=await page.$$eval('#launchCwds option',o=>o.map(x=>x.value));
   assert.ok(cwds.includes(await page.inputValue('#launchCwd')));
   // Model is a menu: the CLI's default, then what the server detected; "?" says where the list comes from; an unlisted model runs with the default.
   const agent=await page.inputValue('#launchAgent');
   const models=await page.evaluate(a=>fetch('/api/launch/models?agent='+a).then(r=>r.json()).then(d=>d.models),agent);
   await page.waitForFunction(n=>document.querySelectorAll('#launchModel option').length===n+1,models.length);
   assert.deepEqual(await page.$$eval('#launchModel option',o=>o.map(x=>x.value)),['',...models]);
   assert.ok((await page.getAttribute('#launchModelHelp','data-tip')).length>10);
   const unlisted=await page.evaluate(a=>fetch('/api/launch',{method:'POST',headers:{'x-whatsmyagentdoing':'launch'},body:JSON.stringify({agent:a,model:'not-a-listed-model',cwd:'/',prompt:'x'})}).then(r=>r.json()),agent);
   assert.deepEqual([unlisted.ok,unlisted.model],[true,'']);
   // Non-interactive is remembered and changes the hint.
   const hint=await page.locator('#launchHint').innerText();
   await page.check('#launchPrint');
   assert.notEqual(await page.locator('#launchHint').innerText(),hint);
   assert.equal(await page.evaluate(()=>JSON.parse(localStorage.getItem('wmad.launchPrint'))),true);
   await page.uncheck('#launchPrint');
   await page.fill('#launchPrompt','hello');
   await page.fill('#launchCwd','/definitely/missing');await page.click('#launchSend');
   await page.waitForSelector('#launchErr:not([hidden])');
   assert.match(await page.locator('#launchErr').innerText(),/missing/);
   await page.fill('#launchCwd','/');await page.click('#launchSend');
   await page.waitForFunction(()=>!document.getElementById('launchDlg').open);
   assert.equal(await page.inputValue('#launchPrompt'),'');
   await openSettings(page);
   const terms=await page.$$eval('#termSel option',o=>o.map(x=>x.value));
   assert.equal(terms[0],'default');
   await page.selectOption('#termSel',terms.at(-1));
   assert.equal(await page.evaluate(()=>JSON.parse(localStorage.getItem('wmad.terminal'))),terms.at(-1));
   await page.selectOption('#termSel','default');
   await page.locator('#settingsMenu > summary').press('Escape');
   check('New chat: agents and detected models as menus with a "?" note, unlisted models fall back to the default, non-interactive is saved, a missing folder is refused, the demo starts nothing, and Settings → Terminal is saved');
  } else {
   assert.equal(await page.locator('#launchBtn').isVisible(),false);
   check('New chat stays hidden where the server cannot start agents');
  }
  assert.deepEqual(errors,[]);check('no uncaught browser errors');
  console.log('TOTAL',checks.length);
 } finally {
  await browser?.close();
  server.kill();
 }
})().catch(e=>{console.error(e);process.exitCode=1});
