// Exercise the embedded UI with deterministic operator and guide responses.
import assert from 'node:assert/strict';
import {mkdir, readFile} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {join} from 'node:path';
import {createRequire} from 'node:module';
const require = createRequire(import.meta.url);
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({executablePath:process.env.CHROMIUM_PATH || undefined, headless:true});
const viewportSizes = [{width:1280,height:900},{width:375,height:812}];

async function run(viewport) {
  const page = await browser.newPage({viewport});
  const errors = [];
  page.on('pageerror',e=>errors.push(e.message));
  page.setDefaultTimeout(6000);
  const profile = {id:'primary',name:'London',market:'London',country:'GB',guide_location:'SW1A 1AA',enabled:true,
    preset_id:'uk-london',channel_count:2,tuner_url:'http://tv.test:39193'};
  const programmes = {now:{title:'Local news',start:1700000000,stop:1700003600},next:{title:'Sports desk',start:1700003600,stop:1700007200}};
  const candidate = {id:'station-a',station:'BBC One London',name:'BBC One London',source:'provider.example',...programmes};
  const channels = ['BBC One','BBC Two'].map((name,i)=>({name,number:String(i+1),profile_id:'primary',id:'channel-'+i,enabled:true,aliases:[],region:'GB'}));
  const roster = {channels:channels.map((ch,i)=>({channel_id:ch.id,count:i?1:3,providers:2,policy:{target:4}}))};
  const monitor = {playback:{viewers:0,outgoing_streams:[]},live:[],work:{},checker:{},database:{channels:{},coverage:{},queue:{},progress:{}}};
  const summary = {state:'conflict',origins:2,until:1700007200,...programmes};
  let pin = '',externalURL = '',receivedPortal = null;
  let setup = {state:'active',step:'plex',profile_id:'primary',has_sources:true};
  const mutate = (path,body)=>{
    switch(path) {
      case 'guide/pin': assert.equal(body.channel_id,channels[0].id); pin=body.candidate_id; return {};
      case 'guide/feed': externalURL=body.url; return {};
      case 'guide/refresh': return {};
      case 'input/portal': receivedPortal=body; return {account_id:5,channels:42};
      case 'setup': setup={...setup,...body}; return setup;
      default: throw Error('Unexpected POST /api/' + path);
    }
  };
  const read = path=>{
    switch(path) {
      case 'guide': return {feed_configured:!!externalURL,feed_host:externalURL?'epg.example':'',channels:{
        [channels[0].id]:{...summary,state:pin?'pinned':'conflict',pin},[channels[1].id]:{state:'missing'}}};
      case 'guide/channel': return {candidates:[candidate],...summary,pin};
      case 'guide/stations': return {candidates:[candidate]};
      case 'profiles': return [profile];
      case 'presets': case 'events': case 'roster/channel-0': case 'roster/channel-1': return [];
      case 'listeners': return {port_range:'39192-39198',tuner_ports:[39193,39194],available_slots:1};
      case 'channels': return channels;
      case 'monitor': return monitor;
      case 'roster': return roster;
      case 'setup': return setup;
      case 'settings': case 'removal-policy': case 'input/import': return {};
      case 'input/accounts':return {items:[],offset:0,limit:50,total:0,has_next:false};
      default: throw Error('Unexpected GET /api/' + path);
    }
  };
  await page.route('**/*',async route=>{
    try {
      const url=new URL(route.request().url()), path=url.pathname;
      if(path.startsWith('/api/')) {
        const key=path.slice(5);
        const data=route.request().method()==='POST'?mutate(key,route.request().postDataJSON()):read(key);
        return await route.fulfill({json:data});
      }
      const file = path === '/' ? 'index.html' : path.replace(/^\/admin\//,'');
      if(!/^(?:ui\/)?[\w.-]+$/.test(file)) throw Error('Invalid fixture asset');
      const ext=file.split('.').at(-1);
      const body=await readFile(new URL('../internal/admin/'+file,import.meta.url));
      await route.fulfill({body,contentType:{html:'text/html',css:'text/css',js:'text/javascript',png:'image/png'}[ext]});
    } catch(e) { errors.push(e.message); await route.fulfill({status:500,body:e.message}); }
  });
  await page.goto('http://antennarr.test/#status');
  const wizard = page.locator('#setup-guide');
  await wizard.getByRole('button',{name:'Connect to Jellyfin'}).click();
  assert.equal(await wizard.getByRole('textbox',{name:'XMLTV guide URL'}).inputValue(),'http://tv.test:39193/guide.xml');
  await wizard.getByRole('button',{name:'Connect to Plex'}).click();
  assert.equal(await wizard.getByRole('textbox',{name:'Tuner address',exact:true}).inputValue(),profile.tuner_url);
  await wizard.getByRole('button',{name:'Finish setup'}).click();
  await page.getByRole('link',{name:'Tuners',exact:true}).click();
  await page.locator('#tuner-directory a').filter({hasText:'London'}).click();
  const output=page.locator('#output');
  await output.getByRole('button',{name:'Connect to Jellyfin'}).click();
  assert.equal(await output.getByRole('textbox',{name:'M3U playlist URL'}).inputValue(),profile.tuner_url+'/playlist.m3u');
  const health=output.getByRole('combobox',{name:'Show channels'});
  await health.selectOption('missing');
  assert.deepEqual(await output.locator('.tuner-channels > details:visible strong').allTextContents(),['BBC Two']);
  await health.selectOption('conflict');
  const row = output.locator('[data-channel-id="channel-0"]');
  await row.locator(':scope > summary').focus();
  await page.keyboard.press('Enter');
  await row.getByText('Compare guide sources',{exact:true}).click();
  await row.getByRole('button',{name:/^Pin BBC One London/}).click();
  await health.selectOption('');
  await row.getByRole('button',{name:'Use automatic selection'}).click();
  await row.getByText('Automatic selection restored.').waitFor();
  const term=row.getByRole('searchbox',{name:'Find another guide station'});
  await term.fill('local');
  await term.focus();
  await page.evaluate(()=>import('/admin/ui/output.js').then(m=>m.refresh()));
  assert.equal(await term.inputValue(),'local');
  assert.equal(await term.evaluate(e=>document.activeElement===e),true);
  await row.getByRole('button',{name:'Find station',exact:true}).click();
  await row.locator('.guide-candidates').last().getByText('BBC One London',{exact:true}).waitFor();
  await output.getByText('Programme guide settings',{exact:true}).click();
  await output.getByLabel('External XMLTV URL').fill('https://epg.example/listings.xml');
  await output.getByRole('button',{name:'Save feed'}).click();
  await output.getByText('External feed · epg.example').waitFor();
  await output.getByRole('button',{name:'Remove feed'}).click();
  await output.getByText('External guide removed.').waitFor();
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.getByRole('link',{name:'Sources',exact:true}).click();
  await page.getByText('Add a Ministra portal account',{exact:true}).click();
  const form=page.locator('#portal-form');
  await form.getByLabel('Portal URL',{exact:true}).fill('https://portal.example');
  await form.getByLabel('Registered MAC address').fill('00:11:22:33:44:55');
  await form.getByLabel('Account playlist URL (optional)').fill('https://portal.example/my-list');
  await form.getByRole('button',{name:'Connect portal account'}).click();
  await form.getByText('Account saved · 42 channels imported.').waitFor();
  assert.equal(receivedPortal.mac,'00:11:22:33:44:55');
  await switchTuners(page,profile,channels,roster);
  assert.deepEqual(errors,[]);
  await page.close();
  console.log('PASS real browser UI ' + viewport.width + 'x' + viewport.height);
}
// Exercise both a failed selected tuner and a superseded request that finishes late.
async function switchTuners(page,first,channels,roster) {
  const second={...first,id:'other',name:'Second tuner',tuner_url:'http://tv.test:39194'};
  const scoped=channels.map(c=>({...c,id:'other-'+c.id,profile_id:second.id}));
  await page.route('**/api/profiles',r=>r.fulfill({json:[first,second]}));
  await page.route('**/api/channels?profile_id=other',r=>r.fulfill({json:scoped}));
  await page.route('**/api/roster?profile_id=other',r=>r.fulfill({json:{
    channels:roster.channels.map(r=>({...r,channel_id:'other-'+r.channel_id}))}}));
  let unavailable=true;
  await page.route('**/api/guide?profile_id=other',r=>unavailable
    ? r.fulfill({status:503,body:'Guide temporarily unavailable'}) : r.fulfill({json:{channels:{}}}));
  const go = hash=>page.evaluate(h=>{location.hash=h;},hash);
  const content=page.locator('#tuner-view');
  const playlist=content.getByRole('textbox',{name:'M3U playlist URL'});
  await go('output?tuner=primary&connect=jellyfin');
  await playlist.waitFor();
  assert.equal(await playlist.inputValue(),first.tuner_url+'/playlist.m3u');
  await go('output?tuner=other&connect=jellyfin');
  await page.getByText('Unable to load this lineup. Retrying shortly.').waitFor();
  assert.equal(await content.isVisible(),false);
  unavailable=false;
  await page.evaluate(()=>import('/admin/ui/output.js').then(m=>m.refresh()));
  await playlist.waitFor();
  assert.equal(await playlist.inputValue(),second.tuner_url+'/playlist.m3u');
  // Parameter-only navigation also changes the open client setup panel.
  await go('output?tuner=other&connect=plex');
  await content.getByRole('textbox',{name:'Tuner address',exact:true}).waitFor();
  await go('output?tuner=other&connect=jellyfin');
  await playlist.waitFor();

  let resume;
  const delayed=new Promise(resolve=>{resume=resolve;});
  await page.route('**/api/guide?profile_id=primary',async r=>{
    await delayed; await r.fulfill({json:{channels:{}}});
  });
  const waiting=page.waitForRequest('**/api/guide?profile_id=primary');
  const staleReply=page.waitForResponse('**/api/guide?profile_id=primary');
  await go('output?tuner=primary&connect=jellyfin');
  await waiting;
  try {
    await go('output?tuner=other&connect=jellyfin');
    await playlist.waitFor();
    assert.equal(await playlist.inputValue(),second.tuner_url+'/playlist.m3u');
  } finally { resume(); }
  await (await staleReply).finished();
  await page.evaluate(()=>import('/admin/ui/output.js').then(m=>m.refresh()));
  assert.equal(await playlist.inputValue(),second.tuner_url+'/playlist.m3u');
}

async function runReal(admin) {
  const context = await browser.newContext({viewport:viewportSizes[1],httpCredentials:{username:'admin',password:'admin'}});
  const page = await context.newPage();
  const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  page.on('response',r=>{if(r.url().startsWith(admin+'/api/') && r.status()>=400) errors.push(r.status()+' '+new URL(r.url()).pathname);});
  page.setDefaultTimeout(12000);
  try {
    await page.goto(admin+'/admin#status');
    const wizard=page.locator('#setup-guide');
    await wizard.locator('#setup-tuner').selectOption('preset:uk-london');
    await wizard.getByRole('button',{name:'Next',exact:true}).click();
    await wizard.getByRole('heading',{name:'Add a source'}).waitFor();
    // The unpopulated tuner is deliberate. A fresh installation should expose
    // its lineup and XMLTV channel IDs before an operator connects a provider.
    await wizard.getByRole('button',{name:'Skip for now'}).click();
    await page.getByRole('link',{name:'Tuners',exact:true}).click();
    await page.locator('#tuner-directory a').filter({hasText:'London'}).click();
    const tuner=page.locator('#output');
    await tuner.getByRole('button',{name:'Connect to Plex'}).click();
    const address=await tuner.getByRole('textbox',{name:'Tuner address',exact:true}).inputValue();
    assert.equal(new URL(address).hostname,'127.0.0.1');
    const discovery=await fetch(address+'/discover.json').then(async r=>{assert.equal(r.status,200);return r.json();});
    assert.equal(discovery.BaseURL,address);
    await tuner.getByRole('button',{name:'Connect to Jellyfin'}).click();
    const playlistURL=await tuner.getByRole('textbox',{name:'M3U playlist URL'}).inputValue();
    const guideURL=await tuner.getByRole('textbox',{name:'XMLTV guide URL'}).inputValue();
    const [playlist,guide]=await Promise.all([playlistURL,guideURL].map(async url=>{
      assert.equal(new URL(url).origin,address);
      const res=await fetch(url);assert.equal(res.status,200);return res.text();
    }));
    const ids=[...playlist.matchAll(/tvg-id="([^"]+)"/g)].map(m=>m[1]);
    assert.ok(ids.length>50,'London preset should contain a full lineup');
    assert.deepEqual(ids,[...guide.matchAll(/<channel id="([^"]+)">/g)].map(m=>m[1]));
    const health=tuner.getByRole('combobox',{name:'Show channels'});
    await health.selectOption('missing');
    assert.equal(await tuner.locator('.tuner-channels > details:visible').count(),ids.length);
    await health.selectOption('conflict');
    await tuner.getByText('No channels in this group.').waitFor();
    await health.selectOption('missing');
    const row=tuner.locator('.tuner-channels > details:visible').first();
    await row.locator(':scope > summary').click();
    await row.getByText('Compare guide sources',{exact:true}).click();
    await row.getByText('No current candidates.',{exact:false}).waitFor();
    await row.getByRole('searchbox',{name:'Find another guide station'}).fill('synthetic-station');
    await row.getByRole('button',{name:'Find station',exact:true}).click();
    await row.getByText('No matching guide stations.').waitFor();
    await tuner.getByText('Programme guide settings',{exact:true}).click();
    await tuner.getByRole('button',{name:'Refresh listings'}).click();
    await tuner.getByText('Guide refresh requested.').waitFor();
    await row.locator(':scope > summary').click();
    await tuner.getByText('Programme guide settings',{exact:true}).click();
    const imageDir=process.env.ANTENNARR_UI_SCREENSHOTS;
    if (imageDir) await mkdir(imageDir,{recursive:true});
    for(const size of viewportSizes) {
      await page.setViewportSize(size);
      await tuner.getByRole('button',{name:'Connect to Jellyfin'}).scrollIntoViewIfNeeded();
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
      if(imageDir) await page.screenshot({path:join(imageDir,'lineup-'+size.width+'.png')});
    }
    assert.deepEqual(errors,[]);
    console.log('PASS production server UI: wizard, client URLs, matching M3U/XMLTV channel IDs, guide actions, and responsive layout');
  } finally { await context.close(); }
}

const server = process.argv[2] === '--server' ? process.argv[3] : '';
try {
  if (server) await runReal(server);
  else for(const viewport of viewportSizes) await run(viewport);
} finally { await browser.close(); }
if (!server) {
  const runner=fileURLToPath(new URL('./alpha12/browser.py',import.meta.url));
  execFileSync('python3',['-B',runner,process.execPath],{stdio:'inherit'});
}
