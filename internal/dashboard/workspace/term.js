// ---- 터미널 패널 (#444): a snapshot then output over the terminal socket; text reaches the page only through the terminal renderer ----
// Moved out of dashboard.go. Each terminal panel is one termMake object with
// its own pane, socket and renderer, so several stay open side by side. The
// view and input rules, the ticket and the resize frames are those of the
// one screen it replaces. Runs on the page's globals (node, setMsg, apiSend,
// termAssets, termState).
let termLib=null;
function loadTermLib(){
  if(!termLib)termLib=loadAssets(termAssets).then(()=>{if(typeof Terminal!=='function')throw Error('불러오기 실패')}).catch(e=>{termLib=null;throw e});
  return termLib;
}
// termMake(id) builds one pane: a bar (state, connection, 입력, 넓히기), a
// message line and the screen. onWide is set by the workspace.
function termMake(id){
  const t={id:id,el:node('div','','termpane'),ws:null,term:null,fit:null,ro:null,timer:0,fitTimer:0,tries:0,closed:false,mode:'view',onWide:null};
  const bar=node('div','','toolbar termbar');
  t.stateEl=node('span','','muted');
  t.connEl=node('span','','muted');t.connEl.setAttribute('role','status');t.connEl.setAttribute('aria-live','polite');
  t.modeEl=node('button','입력','t-mode');t.modeEl.type='button';t.modeEl.setAttribute('aria-pressed','false');
  t.wideEl=node('button','넓히기','t-wide');t.wideEl.type='button';t.wideEl.setAttribute('aria-pressed','false');t.wideEl.title='Esc: 원래 크기';
  bar.append(t.stateEl,t.connEl,t.modeEl,t.wideEl);
  t.msgEl=node('div','','msg');t.msgEl.setAttribute('role','status');t.msgEl.setAttribute('aria-live','polite');
  t.screen=node('div','','termscreen');
  t.el.append(bar,t.msgEl,t.screen);
  t.modeEl.onclick=()=>termToggle(t);
  t.wideEl.onclick=()=>{if(t.onWide)t.onWide()};
  // Esc in input mode is the terminal's, never the page's.
  t.screen.addEventListener('keydown',e=>{if(e.key==='Escape'&&t.mode==='input')e.preventDefault()});
  return t;
}
// termWideShow marks 넓히기 on or off.
function termWideShow(t,on){t.wideEl.setAttribute('aria-pressed',String(on));t.wideEl.textContent=on?'원래 크기':'넓히기'}
// termSend sends one client frame on an open input socket.
function termSend(t,m){if(t.mode==='input'&&t.ws&&t.ws.readyState===1)t.ws.send(JSON.stringify(m))}
// termInput sends typed or pasted text, split under the host's 64 KiB frame limit.
const termEnc=new TextEncoder();
function termInput(t,d){
  if(t.mode!=='input')return;
  let chunk='',n=0;
  for(const ch of d){
    const b=termEnc.encode(ch).length;
    if(n+b>32768){termSend(t,{type:'input',data:chunk});chunk='';n=0}
    chunk+=ch;n+=b;
  }
  if(chunk)termSend(t,{type:'input',data:chunk});
}
// termFit fits the screen to its box: an input terminal takes the box's
// size (the host gets it); a view terminal keeps the host's size and scales
// its font to the box's width.
function termFit(t,always){
  if(t.closed||!t.term)return;
  if(t.mode!=='input'){termScale(t);return}
  if(!t.screen.clientWidth||!t.screen.clientHeight)return;
  const c=t.term.cols,r=t.term.rows;
  if(t.fit)try{t.fit.fit()}catch(e){}
  if(always&&t.term.cols===c&&t.term.rows===r)termSend(t,{type:'resize',cols:t.term.cols,rows:t.term.rows});
}
// termMinFont is the smallest font a view terminal scales down to (#253): 5px
// where the box is wide, 11px on a phone, where the box then scrolls
// sideways inside itself instead of shrinking 120 columns into unreadable.
function termMinFont(){return window.matchMedia&&window.matchMedia('(max-width:600px)').matches?11:5}
function termScale(t){
  if(t.closed||!t.term||t.mode==='input'||!t.screen.clientWidth)return;
  let d;try{d=t.fit&&t.fit.proposeDimensions()}catch(e){}
  if(!d||!(d.cols>0))return;
  const f=t.term.options.fontSize,want=Math.max(termMinFont(),Math.min(15,Math.floor(f*d.cols/t.term.cols*4)/4));
  if(want<f||want>=f+0.5)t.term.options.fontSize=want;
}
function termModeShow(t){
  const typing=t.mode==='input';
  t.modeEl.setAttribute('aria-pressed',String(typing));
  t.el.classList.toggle('typing',typing);
  if(t.term)t.term.options.disableStdin=!typing;
}
// termToggle switches 보기 ⇄ 입력: a new ticket and socket in the other mode.
function termToggle(t){
  if(!t.term)return;
  t.mode=t.mode==='input'?'view':'input';termModeShow(t);
  if(t.mode==='input')t.term.options.fontSize=15;
  const ws=t.ws;t.ws=null;if(ws)ws.close();
  t.tries=0;clearTimeout(t.timer);termConnect(t);
  if(t.mode==='input')t.term.focus();
}
const termConnText={connecting:'연결 중',open:'연결됨',retry:'다시 연결 중',closed:'끊김'};
function termConnSet(t,s){t.connEl.textContent=termConnText[s]||s}
// termOpen starts the renderer and the socket for record v ({id} is enough;
// the host's size comes with hello).
async function termOpen(t,v){
  termModeShow(t);t.stateEl.textContent='';setMsg(t.msgEl,'');
  termConnSet(t,'connecting');
  try{await loadTermLib()}catch(e){termConnSet(t,'closed');setMsg(t.msgEl,e.message,'err');return}
  if(t.closed)return;
  t.term=new Terminal({cols:v.cols||120,rows:v.rows||40,disableStdin:true,scrollback:5000});
  if(typeof FitAddon==='object'&&FitAddon.FitAddon){t.fit=new FitAddon.FitAddon();t.term.loadAddon(t.fit)}
  // Esc in view mode is the page's (넓히기, then the panel); in input mode the terminal's.
  t.term.attachCustomKeyEventHandler(e=>!(e.key==='Escape'&&t.mode!=='input'));
  t.term.open(t.screen);
  t.term.onData(d=>termInput(t,d));
  t.term.onResize(s=>{termSend(t,{type:'resize',cols:s.cols,rows:s.rows});if(t.mode!=='input')setTimeout(()=>termScale(t),0)});
  if(typeof ResizeObserver==='function'){
    t.ro=new ResizeObserver(()=>{clearTimeout(t.fitTimer);t.fitTimer=setTimeout(()=>termFit(t),150)});
    t.ro.observe(t.screen);
  }
  termScale(t);
  termConnect(t);
}
async function termConnect(t){
  if(t.closed)return;
  termConnSet(t,t.tries?'retry':'connecting');
  let tk;
  try{tk=await apiSend('POST','terminals/'+encodeURIComponent(t.id)+'/ticket',{mode:t.mode})}
  catch(e){
    if(e.status===404||e.status===401){termConnSet(t,'closed');setMsg(t.msgEl,e.message,'err');return}
    termRetry(t);return;
  }
  if(t.closed)return;
  const ws=new WebSocket((location.protocol==='https:'?'wss://':'ws://')+location.host+'/api/admin/terminals/'+encodeURIComponent(t.id)+'/socket?ticket='+encodeURIComponent(tk.ticket));
  ws.binaryType='arraybuffer';t.ws=ws;
  ws.onmessage=ev=>{
    if(t.closed||t.ws!==ws)return;
    if(typeof ev.data==='string'){
      let m;try{m=JSON.parse(ev.data)}catch(e){return}
      if(m.type==='hello'){
        t.tries=0;termConnSet(t,'open');
        if(t.mode==='input')termFit(t,true);
        else if(m.cols>0&&m.rows>0&&(m.cols!==t.term.cols||m.rows!==t.term.rows))t.term.resize(m.cols,m.rows);
      }
      if(m.type==='size'&&t.mode!=='input'&&m.cols>0&&m.rows>0)t.term.resize(m.cols,m.rows);
      if(m.type==='refused')setMsg(t.msgEl,m.error||'거부됨','err');
      if(m.type==='hello'||m.type==='state')t.stateEl.textContent=termState[m.state]||m.state;
      return;
    }
    t.term.write(new Uint8Array(ev.data));
  };
  ws.onclose=()=>{if(t.ws===ws&&!t.closed)termRetry(t)};
}
function termRetry(t){
  if(t.closed)return;
  t.tries++;termConnSet(t,'retry');
  clearTimeout(t.timer);t.timer=setTimeout(()=>termConnect(t),Math.min(30000,500*2**Math.min(t.tries,6)));
}
// termStop ends the panel's terminal for good (the panel closed).
function termStop(t){
  if(t.closed)return;
  t.closed=true;clearTimeout(t.timer);clearTimeout(t.fitTimer);
  if(t.ro)t.ro.disconnect();
  if(t.ws)t.ws.close();
  if(t.term)t.term.dispose();
  t.screen.replaceChildren();
}
