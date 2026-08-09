let token = localStorage.getItem('token');
let currentUser = JSON.parse(localStorage.getItem('user') || 'null');
let authTab = 'login';
let draftProgram = null;
let lastError = '';

const STAGE_LABEL = {dean_review:'Dean', registrar_review:'Registrar', for_payment:'Cashier', enrolled:'Enrolled'};
const STAGE_ORDER = ['dean_review','registrar_review','for_payment','enrolled'];

async function api(path, method, body, isForm){
  const opts = { method: method || 'GET', headers: {} };
  if(token) opts.headers['Authorization'] = 'Bearer ' + token;
  if(body && !isForm){ opts.headers['Content-Type']='application/json'; opts.body = JSON.stringify(body); }
  if(body && isForm){ opts.body = body; }
  const res = await fetch('/api' + path, opts);
  let data = {};
  try{ data = await res.json(); }catch(e){}
  if(!res.ok){ throw new Error(data.error || 'Something went wrong. Please try again.'); }
  return data;
}

function el(html){ document.getElementById('app').innerHTML = html; }
function fmtMoney(n){ return '\u20b1' + Number(n).toLocaleString(undefined,{minimumFractionDigits:2, maximumFractionDigits:2}); }
function roleLabel(r){ return {student:'Student', dean:'Dean', registrar:'Registrar', cashier:'Cashier'}[r] || r; }

function saveSession(t,u){ token=t; currentUser=u; localStorage.setItem('token',t); localStorage.setItem('user', JSON.stringify(u)); }
function logout(){ token=null; currentUser=null; localStorage.removeItem('token'); localStorage.removeItem('user'); lastError=''; renderAuth(); }

function header(){
  return `
    <div class="masthead">
      <div>
        <h1>Registrar's Online Enrollment</h1>
        <div class="sub">Student &middot; Dean &middot; Registrar &middot; Cashier</div>
      </div>
      ${currentUser ? `
      <div class="session">
        ${currentUser.name} &mdash; logged in as <b>${roleLabel(currentUser.role)}</b><br/>
        <button onclick="logout()">Log out</button>
      </div>` : ''}
    </div>
  `;
}

function errorBox(){
  if(!lastError) return '';
  return `<div class="error-box">${lastError}</div>`;
}

/* ---------------- AUTH ---------------- */

function renderAuth(){
  el(`
    ${header()}
    <div class="card" style="max-width:440px;margin:30px auto 0;">
      <div class="tabs">
        <button class="${authTab==='login'?'active':''}" onclick="switchAuthTab('login')">Sign in</button>
        <button class="${authTab==='register'?'active':''}" onclick="switchAuthTab('register')">Create student account</button>
      </div>
      ${errorBox()}
      ${authTab === 'login' ? `
        <label>Email</label>
        <input type="email" id="loginEmail" placeholder="you@school.edu" />
        <label>Password</label>
        <input type="password" id="loginPassword" />
        <div class="row"><button class="btn" onclick="doLogin()">Sign in</button></div>
        <p class="hint" style="margin-top:16px;">Staff demo logins: dean@school.edu / registrar@school.edu / cashier@school.edu, password <code>password123</code>.</p>
      ` : `
        <label>Full name</label>
        <input type="text" id="regName" />
        <label>Email</label>
        <input type="email" id="regEmail" />
        <label>Password</label>
        <input type="password" id="regPassword" placeholder="At least 6 characters" />
        <div class="row"><button class="btn" onclick="doRegister()">Create account</button></div>
      `}
    </div>
  `);
}
function switchAuthTab(t){ authTab=t; lastError=''; renderAuth(); }

async function doLogin(){
  const email = document.getElementById('loginEmail').value.trim();
  const password = document.getElementById('loginPassword').value;
  try{
    const data = await api('/login','POST',{email,password});
    saveSession(data.token, data.user);
    lastError=''; boot();
  }catch(e){ lastError = e.message; renderAuth(); }
}
async function doRegister(){
  const name = document.getElementById('regName').value.trim();
  const email = document.getElementById('regEmail').value.trim();
  const password = document.getElementById('regPassword').value;
  try{
    const data = await api('/register','POST',{name,email,password});
    saveSession(data.token, data.user);
    lastError=''; boot();
  }catch(e){ lastError = e.message; renderAuth(); }
}

/* ---------------- STUDENT ---------------- */

