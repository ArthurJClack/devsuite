// hq ui: plain js, no build step. talks to the local hq server, which talks to git + github.
'use strict';

// ------------------------------------------------------------------ plumbing
const KEY = (() => {
  const m = location.hash.match(/k=([0-9a-f]+)/);
  if (m) { localStorage.setItem('hqk', m[1]); history.replaceState(null, '', location.pathname); }
  return localStorage.getItem('hqk') || '';
})();
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

async function api(path, opts = {}) {
  const r = await fetch(path, {
    method: opts.method || (opts.body ? 'POST' : 'GET'),
    headers: { 'X-HQ-Key': KEY, 'Content-Type': 'application/json' },
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || r.statusText);
  return data;
}

function toast(msg, bad) {
  const t = $('#toast');
  t.textContent = msg; t.className = 'on' + (bad ? ' bad' : '');
  clearTimeout(toast.t); toast.t = setTimeout(() => t.className = '', bad ? 5000 : 2500);
}
const fail = e => toast(e.message || String(e), true);

function ago(d) {
  const s = (Date.now() - new Date(d)) / 1000;
  if (s < 60) return 'now';
  if (s < 3600) return Math.floor(s / 60) + 'm';
  if (s < 86400) return Math.floor(s / 3600) + 'h';
  if (s < 86400 * 30) return Math.floor(s / 86400) + 'd';
  return new Date(d).toLocaleDateString();
}

// tiny markdown: `code`, **bold**, links, @mentions
function md(s) {
  return esc(s)
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>')
    .replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noopener">$1</a>')
    .replace(/(^|\s)@([\w-]+)/g, '$1<b class="dim">@$2</b>');
}

function modal(html, onMount) {
  $('#modalBox').innerHTML = html;
  $('#modal').classList.remove('hidden');
  const close = () => $('#modal').classList.add('hidden');
  $$('[data-close]', $('#modalBox')).forEach(b => b.onclick = close);
  onMount && onMount($('#modalBox'), close);
  const first = $('input, textarea', $('#modalBox')); first && first.focus();
  return close;
}
// broken avatar (offline etc): hide it instead of showing a broken-image icon
document.addEventListener('error', e => { if (e.target.tagName === 'IMG') e.target.style.visibility = 'hidden'; }, true);
$('#modal').addEventListener('mousedown', e => { if (e.target.id === 'modal') $('#modal').classList.add('hidden'); });
document.addEventListener('keydown', e => { if (e.key === 'Escape') $('#modal').classList.add('hidden'); });

function confirmBox(title, text, yes = 'do it') {
  return new Promise(res => {
    modal(`<h1>${esc(title)}</h1><p class="dim">${text}</p><div class="btns"><button class="pri" id="cy">${esc(yes)}</button><button data-close>cancel</button></div>`,
      (box, close) => {
        $('#cy', box).onclick = () => { close(); res(true); };
        $$('[data-close]', box).forEach(b => b.addEventListener('click', () => res(false)));
      });
  });
}

function prompt2(title, label, placeholder = '', multiline = false) {
  return new Promise(res => {
    const field = multiline ? `<textarea id="pv" placeholder="${esc(placeholder)}"></textarea>` : `<input id="pv" placeholder="${esc(placeholder)}">`;
    modal(`<h1>${esc(title)}</h1><label>${esc(label)}</label>${field}<div class="btns"><button class="pri" id="py">go</button><button data-close>cancel</button></div>`,
      (box, close) => {
        const go = () => { close(); res($('#pv', box).value); };
        $('#py', box).onclick = go;
        $('#pv', box).addEventListener('keydown', e => { if (e.key === 'Enter' && (!multiline || e.ctrlKey)) go(); });
        $$('[data-close]', box).forEach(b => b.addEventListener('click', () => res(null)));
      });
  });
}

// ------------------------------------------------------------------ state
const S = { state: null, remote: [], activity: [], board: { cards: [], loaded: false, loading: false, error: '', at: 0, view: localStorage.getItem('hqBoard') || 'all', q: '', mine: false, prio: '', tag: '', allDone: false }, templates: {}, chat: [], chatLoaded: false, team: null, page: 'dash', seenChat: +localStorage.getItem('hqSeen') || 0 };
const gameById = id => (S.state?.games || []).find(g => g.id === id);
const gColor = id => gameById(id)?.color || '#8fd3ff';
const studioReady = () => S.state?.studioRepo && S.state?.chatIssue;

async function loadState() {
  try { S.state = await api('/api/state'); } catch (e) {
    if (/bad key/.test(e.message)) {
      document.body.innerHTML = '<div class="signin panel"><h1>reopen hq</h1><p class="dim">this tab lost its key. close it and double-click hq again.</p></div>';
    }
    throw e;
  }
  if (S.lastVer && S.lastVer !== S.state.version) toast('updated to ' + S.state.version);
  S.lastVer = S.state.version;
  $('#studioName').textContent = S.state.studio || 'hq';
  document.title = 'hq · ' + (S.state.studio || '');
  const me = S.state.me;
  $('#meBox').innerHTML = me ? `<img class="av" src="${esc(me.avatar_url)}&s=44" alt=""><span>${esc(me.login)}</span>` : '';
  renderBanner();
}

function renderBanner() {
  const st = S.state, b = $('#banner');
  if (!st.gitOK) {
    b.innerHTML = `<div class="notice"><div class="grow"><b>git isn't installed.</b> <span class="dim">hq needs it to download and push games.</span></div><a href="https://git-scm.com/downloads" target="_blank"><button class="pri">get git</button></a></div>`;
  } else if (st.update?.available) {
    const u = st.update;
    b.innerHTML = `<div class="notice"><div class="grow"><b>hq ${esc(u.latest)} is out</b> <span class="dim">(you have ${esc(st.version)}). ${u.applying ? 'updating, hang on a sec…' : st.autoUpdate ? 'it installs itself once nothing is running.' : ''}</span></div>${u.applying ? '' : '<button class="pri" id="upGo">update now</button>'}</div>`;
    const go = $('#upGo'); if (go) go.onclick = applyUpdate;
  } else if (st.signedIn && !studioReady()) {
    const def = (st.me?.login || 'you') + '/fishmarket-hq';
    b.innerHTML = `<div class="notice"><div class="grow"><b>one-time setup:</b> <span class="dim">make the studio repo. it's private and holds the board, chat and the games list everyone shares.</span></div>
      <input id="stRepo" style="max-width:260px" value="${esc(st.studioRepo || def)}"><button class="pri" id="stGo">set it up</button></div>`;
    $('#stGo').onclick = () => runJob(api('/api/studio/setup', { body: { repo: $('#stRepo').value, name: st.studio } }));
  } else b.innerHTML = '';
}

// ------------------------------------------------------------------ jobs / console
let jobPoll = null;
async function runJob(promise, onDone) {
  let job;
  try { job = await promise; } catch (e) { return fail(e); }
  const con = $('#console'), log = $('#conLog');
  con.classList.remove('hidden');
  $('#conTitle').textContent = job.title;
  $('#conState').textContent = 'running…';
  $('#jobDot').className = 'run';
  log.textContent = '';
  let at = 0;
  clearInterval(jobPoll);
  jobPoll = setInterval(async () => {
    let r;
    try { r = await api(`/api/job/${job.id}?from=${at}`); } catch (e) { return; }
    if (r.log) {
      const stick = log.scrollTop + log.clientHeight >= log.scrollHeight - 30;
      log.textContent += r.log; at = r.at;
      if (stick) log.scrollTop = log.scrollHeight;
    }
    if (r.done) {
      clearInterval(jobPoll);
      $('#conState').textContent = r.ok ? 'done' : 'failed';
      $('#jobDot').className = r.ok ? 'ok' : 'bad';
      toast(r.ok ? `${job.title}: done` : `${job.title}: failed (see console)`, !r.ok);
      await refresh();
      onDone && onDone(r.ok);
    }
  }, 400);
}
$('#conClose').onclick = () => $('#console').classList.add('hidden');
$('#jobsBtn').onclick = () => $('#console').classList.toggle('hidden');

// ------------------------------------------------------------------ pages
function show(page) {
  S.page = page;
  $$('#nav a').forEach(a => a.classList.toggle('on', a.dataset.page === page));
  $$('.page').forEach(p => p.classList.toggle('on', p.id === 'page-' + page));
  localStorage.setItem('hqPage', page);
  render();
  if (page === 'team') loadTeam();
  if (page === 'board' || (page === 'dash' && Date.now() - S.board.at > 20000)) loadBoard();
  if (page === 'dash' && Date.now() - (S.remoteAt || 0) > 60000) loadRemote();
  if (page === 'chat' || page === 'dash') markRead();
}
$$('#nav a').forEach(a => a.onclick = () => show(a.dataset.page));

function render() {
  if (!S.state) return;
  if (!S.state.signedIn) return renderSignin();
  $('#side').classList.remove('hidden');
  ({ dash: renderDash, games: renderGames, board: renderBoard, chat: renderChat, team: renderTeam, settings: renderSettings })[S.page]();
}

// ---------- sign in
function renderSignin() {
  $$('.page').forEach(p => p.classList.remove('on'));
  const p = $('#page-dash'); p.classList.add('on');
  const scopes = 'https://github.com/settings/tokens/new?scopes=repo,read:org,workflow&description=hq%20(' + encodeURIComponent(S.state.studio || 'studio') + ')';
  p.innerHTML = `<div class="signin panel">
    <svg viewBox="0 0 64 32" class="fish"><path d="M4 16C14 2 34 2 46 16 34 30 14 30 4 16Z" fill="#ff7a59"/><path d="M46 16 60 5v22Z" fill="#ff7a59"/><circle cx="14" cy="14" r="2.4" fill="#0b1016"/></svg>
    <h1>${esc(S.state.studio)}</h1><p class="dim">sign in with github to get the games, the board and the chat.</p>
    ${S.state.authError ? `<p class="tag bad">${esc(S.state.authError)}</p>` : ''}
    <div class="btns"><button class="ice" id="useCli">use my github cli login</button></div>
    <label>or paste a token (<a href="${scopes}" target="_blank">make one here</a>: it opens with the right boxes ticked, just hit generate)</label>
    <div class="row"><input id="tok" type="password" placeholder="ghp_…"><button class="pri" id="tokGo">sign in</button></div>
    <p class="dim" style="font-size:12px">the token stays on this computer only.</p></div>`;
  const go = async token => {
    try { await api('/api/signin', { body: { token } }); await refresh(true); show('dash'); } catch (e) { fail(e); }
  };
  $('#useCli').onclick = () => go('');
  $('#tokGo').onclick = () => go($('#tok').value);
  $('#tok').onkeydown = e => e.key === 'Enter' && go($('#tok').value);
}

// ---------- dashboard
function renderDash() {
  const p = $('#page-dash');
  const me = S.state.me?.login;
  const mine = sortCards(S.board.cards.filter(c => c.column !== 'done' && c.assignees.some(a => a.login === me)));
  p.innerHTML = `<div class="head"><h1>dashboard</h1><span class="grow"></span><button class="ghost small" id="dRef">refresh</button></div>
    <div class="grid cols-4">${S.state.games.map(gameTile).join('')}</div>
    <div class="dash">
      <div class="grid" style="align-content:start">
        <div class="panel"><h2>recent work on dev</h2><div class="feed">${!S.remoteLoaded ? '<div class="empty">loading…</div>' : S.activity.length ? S.activity.slice(0, 14).map(a => `
          <div class="it"><img class="av" src="${esc(a.avatar || '')}" alt="" onerror="this.style.visibility='hidden'">
          <div class="msg"><span class="tag c" style="--c:${gColor(a.game)}">${esc(a.game)}</span> ${esc(a.msg)} <span class="dim">· ${esc(a.who)}</span></div>
          <a class="dim mono" href="${esc(a.url)}" target="_blank">${ago(a.at)}</a></div>`).join('') : '<div class="empty">nothing yet</div>'}</div></div>
        <div class="panel"><h2>my cards <a class="small" data-go="board">board →</a></h2>${!S.board.loaded ? `<div class="empty">${S.board.error ? esc(S.board.error) : studioReady() ? 'loading…' : 'set up the studio repo first'}</div>` : mine.length ? mine.map(cardHTML).join('') : '<div class="empty">nothing assigned to you. lucky</div>'}</div>
      </div>
      <div class="panel"><h2>chat <a class="small" data-go="chat">open →</a></h2>${chatHTML(true)}</div>
    </div>`;
  $('#dRef').onclick = () => refresh(true);
  $$('[data-go]', p).forEach(a => a.onclick = () => show(a.dataset.go));
  bindCards(p); bindChat(p); bindTiles(p);
}

function gameTile(g) {
  const r = S.remote.find(x => x.id === g.id) || {};
  const l = g.local;
  const rel = r.release;
  return `<div class="panel gcard" style="--c:${esc(g.color || '#8fd3ff')}" data-game="${esc(g.id)}">
    <div class="title"><h3>${esc(g.name)}</h3><span class="tag c">${esc(g.genre)}</span></div>
    <div class="stat"><span>latest build</span><b>${rel ? esc(rel.tag_name) + (rel.prerelease ? ' <span class="tag warn">dev</span>' : '') : '—'}</b></div>
    <div class="stat"><span>downloads</span><b>${r.downloads ?? '—'}</b></div>
    <div class="stat"><span>dev not on main</span><b>${r.devAhead ?? '—'}</b></div>
    <div class="stat"><span>open PRs</span><b>${r.prs ?? '—'}</b></div>
    <div class="stat"><span>you</span><b>${l.cloned ? `${esc(l.branch)}${l.dirty ? ` <span class="tag warn">${l.dirty} changed</span>` : ''}${l.behind ? ` <span class="tag">↓${l.behind}</span>` : ''}${l.ahead ? ` <span class="tag">↑${l.ahead}</span>` : ''}` : '<span class="dim">not downloaded</span>'}</b></div>
    ${r.last ? `<p class="dim" style="font-size:12px;margin:8px 0 0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(r.last.who)}: ${esc(r.last.msg)} · ${ago(r.last.at)}</p>` : ''}
    ${r.error ? `<p class="tag bad" style="margin-top:8px;white-space:normal">${esc(r.error)}</p>` : ''}
    <div class="btns">${l.cloned ? `<button class="small" data-op="pull">pull dev</button><button class="small pri" data-op="push">push dev</button><button class="small" data-op="editor">open</button><button class="small ice" data-op="playdev">▶ dev</button>`
      : `<button class="small ice" data-op="clone">download</button><button class="small" data-op="playdev">▶ dev</button>`}</div></div>`;
}

function bindTiles(root) {
  $$('[data-game] [data-op]', root).forEach(b => b.onclick = () => gameOp(b.closest('[data-game]').dataset.game, b.dataset.op, b.dataset.path));
}

async function gameOp(id, op, path) {
  const g = gameById(id);
  if (op === 'push') {
    const msg = await prompt2(`push ${g.name} to dev`, 'what did you change? (it goes in the commit and the team chat)' + (g.local.dirty ? '' : ' — no unsaved changes, this sends your commits'), 'fixed the thing', false);
    if (msg === null) return;
    return runJob(api(`/api/game/${id}/push`, { body: { message: msg } }));
  }
  if (op === 'tomain') {
    const r = S.remote.find(x => x.id === id);
    if (!await confirmBox(`ship ${g.name} dev → main?`, `merges ${r?.devAhead ?? 'all'} commit(s) from dev into main on github. main is what release builds come from.`, 'merge to main')) return;
  }
  if (op === 'publish' || op === 'hotfix') {
    if (!await confirmBox(`${op} ${g.name}?`, `runs <code>${esc(op === 'publish' ? g.publish : g.hotfix)}</code> in its own window. players get it.`, op)) return;
  }
  const jobs = ['clone', 'pull', 'push', 'tomain', 'playdev'];
  if (jobs.includes(op)) return runJob(api(`/api/game/${id}/${op}`, { body: {} }));
  try { await api(`/api/game/${id}/${op}`, { body: { path } }); toast(op === 'tool' ? `started ${path}` : 'opened'); } catch (e) { fail(e); }
}

// ---------- games
function renderGames() {
  const p = $('#page-games');
  p.innerHTML = `<div class="head"><h1>games</h1><span class="dim mono">${esc(S.state.workspace)}</span><span class="grow"></span><button class="small" id="gEdit">edit games list</button></div>
    <div class="grid cols-2">${S.state.games.map(g => {
      const l = g.local;
      return `<div class="panel gcard" style="--c:${esc(g.color || '#8fd3ff')}" data-game="${esc(g.id)}">
        <div class="title"><h3>${esc(g.name)}</h3><span class="tag c">${esc(g.genre)}</span><span class="dim mono" style="font-size:12px">${esc(g.repo)}</span></div>
        ${l.cloned ? `
          <div class="stat"><span>branch</span><b>${esc(l.branch)} @ ${esc(l.commit)}</b></div>
          <div class="stat"><span>vs dev</span><b>${!l.hasDev ? '<span class="dim">no dev branch yet</span>' : (l.behind || l.ahead) ? `${l.behind} behind · ${l.ahead} ahead` : 'up to date'}</b></div>
          <div class="stat"><span>unsaved changes</span><b>${l.dirty || 'none'}</b></div>
          ${l.files?.length ? `<pre class="files">${esc(l.files.join('\n'))}</pre>` : ''}
          <div class="btns">
            <button class="ice" data-op="pull">↓ pull dev</button><button class="pri" data-op="push">↑ push to dev</button><button data-op="tomain">dev → main</button>
          </div>
          <div class="btns">
            <button class="small" data-op="editor">godot editor</button><button class="small" data-op="play" title="runs your copy, with your changes">play mine</button><button class="small ice" data-op="playdev" title="runs the latest dev from github in a separate copy, doesn't touch yours">▶ play dev</button><button class="small" data-op="folder">folder</button>
            <button class="small ghost" data-op="github">github</button><button class="small ghost" data-op="releases">releases</button>
            ${g.publish ? '<button class="small" data-op="publish">publish update</button>' : ''}${g.hotfix ? '<button class="small" data-op="hotfix">hotfix</button>' : ''}
          </div>
          ${l.tools?.length ? `<details><summary>tools (${l.tools.length})</summary><div class="tools">${l.tools.map(t => `<button data-op="tool" data-path="${esc(t.path)}" title="${esc(t.path)}">${esc(t.name)}</button>`).join('')}</div></details>` : ''}`
        : `<p class="dim">not on this computer yet. downloads the source into <span class="mono">${esc(l.path)}</span>${g.dev !== '' ? ', on the dev branch' : ''}.</p>
           <div class="btns"><button class="ice" data-op="clone">download source</button><button data-op="playdev">▶ play dev</button><button class="ghost small" data-op="github">github</button><button class="ghost small" data-op="releases">releases</button></div>`}
      </div>`;
    }).join('')}</div>`;
  bindTiles(p);
  $('#gEdit').onclick = editGames;
}

function editGames() {
  const games = S.state.games.map(({ local, ...g }) => g);
  modal(`<h1>games list</h1><p class="dim">shared with everyone through the studio repo. fields: id, name, genre, repo (source), releases (public builds repo), dev/main branch names, color, publish/hotfix scripts, tools [{name, path}].</p>
    <textarea id="gj" style="min-height:360px">${esc(JSON.stringify(games, null, 2))}</textarea>
    <div class="btns"><button class="pri" id="gs">save</button><button data-close>cancel</button></div>`, (box, close) => {
    $('#gs', box).onclick = async () => {
      let parsed;
      try { parsed = JSON.parse($('#gj', box).value); } catch (e) { return fail('that json is broken: ' + e.message); }
      try { await api('/api/games', { body: { games: parsed } }); close(); toast('saved'); refresh(true); } catch (e) { fail(e); }
    };
  });
}

// ---------- board
const PRIO = { p0: ['urgent', '#ff5d73'], p1: ['high', '#ff9f43'], p2: ['normal', '#ffcc66'], p3: ['low', '#6e8a7a'] };
const TAGS = ['bug', 'feature', 'design', 'art', 'audio', 'writing', 'tech', 'tools', 'world', 'combat', 'ui', 'steam', 'research', 'blocked'];
const tagColor = t => ({ bug: '#ff5d73', feature: '#59d499', art: '#ff9eb5', design: '#8fd3ff', audio: '#ffd166', writing: '#c9b6ff', tech: '#5fa8ff', tools: '#a78bfa', world: '#2dd4bf', combat: '#ff6b35', ui: '#f9c0a8', steam: '#9aa7b4', research: '#9fd8d8', blocked: '#ffffff' }[t] || '#7b8fa3');
const today = () => new Date().toISOString().slice(0, 10);
function dueTag(d, done) {
  if (!d) return '';
  const days = Math.round((new Date(d + 'T12:00') - new Date(today() + 'T12:00')) / 864e5);
  const cls = done ? '' : days < 0 ? 'bad' : days <= 2 ? 'warn' : '';
  const txt = days === 0 ? 'today' : days === 1 ? 'tomorrow' : days === -1 ? 'yesterday' : new Date(d + 'T12:00').toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  return `<span class="tag ${cls}" title="due ${d}">⏱ ${txt}</span>`;
}
const prioRank = c => c.priority ? +c.priority[1] : 4;
function sortCards(list) {
  return list.sort((a, b) => prioRank(a) - prioRank(b) || (a.due || '9999').localeCompare(b.due || '9999') || new Date(b.updated) - new Date(a.updated));
}

function cardHTML(c) {
  const g = c.game ? gameById(c.game) : null;
  const p = PRIO[c.priority];
  return `<div class="card" draggable="true" data-card="${c.number}" style="--c:${p ? p[1] : g ? esc(g.color) : 'transparent'}">
    ${c.tags.length ? `<div class="ctags">${c.tags.map(t => `<i style="background:${tagColor(t)}" title="${esc(t)}"></i>`).join('')}</div>` : ''}
    <div class="t">${esc(c.title)}</div>
    <div class="m"><span class="chips">
      ${S.board.view === 'all' && g ? `<span class="tag c" style="--c:${esc(g.color)}">${esc(g.id)}</span>` : ''}
      ${p && c.column !== 'done' ? `<span class="tag" style="color:${p[1]};border-color:${p[1]}55">${p[0]}</span>` : ''}
      ${dueTag(c.due, c.column === 'done')}
      ${c.checkTotal ? `<span class="tag ${c.checkDone === c.checkTotal ? 'ok' : ''}">☑ ${c.checkDone}/${c.checkTotal}</span>` : ''}
      ${c.comments ? `<span class="dim mono" style="font-size:11px">💬${c.comments}</span>` : ''}
      ${c.body && !c.checkTotal ? '<span class="dim" title="has details">≡</span>' : ''}
    </span>
    <span class="avs">${c.assignees.map(a => `<img class="av" src="${esc(a.avatar_url)}&s=44" title="${esc(a.login)}" alt="">`).join('')}</span></div></div>`;
}

function boardFilter(c) {
  const B = S.board, q = B.q.trim().toLowerCase();
  if (B.view !== 'all' && c.game !== B.view) return false;
  if (B.mine && !c.assignees.some(a => a.login === S.state.me.login)) return false;
  if (B.tag && !c.tags.includes(B.tag)) return false;
  if (B.prio && c.priority !== B.prio) return false;
  if (q && !(c.title.toLowerCase().includes(q) || c.body.toLowerCase().includes(q) || ('#' + c.number) === q)) return false;
  return true;
}

function renderBoard() {
  const p = $('#page-board');
  if (!studioReady()) { p.innerHTML = '<div class="head"><h1>board</h1></div><div class="empty">set up the studio repo first (banner up top)</div>'; return; }
  const B = S.board;
  const cards = B.cards.filter(boardFilter);
  const viewGame = B.view !== 'all' ? gameById(B.view) : null;
  const tpl = viewGame && S.templates[B.view];
  const onBoard = B.cards.filter(c => c.game === B.view).length;
  const usedTags = [...new Set(B.cards.flatMap(c => c.tags))].sort();
  const keepFocus = document.activeElement?.id === 'bq';
  p.innerHTML = `<div class="head"><h1>board</h1>
      <span class="chips boards">${[['all', 'everything', '#8fd3ff'], ...S.state.games.map(g => [g.id, g.name, g.color])].map(([id, name, col]) =>
        `<span class="tag chip ${B.view === id ? 'on' : ''}" data-view="${esc(id)}" style="--c:${esc(col)}">${esc(name.toLowerCase())} <b class="dim">${id === 'all' ? B.cards.filter(c => c.column !== 'done').length : B.cards.filter(c => c.game === id && c.column !== 'done').length}</b></span>`).join('')}</span>
      <span class="grow"></span><button class="pri small" id="bNew">+ card</button></div>
    <div class="head filters">
      <input id="bq" placeholder="search cards…" value="${esc(B.q)}" style="max-width:220px">
      <span class="tag chip ${B.mine ? 'on' : ''}" id="bMine">mine</span>
      <select id="bPrio" style="width:auto"><option value="">any priority</option>${Object.entries(PRIO).map(([k, v]) => `<option value="${k}" ${B.prio === k ? 'selected' : ''}>${v[0]}</option>`).join('')}</select>
      <select id="bTag" style="width:auto"><option value="">any tag</option>${usedTags.map(t => `<option ${B.tag === t ? 'selected' : ''}>${esc(t)}</option>`).join('')}</select>
      ${B.q || B.mine || B.prio || B.tag ? '<button class="ghost small" id="bClear">clear</button>' : ''}
      <span class="grow"></span>
      ${B.error ? `<span class="tag bad">${esc(B.error)}</span> <button class="small" id="bRetry">retry</button>` : B.loading ? '<span class="dim">syncing…</span>' : `<span class="dim" style="font-size:12px">synced ${B.at ? ago(B.at) : ''}</span>`}
    </div>
    ${tpl && onBoard < 5 && B.loaded ? `<div class="notice" style="margin-bottom:12px"><div class="grow"><b>${esc(viewGame.name)} board is empty.</b> <span class="dim">load the starter board: ${tpl.cards} cards from the game's docs (status, design plans, known issues).</span></div><button class="pri" id="bTpl">load starter board</button></div>` : ''}
    ${!B.loaded && !B.error ? '<div class="empty">loading the board…</div>' : `<div class="board">${S.state.columns.map(col => {
      let list = sortCards(cards.filter(c => c.column === col));
      const total = list.length, cap = col === 'done' && !B.allDone ? 12 : 1e9;
      list = list.slice(0, cap);
      return `<div class="col" data-col="${col}"><header><span>${col}</span><span class="dim mono">${total}</span></header>${list.map(cardHTML).join('')}
        ${total > list.length ? `<button class="ghost small" id="bMoreDone" style="width:100%">show ${total - list.length} more</button>` : ''}
        ${col !== 'done' ? `<div class="quick"><input data-quick="${col}" placeholder="+ add a card"></div>` : ''}</div>`;
    }).join('')}</div>`}`;
  $$('[data-view]', p).forEach(c => c.onclick = () => { B.view = c.dataset.view; localStorage.setItem('hqBoard', B.view); renderBoard(); });
  $('#bNew').onclick = () => cardModal(null, 'todo');
  const bq = $('#bq'); bq.oninput = () => { B.q = bq.value; renderBoard(); };
  if (keepFocus) { bq.focus(); bq.setSelectionRange(bq.value.length, bq.value.length); }
  $('#bMine').onclick = () => { B.mine = !B.mine; renderBoard(); };
  $('#bPrio').onchange = e => { B.prio = e.target.value; renderBoard(); };
  $('#bTag').onchange = e => { B.tag = e.target.value; renderBoard(); };
  if ($('#bClear')) $('#bClear').onclick = () => { B.q = ''; B.mine = false; B.prio = ''; B.tag = ''; renderBoard(); };
  if ($('#bRetry')) $('#bRetry').onclick = () => loadBoard(true);
  if ($('#bMoreDone')) $('#bMoreDone').onclick = () => { B.allDone = true; renderBoard(); };
  if ($('#bTpl')) $('#bTpl').onclick = () => runJob(api('/api/board/import', { body: { game: B.view } }), () => loadBoard(true));
  $$('[data-quick]', p).forEach(inp => inp.onkeydown = async e => {
    if (e.key !== 'Enter' || !inp.value.trim()) return;
    const title = inp.value.trim(); inp.value = '';
    try {
      const c = await api('/api/board', { body: { title, column: inp.dataset.quick, game: B.view !== 'all' ? B.view : '' } });
      B.cards.unshift(c); renderBoard(); $(`[data-quick="${inp.dataset.quick}"]`)?.focus();
    } catch (er) { fail(er); }
  });
  bindCards(p);
  $$('.col', p).forEach(col => {
    col.ondragover = e => { e.preventDefault(); col.classList.add('drop'); };
    col.ondragleave = () => col.classList.remove('drop');
    col.ondrop = async e => {
      e.preventDefault(); col.classList.remove('drop');
      const n = +e.dataTransfer.getData('text/card'), dest = col.dataset.col;
      const c = B.cards.find(x => x.number === n);
      if (!c || c.column === dest) return;
      c.column = dest; renderBoard(); // optimistic
      try { Object.assign(c, await api('/api/board/' + n, { method: 'PATCH', body: { column: dest } })); } catch (err) { fail(err); loadBoard(true); }
    };
  });
}

function bindCards(root) {
  $$('[data-card]', root).forEach(el => {
    el.onclick = () => cardModal(S.board.cards.find(c => c.number === +el.dataset.card));
    el.ondragstart = e => { e.dataTransfer.setData('text/card', el.dataset.card); el.classList.add('dragging'); };
    el.ondragend = () => el.classList.remove('dragging');
  });
}

// checklist lines live in the body as "- [ ] item"; the modal edits them separately
const CHECK = /^\s*[-*] \[([ xX])\] (.*)$/;
function splitBody(body) {
  const desc = [], items = [];
  for (const line of (body || '').split('\n')) {
    const m = line.match(CHECK);
    if (m) items.push({ done: m[1] !== ' ', text: m[2] }); else desc.push(line);
  }
  return { desc: desc.join('\n').trim(), items };
}
const joinBody = (desc, items) => [desc.trim(), items.length ? items.map(i => `- [${i.done ? 'x' : ' '}] ${i.text}`).join('\n') : ''].filter(Boolean).join('\n\n');

function cardModal(c, col) {
  const B = S.board;
  const members = S.team?.members || [];
  const people = [...new Set([S.state.me.login, ...members.map(m => m.login), ...(c?.assignees || []).map(a => a.login)].filter(Boolean))];
  const st = { ...splitBody(c?.body), tags: [...(c?.tags || [])], who: (c?.assignees || []).map(a => a.login) };
  const allTags = [...new Set([...TAGS, ...B.cards.flatMap(x => x.tags), ...st.tags])];
  modal(`<div class="row" style="align-items:baseline"><h1 style="flex:1">${c ? '#' + c.number : 'new card'}</h1>${c ? `<span class="dim" style="font-size:12px;flex:none">by ${esc(c.author)} · ${ago(c.created)}</span>` : ''}</div>
    <input id="ct" class="ctitle" value="${esc(c?.title || '')}" placeholder="what needs doing">
    <div class="row">
      <div><label>list</label><select id="cc">${S.state.columns.map(x => `<option ${x === (c?.column || col) ? 'selected' : ''}>${x}</option>`).join('')}</select></div>
      <div><label>game</label><select id="cg"><option value="">—</option>${S.state.games.map(g => `<option value="${esc(g.id)}" ${g.id === (c ? c.game : B.view) ? 'selected' : ''}>${esc(g.name)}</option>`).join('')}</select></div>
      <div><label>priority</label><select id="cp"><option value="">none</option>${Object.entries(PRIO).map(([k, v]) => `<option value="${k}" ${c?.priority === k ? 'selected' : ''}>${v[0]}</option>`).join('')}</select></div>
      <div><label>due</label><input id="cdue" type="date" value="${esc(c?.due || '')}"></div>
    </div>
    <label>tags</label><div class="chips" id="ctags">${allTags.map(t => `<span class="tag chip ${st.tags.includes(t) ? 'on' : ''}" data-tag="${esc(t)}"><i class="dot" style="background:${tagColor(t)}"></i>${esc(t)}</span>`).join('')}<input id="cnewtag" placeholder="+ tag" style="width:80px;padding:2px 8px;font-size:12px"></div>
    <label>who</label><div class="chips" id="cwho">${people.map(u => `<span class="tag chip ${st.who.includes(u) ? 'on' : ''}" data-who="${esc(u)}">${esc(u)}</span>`).join('')}</div>
    <label>details</label><textarea id="cb" placeholder="notes, links, repro steps… (markdown)">${esc(st.desc)}</textarea>
    <label>checklist <span id="cprog" class="dim"></span></label><div class="prog"><i id="cbar"></i></div><div id="clist"></div>
    <div class="row"><input id="cadd" placeholder="add an item, enter to add"></div>
    <div class="btns"><button class="pri" id="cs">${c ? 'save' : 'add card'}</button><button data-close>cancel</button><span style="flex:1"></span>
      ${c ? `<a href="${esc(c.url)}" target="_blank"><button class="ghost small">github</button></a><button class="danger small" id="cd">delete</button>` : ''}</div>
    ${c ? `<label style="margin-top:16px">comments</label><div id="ccom" class="msgs" style="max-height:260px"><div class="dim">loading…</div></div>
      <div class="say"><input id="csay" placeholder="write a comment"><button class="small" id="csayGo">post</button></div>` : ''}`,
  (box, close) => {
    const drawList = () => {
      const done = st.items.filter(i => i.done).length;
      $('#cprog', box).textContent = st.items.length ? `${done}/${st.items.length}` : '';
      $('#cbar', box).style.width = st.items.length ? (100 * done / st.items.length) + '%' : '0';
      $('#cbar', box).parentElement.style.display = st.items.length ? '' : 'none';
      $('#clist', box).innerHTML = st.items.map((it, i) => `<div class="citem ${it.done ? 'done' : ''}"><input type="checkbox" data-i="${i}" ${it.done ? 'checked' : ''}><span>${esc(it.text)}</span><button class="ghost small" data-del="${i}">×</button></div>`).join('');
      $$('[data-i]', box).forEach(cb => cb.onchange = () => { st.items[+cb.dataset.i].done = cb.checked; drawList(); });
      $$('[data-del]', box).forEach(b => b.onclick = () => { st.items.splice(+b.dataset.del, 1); drawList(); });
    };
    drawList();
    $('#cadd', box).onkeydown = e => { if (e.key === 'Enter' && e.target.value.trim()) { st.items.push({ done: false, text: e.target.value.trim() }); e.target.value = ''; drawList(); } };
    const bindTag = el => el.onclick = () => { const t = el.dataset.tag; st.tags = st.tags.includes(t) ? st.tags.filter(x => x !== t) : [...st.tags, t]; el.classList.toggle('on'); };
    $$('[data-tag]', box).forEach(bindTag);
    $('#cnewtag', box).onkeydown = e => {
      const t = e.target.value.trim().toLowerCase().replace(/\s+/g, '-');
      if (e.key !== 'Enter' || !t) return;
      if (!st.tags.includes(t)) st.tags.push(t);
      const el = document.createElement('span'); el.className = 'tag chip on'; el.dataset.tag = t; el.innerHTML = `<i class="dot" style="background:${tagColor(t)}"></i>${esc(t)}`;
      e.target.before(el); bindTag(el); e.target.value = '';
    };
    $$('[data-who]', box).forEach(el => el.onclick = () => { const u = el.dataset.who; st.who = st.who.includes(u) ? st.who.filter(x => x !== u) : [...st.who, u]; el.classList.toggle('on'); });
    $('#cs', box).onclick = async () => {
      const body = { title: $('#ct', box).value, body: joinBody($('#cb', box).value, st.items), column: $('#cc', box).value, game: $('#cg', box).value,
        priority: $('#cp', box).value, due: $('#cdue', box).value, tags: st.tags, assignees: st.who };
      if (!body.title.trim()) return fail('give it a title');
      try {
        const saved = c ? await api('/api/board/' + c.number, { method: 'PATCH', body }) : await api('/api/board', { body });
        const i = B.cards.findIndex(x => x.number === saved.number);
        if (i >= 0) B.cards[i] = saved; else B.cards.unshift(saved);
        close(); render();
      } catch (e) { fail(e); }
    };
    const d = $('#cd', box);
    if (d) d.onclick = async () => {
      if (!confirm('delete this card? (it gets closed on github, not erased)')) return;
      try { await api('/api/board/' + c.number, { method: 'PATCH', body: { delete: true } }); B.cards = B.cards.filter(x => x.number !== c.number); close(); render(); } catch (e) { fail(e); }
    };
    if (c) {
      const loadCom = async () => {
        try {
          const cs = await api(`/api/board/${c.number}/comments`);
          $('#ccom', box).innerHTML = cs.length ? cs.map(m => `<div class="m1"><img class="av big" src="${esc(m.avatar)}&s=60" alt=""><div><div class="who">${esc(m.who)}<small>${ago(m.at)}</small></div><div class="txt">${md(m.text)}</div></div></div>`).join('') : '<div class="dim" style="font-size:13px">no comments yet</div>';
        } catch (e) { $('#ccom', box).innerHTML = `<div class="tag bad">${esc(e.message)}</div>`; }
      };
      loadCom();
      const post = async () => {
        const t = $('#csay', box).value.trim(); if (!t) return;
        $('#csay', box).value = '';
        try { await api(`/api/board/${c.number}/comments`, { body: { text: t } }); c.comments++; loadCom(); } catch (e) { fail(e); }
      };
      $('#csayGo', box).onclick = post;
      $('#csay', box).onkeydown = e => e.key === 'Enter' && post();
    }
  });
}

// ---------- chat
function chatHTML(mini) {
  if (!studioReady()) return '<div class="empty">set up the studio repo first</div>';
  let prev = null;
  const rows = S.chat.map(m => {
    const cont = prev && prev.who === m.who && new Date(m.at) - new Date(prev.at) < 5 * 60e3;
    prev = m;
    return `<div class="m1 ${cont ? 'cont' : ''}"><img class="av big" src="${esc(m.avatar)}&s=60" alt="">
      <div>${cont ? '' : `<div class="who">${esc(m.who)}<small>${ago(m.at)}</small></div>`}<div class="txt">${md(m.text)}</div></div></div>`;
  }).join('');
  return `<div class="chat ${mini ? 'mini' : ''}"><div class="msgs">${rows || `<div class="empty">${!S.chatLoaded ? 'loading…' : S.chatError ? esc(S.chatError) : 'say hi'}</div>`}</div>
    <div class="say"><input class="sayIn" placeholder="message ${esc(S.state.studio)}"><button class="pri sayGo">send</button></div></div>`;
}

function bindChat(root) {
  const box = $('.msgs', root); if (box) box.scrollTop = box.scrollHeight;
  const inp = $('.sayIn', root); if (!inp) return;
  const send = async () => {
    const text = inp.value.trim(); if (!text) return;
    inp.value = '';
    S.chat.push({ id: -1, text, who: S.state.me.login, avatar: S.state.me.avatar_url, at: new Date().toISOString() });
    rerenderChat();
    try { await api('/api/chat', { body: { text } }); } catch (e) { fail(e); }
    loadChat();
  };
  $('.sayGo', root).onclick = send;
  inp.onkeydown = e => e.key === 'Enter' && send();
}

function rerenderChat() {
  // keep a half-typed message when new messages arrive
  for (const page of ['chat', 'dash']) {
    if (S.page !== page) continue;
    const root = $('#page-' + page);
    const old = $('.chat', root); if (!old) continue;
    const typed = $('.sayIn', root)?.value || '', focused = document.activeElement === $('.sayIn', root);
    const tmp = document.createElement('div'); tmp.innerHTML = chatHTML(page === 'dash');
    old.replaceWith(tmp.firstElementChild);
    bindChat(root);
    const inp = $('.sayIn', root); if (inp) { inp.value = typed; if (focused) inp.focus(); }
  }
}

function renderChat() {
  $('#page-chat').innerHTML = `<div class="head"><h1>chat</h1><span class="dim">lives in the studio repo's chat issue, so it works from github too</span></div>${chatHTML(false)}`;
  bindChat($('#page-chat'));
}

function markRead() {
  const last = S.chat.length ? S.chat[S.chat.length - 1].id : 0;
  if (last > S.seenChat) { S.seenChat = last; localStorage.setItem('hqSeen', last); }
  $('#unread').textContent = '';
}

// ---------- team
function renderTeam() {
  const p = $('#page-team');
  const t = S.team;
  const repos = t ? Object.keys(t.repos) : [...S.state.games.map(g => g.id), 'studio'];
  p.innerHTML = `<div class="head"><h1>team</h1><span class="grow"></span><button class="ghost small" id="tRef">refresh</button></div>
    <div class="panel"><h2>add someone</h2>
      <div class="row"><input id="tu" placeholder="their github username"></div>
      <div class="checks">${repos.map(id => `<label><input type="checkbox" value="${esc(id)}" checked> ${esc(id)}</label>`).join('')}</div>
      <p class="dim" style="font-size:12px;margin:4px 0">they get push access: pull/push dev and merge to main. github emails them an invite to accept.</p>
      <div class="btns"><button class="pri" id="tAdd">add as collaborator</button><button class="ice" id="tPkg">add + make their package</button><button id="tPkgOnly">just make a package</button></div>
      <div id="pkgOut"></div>
    </div>
    <div class="panel" style="margin-top:14px"><h2>who has access</h2>
      ${!t ? '<div class="empty">loading…</div>' : `<table><tr><th>who</th>${repos.map(r => `<th class="ck">${esc(r)}</th>`).join('')}<th></th></tr>
      ${t.members.map(m => `<tr><td><img class="av" src="${esc(m.avatar)}&s=44" alt=""> ${esc(m.login)}</td>
        ${repos.map(r => `<td class="ck">${m.repos[r] ? '<span class="yes">●</span>' : m.pending[r] ? '<span class="tag warn">invited</span>' : '<span class="no">·</span>'}</td>`).join('')}
        <td>${m.login !== S.state.me.login ? `<button class="danger small" data-rm="${esc(m.login)}">remove</button>` : ''}</td></tr>`).join('')}</table>
      ${t.errors?.length ? `<p class="dim" style="font-size:12px">${t.errors.map(esc).join('<br>')}</p>` : ''}
      <p class="dim" style="font-size:12px">● has access · invited = hasn't accepted yet</p>`}
    </div>`;
  const picked = () => $$('.checks input:checked', p).map(i => i.value);
  const user = () => $('#tu', p).value.trim();
  $('#tRef').onclick = () => { S.team = null; renderTeam(); loadTeam(); };
  $('#tAdd').onclick = () => { if (!user()) return fail('type a username'); runJob(api('/api/team/add', { body: { user: user(), repos: picked() } }), () => loadTeam()); };
  const pkg = async invite => {
    if (invite && !user()) return fail('type a username');
    try {
      const r = await api('/api/team/package', { body: { user: user(), repos: picked(), invite } });
      $('#pkgOut', p).innerHTML = `<div class="notice" style="margin-top:10px"><div class="grow">package ready: <span class="mono">${esc(r.path)}</span><br><span class="dim">send them that zip (discord, drive, whatever). they unzip, double-click hq, sign in, done.</span></div>
        <a href="/package/${encodeURIComponent(r.file)}?k=${KEY}"><button class="pri">download zip</button></a></div>`;
      if (invite) setTimeout(loadTeam, 2500);
    } catch (e) { fail(e); }
  };
  $('#tPkg').onclick = () => pkg(true);
  $('#tPkgOnly').onclick = () => pkg(false);
  $$('[data-rm]', p).forEach(b => b.onclick = async () => {
    if (!await confirmBox(`remove ${b.dataset.rm}?`, 'takes away their access to every repo checked above.', 'remove')) return;
    runJob(api('/api/team/remove', { body: { user: b.dataset.rm, repos: picked() } }), () => loadTeam());
  });
}

async function applyUpdate() {
  try { await api('/api/update/apply', { body: {} }); } catch (e) { return fail(e); }
  toast('updating… hq restarts in a few seconds');
  const before = S.state.version;
  for (let i = 0; i < 60; i++) {
    await new Promise(r => setTimeout(r, 1000));
    try { const st = await api('/api/state'); if (st.version !== before) { await refresh(true); return; } } catch (e) { }
  }
  fail('hq didn\'t come back, open it again from the start menu');
}

// ---------- settings
function renderSettings() {
  const st = S.state;
  const p = $('#page-settings');
  p.innerHTML = `<div class="head"><h1>settings</h1></div>
    <div class="grid cols-2">
      <div class="panel"><h2>this computer</h2>
        <label>where games get downloaded</label><input id="sw" value="${esc(st.workspace)}">
        <label>godot (${st.godot ? 'found: <span class="mono">' + esc(st.godot) + '</span>' : 'not found, paste the path to the godot exe'})</label><input id="sgd" value="${esc(st.godotSaved || '')}" placeholder="${esc(st.godot || 'C:\\Godot\\Godot_v4.x.exe')}">
        <div class="btns"><button class="pri" id="sSave">save</button></div>
      </div>
      <div class="panel"><h2>studio</h2>
        <div class="stat"><span>name</span><b>${esc(st.studio)}</b></div>
        <div class="stat"><span>studio repo</span><b>${st.studioRepo ? `<a href="https://github.com/${esc(st.studioRepo)}" target="_blank">${esc(st.studioRepo)}</a>` : '—'}</b></div>
        <div class="stat"><span>chat thread</span><b>${st.chatIssue ? '#' + st.chatIssue : '—'}</b></div>
        <label>join a different studio repo (owner/name)</label><div class="row"><input id="sst" value="${esc(st.studioRepo || '')}"><button id="sStudio">use it</button></div>
        <div class="btns"><button class="small" id="sSetup">re-run studio setup</button></div>
      </div>
      <div class="panel"><h2>updates</h2>
        <div class="stat"><span>this version</span><b>${esc(st.version)}</b></div>
        <div class="stat"><span>latest</span><b>${esc(st.update?.latest || '—')}${st.update?.checked && !st.update.checked.startsWith('0001') ? ` <span class="dim">· checked ${ago(st.update.checked)}</span>` : ''}</b></div>
        <div class="stat"><span>installed</span><b>${st.update?.installed ? 'yes' : 'no (running from its own folder)'}</b></div>
        ${st.update?.error ? `<p class="dim" style="font-size:12px">${esc(st.update.error)}</p>` : ''}
        <div class="checks"><label><input type="checkbox" id="sAuto" ${st.autoUpdate ? 'checked' : ''}> update automatically</label></div>
        <div class="btns"><button class="small" id="sCheck">check now</button>${st.update?.available ? '<button class="small pri" id="sUpd">update to ' + esc(st.update.latest) + '</button>' : ''}</div>
      </div>
      <div class="panel"><h2>github</h2>
        <div class="stat"><span>signed in as</span><b>${esc(st.me?.login)}</b></div>
        <div class="stat"><span>token from</span><b>${esc(st.tokenFrom)}</b></div>
        <div class="btns"><button class="danger" id="sOut">sign out</button></div>
      </div>
    </div>`;
  $('#sSave').onclick = async () => { try { await api('/api/settings', { body: { workspace: $('#sw').value, godot: $('#sgd').value } }); toast('saved'); refresh(); } catch (e) { fail(e); } };
  $('#sStudio').onclick = async () => { try { await api('/api/settings', { body: { studio: $('#sst').value } }); toast('switched'); refresh(true); } catch (e) { fail(e); } };
  $('#sSetup').onclick = () => runJob(api('/api/studio/setup', { body: { repo: st.studioRepo || (st.me.login + '/fishmarket-hq'), name: st.studio } }));
  $('#sAuto').onchange = async e => { try { await api('/api/settings', { body: { auto: e.target.checked } }); refresh(); } catch (er) { fail(er); } };
  $('#sCheck').onclick = async () => { try { const u = await api('/api/update/check', { body: {} }); toast(u.available ? u.latest + ' is available' : u.error || 'you\'re on the latest'); refresh(); } catch (e) { fail(e); } };
  if ($('#sUpd')) $('#sUpd').onclick = applyUpdate;
  $('#sOut').onclick = async () => { await api('/api/signout', { body: {} }); refresh(true); };
}

// ------------------------------------------------------------------ loading + polling
// re-render only when it won't eat what someone is typing
const busy = () => !$('#modal').classList.contains('hidden') || document.activeElement?.matches('input,textarea,select');
// data that arrives while you're typing waits; it's drawn as soon as you click out (see focusout below)
let pendingRender = false;
function softRender(pages) { if (!pages.includes(S.page)) return; if (busy()) pendingRender = true; else render(); }
document.addEventListener('focusout', () => setTimeout(() => { if (pendingRender && !busy()) { pendingRender = false; render(); } }, 50));
async function loadBoard(force) {
  const B = S.board;
  if (!studioReady() || (B.loading && !force)) return;
  B.loading = true;
  try {
    B.cards = await api('/api/board'); B.loaded = true; B.error = ''; B.at = Date.now();
  } catch (e) {
    B.error = 'board: ' + e.message; // keep showing the last good cards
  } finally { B.loading = false; }
  softRender(['board', 'dash']);
}
async function loadTeam() { try { S.team = await api('/api/team'); if (S.page === 'team') renderTeam(); } catch (e) { fail(e); } }
async function loadChat() {
  if (!studioReady()) return;
  let msgs; try { msgs = await api('/api/chat'); S.chatError = ''; } catch (e) { S.chatError = e.message; return; }
  const first = !S.chatLoaded; S.chatLoaded = true;
  if (first) { S.chat = msgs; softRender(['chat', 'dash']); return; }
  const changed = msgs.length !== S.chat.length || msgs.at(-1)?.id !== S.chat.at(-1)?.id;
  S.chat = msgs;
  if (!changed) return;
  if (S.page === 'chat' || S.page === 'dash') { rerenderChat(); markRead(); }
  else {
    const n = msgs.filter(m => m.id > S.seenChat && m.who !== S.state.me.login).length;
    $('#unread').textContent = n ? n : '';
  }
}
async function loadRemote() {
  try {
    const [r, a] = await Promise.all([api('/api/remote'), api('/api/activity')]);
    S.remote = r; S.activity = a; S.remoteLoaded = true; S.remoteAt = Date.now();
    softRender(['dash']);
  } catch (e) { S.remoteLoaded = true; }
}

async function refresh(all) {
  // don't wipe a modal or a half-typed chat message: only re-render when nothing's open
  await loadState().catch(fail);
  if (!S.state.signedIn) return render();
  if (all) {
    if (!Object.keys(S.templates).length) api('/api/templates').then(t => { S.templates = t; softRender(['board']); }).catch(() => { });
    await Promise.all([loadRemote(), loadBoard(true), loadChat()]); if (!S.team) loadTeam();
  }
  if (!busy()) render();
}

(async () => {
  if (!KEY) { document.body.innerHTML = '<div class="signin panel"><h1>open hq from the app</h1><p class="dim">double-click hq. it opens this page with its key.</p></div>'; return; }
  await refresh(true);
  show(S.state?.signedIn ? (localStorage.getItem('hqPage') || 'dash') : 'dash');
  setInterval(() => S.state?.signedIn && loadChat(), 5000);
  setInterval(() => S.state?.signedIn && loadBoard(), 20000);
  setInterval(() => S.state?.signedIn && loadRemote(), 90000);
  setInterval(() => { if (S.state?.signedIn && document.visibilityState === 'visible' && !busy()) refresh(); }, 15000);
})();
