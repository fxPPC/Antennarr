// Exercise the embedded UI with deterministic operator and guide responses.
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
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
  assert.deepEqual(errors,[]);
  await page.close();
  console.log('PASS real browser UI ' + viewport.width + 'x' + viewport.height);
}
try { for(const viewport of viewportSizes) await run(viewport); }
finally { await browser.close(); }