function trackerHTML(status){
  let idx = STAGE_ORDER.indexOf(status);
  let rejected = false, rejectedAt = -1;
  if(status === 'dean_returned'){ rejected = true; rejectedAt = 0; idx = 0; }
  if(status === 'registrar_returned'){ rejected = true; rejectedAt = 1; idx = 1; }
  return `<div class="tracker">${STAGE_ORDER.map((s,i)=>{
    let cls = '';
    if(rejected && i===rejectedAt) cls='rejected';
    else if(i < idx) cls='done';
    else if(i === idx) cls = status==='enrolled' ? 'done' : 'now';
    return `<div class="tstep ${cls}"><div class="dot"></div>${STAGE_LABEL[s]}</div>`;
  }).join('')}</div>`;
}

async function renderStudent(){
  let data;
  try{ data = await api('/enrollment/mine'); }catch(e){ lastError=e.message; renderAuth(); return; }
  const app = data.application;
  if(!app || app.status === 'enrolled'){
    if(app && app.status === 'enrolled'){ renderSlip(app); return; }
    renderProfileAndPicker();
    return;
  }
  renderStudentStatus(app);
}

async function renderProfileAndPicker(){
  let me;
  try{ me = await api('/me'); }catch(e){ lastError=e.message; renderAuth(); return; }
  let programs;
  try{ programs = await api('/programs'); }catch(e){ programs = []; }
  const p = me.profile || {};
  el(`
    ${header()}
    <span class="roleTag">Student</span>
    ${errorBox()}
    <div class="card">
      <h2>Personal information</h2>
      <p class="hint">Keep this up to date before submitting an enrollment application.</p>
      <div class="grid2">
        <div><label>Address</label><input type="text" id="pAddress" value="${p.address||''}"/></div>
        <div><label>Contact number</label><input type="text" id="pContact" value="${p.contact_number||''}"/></div>
        <div><label>Birthdate</label><input type="date" id="pBirth" value="${p.birthdate||''}"/></div>
        <div><label>Previous school</label><input type="text" id="pSchool" value="${p.previous_school||''}"/></div>
      </div>
      <label>Previous program / units earned (if any)</label>
      <input type="text" id="pPrevProg" value="${p.previous_program||''}" placeholder="e.g. BS Education, 60 units earned"/>
      <div class="row"><button class="btn ghost" onclick="saveProfile()">Save information</button></div>
    </div>
    <div class="card">
      <h2>Select program</h2>
      <p class="hint">Choose the program you're enrolling into. Subjects are assigned by the Dean after evaluation &mdash; you won't pick them yourself.</p>
      <div class="progs">
        ${programs.map(pr => `
          <div class="prog ${draftProgram===pr.id?'sel':''}" onclick="pickProgram(${pr.id})">
            <div class="code">${pr.code}</div>
            <h3>${pr.name}</h3>
          </div>
        `).join('')}
      </div>
      <div class="row">
        <button class="btn" ${draftProgram?'':'disabled'} onclick="submitEnrollment()">Submit enrollment application</button>
      </div>
    </div>
  `);
}
function pickProgram(id){ draftProgram = id; renderProfileAndPicker(); }
async function saveProfile(){
  try{
    await api('/me','PUT',{
      birthdate: document.getElementById('pBirth').value,
      address: document.getElementById('pAddress').value,
      contact_number: document.getElementById('pContact').value,
      previous_school: document.getElementById('pSchool').value,
      previous_program: document.getElementById('pPrevProg').value,
    });
    lastError=''; renderProfileAndPicker();
  }catch(e){ lastError = e.message; renderProfileAndPicker(); }
}
async function submitEnrollment(){
  try{
    await api('/enrollment','POST',{program_id: draftProgram});
    draftProgram = null; lastError=''; renderStudent();
  }catch(e){ lastError = e.message; renderProfileAndPicker(); }
}

