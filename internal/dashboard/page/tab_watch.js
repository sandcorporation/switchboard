// ---- 설정 › 지켜보기 (#528) ----
// API watches: the rules (/api/admin/watches) as a settings group, each
// opened as #settings/watch:<id>/edit (watch:+ is a new rule). The form
// holds the rule; 시험 조회 fetches it once as written (no mail, nothing
// stored), 저장 adds or replaces it. A secret header's value is never shown:
// an empty value keeps the stored one.
const watchOps=[['eq','같다'],['ne','다르다'],['gt','크다'],['lt','작다'],['exists','있다'],['missing','없다'],['contains','포함한다']];
function watchRow(v){
  const st=!v.enabled?['끔','neutral']:!v.last_at?['대기','neutral']:v.failing?['실패','danger']:v.last_ok?[v.last_result||'됨',v.matched?'warning':'success']:[v.last_result||'실패','warning'];
  return setRow('watch:'+v.id,[v.name,v.to,'매 '+v.every_min+'분',v.last_at?shortWhen(v.last_at):'-',[cutText(st[0],24),st[1]]]);
}
function cutText(s,n){s=String(s);return s.length>n?s.slice(0,n-1)+'…':s}
async function findWatch(key){
  if(key==='+')return {fresh:true,name:'',to:'',url:'',headers:[],every_min:5,cond:{kind:'value',path:'',op:'eq',value:''},subject:'',urgent:false,window_min:10,fail_after:3,enabled:true};
  return (await api('watches')).find(x=>String(x.id)===key);
}
setHeads.watch=v=>({title:v.fresh?'새 지켜보기':v.name,badge:v.fresh?null:!v.enabled?['끔','neutral']:v.failing?['실패','danger']:['켬','success']});
// watchTests keeps the last test result per form across redraws.
const watchTests=new Map();
function watchInput(value,title,attrs){
  const i=document.createElement('input');i.value=value===undefined||value===null?'':String(value);i.title=title;i.setAttribute('aria-label',title);
  i.autocomplete='off';i.spellcheck=false;i.setAttribute('autocapitalize','off');
  for(const [k,x] of Object.entries(attrs||{}))i.setAttribute(k,x);
  return i;
}
function watchCheck(text,on,title){const l=node('label','','check'),i=document.createElement('input');i.type='checkbox';i.checked=on;i.title=title||text;l.title=title||text;l.append(i,' '+text);return [l,i]}
function watchHeaderRow(h,list){
  const r=node('div','','watch-hdr');
  const name=watchInput(h.name,'머리글 이름',{maxlength:'100',placeholder:'X-Api-Key'});
  const val=watchInput('',h.secret?(h.set?'새 값을 적으면 바꿈 · 비우면 그대로':'키 값'):'머리글 값',{maxlength:'4096'});
  if(h.secret){val.type='password';val.placeholder=h.unreadable?'다시 적어야 함':h.set?'등록됨':'키 값';if(h.unreadable)val.title='서버 키가 바뀌어 저장한 값을 열 수 없음 · 다시 적기';else if(h.set&&h.set_at)val.title+=' · 등록 '+shortWhen(h.set_at)}else val.value=h.value||'';
  const [sl,secret]=watchCheck('비밀',!!h.secret,'값을 비밀로 보관 · 다시 보이지 않음');
  secret.onchange=()=>{if(secret.checked){val.type='password'}else{val.type='text'}};
  const rm=node('button','×','fr-ib');rm.type='button';rm.title='이 머리글 빼기';rm.setAttribute('aria-label','머리글 빼기');rm.onclick=()=>r.remove();
  r.append(name,val,sl,rm);
  r.read=()=>({name:name.value.trim(),value:val.value,secret:secret.checked});
  list.append(r);
}
function watchForm(v){
  const key='watch:'+(v.fresh?'+':v.id),kept=setForms.get(key);
  if(kept&&kept.stamp===(v.updated_at||''))return kept.form;
  const f=node('form','','fld watch-form');
  const name=watchInput(v.name,'규칙 이름',{maxlength:'64',required:''});
  const to=watchInput(v.to,'메일 받을 역할',{placeholder:'team/boss',required:''});
  const url=watchInput(v.url,'GET 할 주소',{type:'url',placeholder:'https://api.example/v1/items',required:''});
  const every=watchInput(v.every_min,'몇 분마다 조회 (1~1440)',{type:'number',min:'1',max:'1440',step:'1',inputmode:'numeric',required:''});
  const hdrs=node('div','','watch-hdrs');
  for(const h of v.headers||[])watchHeaderRow(h,hdrs);
  const addH=node('button','+ 머리글');addH.type='button';addH.title='요청 머리글 추가 (인증 키는 비밀로)';addH.onclick=()=>watchHeaderRow({name:'',secret:false},hdrs);
  const hbox=node('div','','watch-hbox');hbox.append(hdrs,addH);
  const kind=document.createElement('select');kind.title='조건 종류';kind.setAttribute('aria-label','조건 종류');
  for(const [k,t] of [['value','값 조건'],['new','새 항목']]){const o=node('option',t);o.value=k;kind.append(o)}
  kind.value=v.cond.kind;
  const path=watchInput(v.cond.path,'응답 JSON 경로 (점으로 구분, 숫자는 배열 자리)',{placeholder:'data.count'});
  const op=document.createElement('select');op.title='비교';op.setAttribute('aria-label','비교');
  for(const [k,t] of watchOps){const o=node('option',t);o.value=k;op.append(o)}
  op.value=v.cond.op||'eq';
  const value=watchInput(v.cond.value,'비교할 값',{placeholder:'10'});
  const list=watchInput(v.cond.list,'목록(배열)의 경로 · 비우면 응답 전체',{placeholder:'results'});
  const ikey=watchInput(v.cond.key,'항목을 가르는 키의 경로',{placeholder:'id'});
  const subject=watchInput(v.subject,'메일 제목 틀 · {경로}에 값이 들어감 · 비우면 기본',{maxlength:'200',placeholder:'[지켜보기] {data.count}건'});
  const win=watchInput(v.window_min,'같은 제목을 메일 하나로 묶는 시간(분)',{type:'number',min:'1',max:'1440',step:'1',inputmode:'numeric'});
  const failAfter=watchInput(v.fail_after,'연달아 몇 번 실패하면 알릴지',{type:'number',min:'1',max:'100',step:'1',inputmode:'numeric'});
  const [ul,urgent]=watchCheck('긴급',!!v.urgent,'긴급 메일로 보냄');
  const [el,enabled]=watchCheck('켬',v.fresh||!!v.enabled,'끄면 조회하지 않음');
  const lab=(t,el)=>{const l=node('label',t);l.title=el.title;return [l,el]};
  const rows=[lab('이름',name),lab('받는 역할',to),lab('주소',url),[node('label','머리글'),hbox],lab('주기(분)',every),lab('조건',kind),
    lab('경로',path),lab('비교',op),lab('값',value),lab('목록 경로',list),lab('키 경로',ikey),
    lab('제목 틀',subject),lab('묶는 시간(분)',win),lab('실패 알림(번)',failAfter),[node('label',''),(()=>{const s=node('span','','watch-checks');s.append(ul,el);return s})()]];
  for(const [l,el] of rows)f.append(l,el);
  const vRows=[path,op,value],nRows=[list,ikey];
  const show=()=>{
    const isNew=kind.value==='new';
    for(const el of vRows){el.hidden=isNew;el.previousSibling.hidden=isNew}
    for(const el of nRows){el.hidden=!isNew;el.previousSibling.hidden=!isNew}
    const noVal=op.value==='exists'||op.value==='missing';
    value.hidden=isNew||noVal;value.previousSibling.hidden=isNew||noVal;
  };
  kind.onchange=show;op.onchange=show;show();
  const read=()=>({name:name.value.trim(),to:to.value.trim(),url:url.value.trim(),headers:[...hdrs.children].map(r=>r.read()).filter(h=>h.name),
    every_min:Number(every.value)||0,cond:kind.value==='new'?{kind:'new',list:list.value.trim(),key:ikey.value.trim()}:{kind:'value',path:path.value.trim(),op:op.value,value:value.value},
    subject:subject.value.trim(),urgent:urgent.checked,window_min:Number(win.value)||0,fail_after:Number(failAfter.value)||0,enabled:enabled.checked});
  const out=node('div','','watch-test');out.setAttribute('role','status');
  const drawTest=()=>{
    out.textContent='';const t=watchTests.get(key);if(!t)return;
    if(t.err){out.append(badge('실패','danger'),' '+t.err);return}
    const r=t.r,parts=[];
    if(r.status)parts.push('HTTP '+r.status);
    if(!r.ok)parts.push(r.error||'실패');
    else if(t.kind==='new')parts.push('항목 '+r.items+'개',v.fresh||!v.last_at?'처음 돌 때 기준으로 삼음':'새 항목 '+r.new+'개');
    else parts.push(r.matched?'조건 맞음':'조건 안 맞음','값 '+(r.value===undefined||r.value===''?'(없음)':r.value));
    out.append(badge(!r.ok?'실패':r.matched||r.new?'맞음':'됨',!r.ok?'danger':r.matched||r.new?'warning':'success'),' '+parts.join(' · '));
    if(r.ok&&r.subject){const s=node('div','제목: '+r.subject,'watch-subj');out.append(s)}
  };
  const test=node('button','시험 조회');test.type='button';test.title='지금 한 번 조회해 결과만 봄 · 메일 없음 · 저장 안 함';
  test.onclick=async()=>{
    const body=read();if(!v.fresh)body.id=v.id;
    test.disabled=true;
    try{watchTests.set(key,{r:await apiSend('POST','watches/test',body),kind:body.cond.kind})}
    catch(err){watchTests.set(key,{err:err.message})}
    test.disabled=false;drawTest();
  };
  const save=node('button',v.fresh?'추가':'저장');save.type='submit';save.title=v.fresh?'규칙 추가':'규칙 저장';
  const acts=node('span','','watch-acts');acts.append(test,save);
  f.append(node('span'),acts,node('span'),out);
  drawTest();
  f.onsubmit=async e=>{
    e.preventDefault();
    try{
      if(v.fresh){
        const w=await apiSend('POST','watches',read());
        setForms.delete(key);watchTests.delete(key);setNote('watch:'+w.id,w.name+' 추가','ok');loadSettingsView();openDetail('settings','watch:'+w.id,'edit');return;
      }
      await apiSend('PUT','watches/'+v.id,read());
      setForms.delete(key);setNote(key,'저장','ok');
    }catch(err){setNote(key,err.message,'err')}
    loadSettingsView();drawDetail();
  };
  if(!can('sessions')){acts.hidden=true;for(const el of f.elements)el.disabled=true}
  setForms.set(key,{stamp:v.updated_at||'',form:f});
  return f;
}
function drawWatch(v,box){
  const id='watch:'+(v.fresh?'+':v.id);
  if(!v.fresh&&can('sessions')){
    const acts=node('div','','detail-acts');
    acts.append(setButton(v.enabled?'끄기':'켜기','',async()=>{
      try{await apiSend('POST','watches/'+v.id+'/'+(v.enabled?'off':'on'));setForms.delete(id);setNote(id,v.enabled?'껐음':'켰음','ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
    },v.enabled?'조회를 멈춤':'다시 조회'),setButton('지우기','',async e=>{
      if(!await askOn(e.currentTarget,{text:v.name+' 규칙을 지울까요? 저장한 키도 지워집니다.'}))return;
      try{await apiSend('DELETE','watches/'+v.id);setForms.delete(id);setMsg($('settingsMsg'),v.name+' 규칙을 지웠습니다.','ok');closeDetail()}
      catch(e){setNote(id,e.message,'err');drawDetail()}
      loadSettingsView();
    },'규칙과 저장한 키를 지움'));
    box.append(acts);
  }
  drawSetNote(box,id);
  if(!v.fresh)box.append(dl([['마지막 조회',v.last_at?when(v.last_at):'-'],['결과',v.last_result||'-'],['연달아 실패',String(v.fails)],['본 항목',v.cond.kind==='new'?String(v.seen):'-']]));
  box.append(watchForm(v));
}
setDraws.watch=drawWatch;