function renderStudentStatus(app){
  const rejected = app.status.endsWith('_returned');
  const waitingCashier = app.status === 'for_payment';
  el(`
    ${header()}
    <span class="roleTag">Student</span>
    ${errorBox()}
    <div class="card">
      <h2>${app.program.name}</h2>
      <p class="hint">Reference ENR-${String(app.id).padStart(5,'0')} &middot; submitted ${app.created_at}</p>
      ${trackerHTML(app.status)}
      ${rejected ? `
        <div class="stamp-wrap"><div class="stamp no">Returned</div></div>
        <p class="hint" style="text-align:center;">${app.return_reason || 'Your application was returned for correction.'}</p>
        <div class="row" style="justify-content:center;">
          <button class="btn" onclick="resubmit(${app.id})">Resubmit application</button>
        </div>
      ` : ''}
      ${waitingCashier ? `<p class="hint" style="text-align:center;">Cleared by the Dean and Registrar. Payment is pending at the Cashier's window.</p>` : ''}
      ${app.dean_remarks ? `<p class="hint"><b>Dean's remarks:</b> ${app.dean_remarks}</p>` : ''}
    </div>

    <div class="card">
      <h2>Suggested subjects</h2>
      <p class="hint">These are assigned by the Dean based on your program and academic record.</p>
      ${app.subjects.length === 0 ? `<div class="empty">Not yet assigned &mdash; your application is with the Dean.</div>` : `
        <div class="subjlist">
          ${app.subjects.map(s => `<div class="subjrow"><span class="scode">${s.code}</span>${s.title}<span class="sunits">${s.units} units</span></div>`).join('')}
        </div>
        <p class="hint" style="margin-top:10px;">Total: ${app.total_units} units</p>
      `}
    </div>

    ${app.payment ? `
    <div class="card">
      <h2>Assessment / payment</h2>
      <table class="feetable">
        <tr><td>Tuition (${app.payment.total_units} units)</td><td class="r">${fmtMoney(app.payment.tuition_fee)}</td></tr>
        <tr><td>Miscellaneous &amp; other fees</td><td class="r">${fmtMoney(app.payment.misc_fee)}</td></tr>
        <tr class="total"><td>Total assessment</td><td class="r">${fmtMoney(app.payment.total_amount)}</td></tr>
      </table>
      <p class="hint" style="margin-top:10px;">Payment status:
        <span class="badge ${app.payment.status==='confirmed'?'ok':'wait'}">${app.payment.status==='confirmed'?'Confirmed':'Pending at Cashier'}</span>
      </p>
    </div>
    ` : ''}

    <div class="card">
      <h2>Required documents</h2>
      <div class="doclist">
        ${app.documents.length===0 ? `<div class="empty">No documents uploaded yet.</div>` :
          app.documents.map(d=>`<div class="docitem"><span>${d.name}</span><span>${d.uploaded_at}</span></div>`).join('')}
      </div>
      <div class="filerow">
        <input type="file" id="docFile" />
        <button class="btn small ghost" onclick="uploadDoc(${app.id})">Upload</button>
      </div>
    </div>

    <div class="card">
      <h2>Application history</h2>
      <div class="history">
        ${app.history.map(h => `<div class="hitem"><b>${h.actor_role[0].toUpperCase()+h.actor_role.slice(1)}:</b> ${h.action} &mdash; <i>${h.actor_name}</i>, ${h.at}${h.note?` <span style="color:var(--red)">(${h.note})</span>`:''}</div>`).join('')}
      </div>
    </div>
  `);
}

async function resubmit(id){
  try{ await api(`/enrollment/${id}/resubmit`,'POST',{}); lastError=''; renderStudent(); }
  catch(e){ lastError=e.message; renderStudent(); }
}
async function uploadDoc(appId){
  const f = document.getElementById('docFile').files[0];
  if(!f){ lastError='Choose a file first.'; renderStudent(); return; }
  const form = new FormData(); form.append('file', f);
  try{ await api(`/enrollment/${appId}/documents`,'POST', form, true); lastError=''; renderStudent(); }
  catch(e){ lastError=e.message; renderStudent(); }
}

function renderSlip(app){
  el(`
    ${header()}
    <span class="roleTag">Student</span>
    <div class="card">
      ${trackerHTML('enrolled')}
      <div class="stamp-wrap"><div class="stamp ok">Enrolled</div></div>
    </div>
    <div class="slip">
      <h2>Enrollment slip</h2>
      <div class="sid">ENR-${String(app.id).padStart(5,'0')}</div>
      <table>
        <tr><td class="k">Student</td><td>${app.student.name}</td></tr>
        <tr><td class="k">Program</td><td>${app.program.name} (${app.program.code})</td></tr>
        <tr><td class="k">Subjects</td><td>${app.subjects.map(s=>s.code).join(', ')} &mdash; ${app.total_units} units</td></tr>
        <tr><td class="k">Status</td><td>Officially enrolled</td></tr>
        <tr><td class="k">Amount paid</td><td>${app.payment ? fmtMoney(app.payment.total_amount) : '\u2014'}</td></tr>
        <tr><td class="k">Issued</td><td>${app.updated_at}</td></tr>
      </table>
    </div>
    <div class="row"><button class="btn ghost" onclick="window.print()">Print / save slip</button></div>
  `);
}

/* ---------------- STAFF QUEUES ---------------- */

async function renderQueue(){
  const role = currentUser.role;
  let queue;
  try{ queue = await api(`/${role}/queue`); }catch(e){ lastError=e.message; renderAuth(); return; }
  const title = role==='dean' ? 'Applications for evaluation'
    : role==='registrar' ? 'Applications for verification'
    : 'Pending payments';
  el(`
    ${header()}
    <span class="roleTag">${roleLabel(role)}</span>
    ${errorBox()}
    <div class="card">
      <h2>${title}</h2>
      <p class="hint">${queue.length} record${queue.length===1?'':'s'} waiting on you.</p>
      ${queue.length===0 ? `<div class="empty">Nothing in your queue right now.</div>` :
        queue.map(r => `
          <div class="rec">
            <div>
              <div class="who">${r.student_name}</div>
              <div class="meta">ENR-${String(r.id).padStart(5,'0')} &middot; ${r.program_name} &middot; ${r.created_at}</div>
            </div>
            <button class="btn small" onclick="openApplication(${r.id})">Open</button>
          </div>
        `).join('')}
    </div>
  `);
}

async function openApplication(id){
  let app;
  try{ app = await api(`/enrollment/${id}`); }catch(e){ lastError=e.message; renderQueue(); return; }
  if(currentUser.role === 'dean') renderDeanDetail(app);
  else if(currentUser.role === 'registrar') renderRegistrarDetail(app);
  else renderCashierDetail(app);
}

async function renderDeanDetail(app){
  let subjects = [];
  try{ subjects = await api(`/programs/${app.program.id}/subjects`); }catch(e){}
  const assigned = new Set(app.subjects.map(s=>s.id));
  el(`
    ${header()}
    <span class="roleTag">Dean</span>
    ${errorBox()}
    <div class="card">
      <button class="btn ghost small" onclick="renderQueue()">&larr; Back to queue</button>
      <h2 style="margin-top:14px;">${app.student.name}</h2>
      <p class="hint">${app.program.name} &middot; ENR-${String(app.id).padStart(5,'0')} &middot; submitted ${app.created_at}</p>

      <h3>Student information</h3>
      <p class="hint">Address: ${app.student.address || '\u2014'} &middot; Contact: ${app.student.contact_number || '\u2014'}<br/>
      Previous school: ${app.student.previous_school || '\u2014'} &middot; Previous program: ${app.student.previous_program || '\u2014'}</p>

      <h3>Prior enrollment records</h3>
      ${app.prior_records.length===0 ? `<p class="hint">No prior applications on file.</p>` :
        app.prior_records.map(p=>`<p class="hint">${p.program} &mdash; ${p.status} (${p.date})</p>`).join('')}

      <h3>Assign subjects</h3>
      <p class="hint">Select the subjects appropriate for this student based on the program and academic record.</p>
      <div class="subjlist">
        ${subjects.map(s => `
          <label class="subjrow" style="cursor:pointer;">
            <input type="checkbox" value="${s.id}" class="subjChk" ${assigned.has(s.id)?'checked':''} />
            <span class="scode">${s.code}</span>${s.title}<span class="sunits">${s.units} units</span>
          </label>
        `).join('')}
      </div>
      <div class="row"><button class="btn small ghost" onclick="saveSubjects(${app.id})">Save subjects</button></div>

      <h3>Remarks</h3>
      <textarea id="deanRemarks" placeholder="e.g. Recommended for enrollment">${app.dean_remarks||''}</textarea>
      <div class="row">
        <button class="btn small ghost" onclick="saveRemarks(${app.id})">Save remarks</button>
      </div>

      <div class="row">
        <button class="btn approve" onclick="deanApprove(${app.id})">Approve &rarr; Registrar</button>
        <button class="btn reject" onclick="deanReject(${app.id})">Return to student</button>
      </div>
    </div>
  `);
}
async function saveSubjects(appId){
  const ids = Array.from(document.querySelectorAll('.subjChk:checked')).map(c=>Number(c.value));
  try{ await api(`/dean/applications/${appId}/subjects`,'POST',{subject_ids: ids}); lastError=''; openApplication(appId); }
  catch(e){ lastError=e.message; openApplication(appId); }
}
async function saveRemarks(appId){
  const remarks = document.getElementById('deanRemarks').value;
  try{ await api(`/dean/applications/${appId}/remarks`,'POST',{Remarks: remarks}); lastError=''; }
  catch(e){ lastError=e.message; openApplication(appId); }
}
async function deanApprove(appId){
  try{ await api(`/dean/applications/${appId}/approve`,'POST',{}); lastError=''; renderQueue(); }
  catch(e){ lastError=e.message; openApplication(appId); }
}
async function deanReject(appId){
  const reason = prompt('Reason for returning this application to the student:');
  if(reason===null) return;
  try{ await api(`/dean/applications/${appId}/reject`,'POST',{Reason: reason}); lastError=''; renderQueue(); }
  catch(e){ lastError=e.message; openApplication(appId); }
}

async function renderRegistrarDetail(app){
  el(`
    ${header()}
    <span class="roleTag">Registrar</span>
    ${errorBox()}
    <div class="card">
      <button class="btn ghost small" onclick="renderQueue()">&larr; Back to queue</button>
      <h2 style="margin-top:14px;">${app.student.name}</h2>
      <p class="hint">${app.program.name} &middot; ENR-${String(app.id).padStart(5,'0')}</p>

      <h3>Student information</h3>
      <p class="hint">Address: ${app.student.address || '\u2014'} &middot; Contact: ${app.student.contact_number || '\u2014'}</p>

      <h3>Documents submitted</h3>
      ${app.documents.length===0 ? `<p class="hint">No documents uploaded.</p>` :
        app.documents.map(d=>`<div class="docitem"><span>${d.name}</span><span>${d.uploaded_at}</span></div>`).join('')}

      <h3>Dean's suggested subjects</h3>
      <div class="subjlist">
        ${app.subjects.map(s => `<div class="subjrow"><span class="scode">${s.code}</span>${s.title}<span class="sunits">${s.units} units</span></div>`).join('')}
      </div>
      <p class="hint" style="margin-top:8px;">Total: ${app.total_units} units &middot; Dean's remarks: ${app.dean_remarks || '\u2014'}</p>

      <div class="row">
        <button class="btn approve" onclick="registrarVerify(${app.id})">Verify &rarr; Cashier</button>
        <button class="btn reject" onclick="registrarReturn(${app.id},'student')">Return to student</button>
        <button class="btn ghost" onclick="registrarReturn(${app.id},'dean')">Return to Dean</button>
      </div>
    </div>
  `);
}
async function registrarVerify(appId){
  try{ await api(`/registrar/applications/${appId}/verify`,'POST',{}); lastError=''; renderQueue(); }
  catch(e){ lastError=e.message; openApplication(appId); }
}
async function registrarReturn(appId, target){
  const reason = prompt(`Reason for returning to the ${target === 'dean' ? 'Dean' : 'student'}:`);
  if(reason===null) return;
  try{ await api(`/registrar/applications/${appId}/return`,'POST',{Reason: reason, Target: target}); lastError=''; renderQueue(); }
  catch(e){ lastError=e.message; openApplication(appId); }
}

async function renderCashierDetail(app){
  el(`
    ${header()}
    <span class="roleTag">Cashier</span>
    ${errorBox()}
    <div class="card">
      <button class="btn ghost small" onclick="renderQueue()">&larr; Back to queue</button>
      <h2 style="margin-top:14px;">${app.student.name}</h2>
      <p class="hint">${app.program.name} &middot; ENR-${String(app.id).padStart(5,'0')}</p>

      <h3>Subjects</h3>
      <div class="subjlist">
        ${app.subjects.map(s => `<div class="subjrow"><span class="scode">${s.code}</span>${s.title}<span class="sunits">${s.units} units</span></div>`).join('')}
      </div>

      <h3>Assessment</h3>
      ${app.payment ? `
      <table class="feetable">
        <tr><td>Tuition (${app.payment.total_units} units)</td><td class="r">${fmtMoney(app.payment.tuition_fee)}</td></tr>
        <tr><td>Miscellaneous &amp; other fees</td><td class="r">${fmtMoney(app.payment.misc_fee)}</td></tr>
        <tr class="total"><td>Total amount due</td><td class="r">${fmtMoney(app.payment.total_amount)}</td></tr>
      </table>
      ` : `<p class="hint">No assessment on file.</p>`}

      <div class="row">
        <button class="btn approve" onclick="cashierConfirm(${app.id})">Confirm payment received</button>
      </div>
    </div>
  `);
}
async function cashierConfirm(appId){
  try{ await api(`/cashier/applications/${appId}/confirm`,'POST',{}); lastError=''; renderQueue(); }
  catch(e){ lastError=e.message; openApplication(appId); }
}

/* ---------------- BOOT ---------------- */

async function boot(){
  if(!token || !currentUser){ renderAuth(); return; }
  if(currentUser.role === 'student') await renderStudent();
  else await renderQueue();
}
boot();
