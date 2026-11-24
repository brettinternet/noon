// Noon web viewer: one event stream per page, live swaps of [data-swap]
// sections, log lines, local times, tab status, menus, action confirmations,
// and keyboard shortcuts.
'use strict';

const root = document.documentElement;
root.classList.add('js');
const assets = root.dataset.assets;
let source = null;
let seq = root.dataset.seq;

const clockFormat = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' });
const weekFormat = new Intl.DateTimeFormat(undefined, { weekday: 'short', hour: 'numeric', minute: '2-digit' });
const dateFormat = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' });
const fullFormat = new Intl.DateTimeFormat(undefined, { dateStyle: 'full', timeStyle: 'long' });
const relativeFormat = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto', style: 'narrow' });

function relative(date) {
  const seconds = (date - Date.now()) / 1000;
  const size = Math.abs(seconds);
  if (size < 60) return seconds > 0 ? relativeFormat.format(1, 'minute') : relativeFormat.format(0, 'second');
  for (const [unit, length] of [['day', 86400], ['hour', 3600], ['minute', 60]]) {
    if (size >= length) return relativeFormat.format(Math.trunc(seconds / length), unit);
  }
}

// The clock time today, the weekday and time within a week, else the date.
function compact(date) {
  const days = Math.abs(date - Date.now()) / 86400000;
  if (date.toDateString() === new Date().toDateString()) return clockFormat.format(date);
  return days < 6 ? weekFormat.format(date) : dateFormat.format(date);
}

// Render every <time> in the browser's timezone: a relative time, then the
// compact absolute one unless the element asks for relative only.
function localTimes(scope) {
  for (const element of scope.querySelectorAll('time[datetime]')) {
    const date = new Date(element.getAttribute('datetime'));
    if (Number.isNaN(date.getTime())) continue;
    if ('relative' in element.dataset) {
      element.textContent = relative(date);
    } else {
      const at = document.createElement('span');
      at.className = 'at';
      at.textContent = compact(date);
      element.replaceChildren(relative(date), at);
    }
    element.title = fullFormat.format(date);
  }
}

// Running durations count up, formatted like the server's ("1m5s").
function elapsed(scope) {
  for (const element of scope.querySelectorAll('[data-since]')) {
    const total = Math.max(0, Math.floor((Date.now() - new Date(element.dataset.since)) / 1000));
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    const seconds = total % 60;
    element.textContent = hours ? `${hours}h${minutes}m${seconds}s` : minutes ? `${minutes}m${seconds}s` : `${seconds}s`;
  }
}

function announce(text) {
  const region = document.getElementById('announce');
  if (region) region.textContent = text;
}

function tabStatus(failing) {
  const count = Number(failing);
  document.title = (count ? `✗ ${count} · ` : '') + root.dataset.title;
  const icon = document.querySelector('link[rel=icon]');
  if (icon) icon.href = assets + (count ? 'favicon-failing.svg' : 'favicon-ok.svg');
  if (count !== Number(root.dataset.failing)) {
    root.dataset.failing = count;
    announce(count ? `${count} failing` : 'Nothing failing');
  }
}

function stale(on) {
  const element = document.getElementById('stale');
  if (element) element.hidden = !on;
}

// fetchPage returns null when the page is unavailable, so callers carry on
// (and reconnect) while the server is down.
async function fetchPage(url) {
  let response;
  try {
    response = await fetch(url, { headers: { Accept: 'text/html' } });
  } catch {
    return null;
  }
  // A lapsed session redirects to sign-in; let the browser follow it.
  if (response.redirected || response.status === 401) {
    location.reload();
    return null;
  }
  if (!response.ok) return null;
  return new DOMParser().parseFromString(await response.text(), 'text/html');
}

// A swap waits while the user is inside a menu, field, or dialog, so it never
// closes or clears what they are working in.
let deferred = false;
function busy() {
  if (document.querySelector('dialog[open], [data-swap] details[open]')) return true;
  const active = document.activeElement;
  return Boolean(active?.closest('[data-swap]') && active.matches('input, select, textarea'));
}

function resume() {
  setTimeout(() => {
    if (deferred && !busy()) {
      deferred = false;
      refresh(false);
    }
  }, 0);
}

// The focused job row, or the control within it, survives a swap.
function focusKey() {
  const active = document.activeElement;
  const row = active?.closest('tr[data-job]');
  if (!row) return null;
  return { job: row.dataset.job, key: active === row ? null : active.dataset.key };
}

function restoreFocus(saved) {
  if (!saved) return;
  const row = [...document.querySelectorAll('tr[data-job]')].find((element) => element.dataset.job === saved.job);
  if (!row) return;
  const control = saved.key && row.querySelector(`[data-key="${saved.key}"]`);
  if (control) {
    control.focus({ preventScroll: true });
  } else {
    row.tabIndex = -1;
    row.focus({ preventScroll: true });
  }
}

// Replace the live parts of the page. The run log is replaced only on
// reconnect, because the stream appends to it in place.
async function refresh(withLog) {
  if (!withLog && busy()) {
    deferred = true;
    return;
  }
  deferred = false;
  const page = await fetchPage(location.href);
  if (!page) return;
  const saved = focusKey();
  const before = document.querySelector('[data-status]')?.dataset.status;
  for (const element of document.querySelectorAll('[data-swap]')) {
    const next = page.getElementById(element.id);
    if (next) {
      element.replaceWith(next);
      localTimes(next);
      elapsed(next);
    }
  }
  restoreFocus(saved);
  const after = document.querySelector('[data-status]')?.dataset.status;
  if (before && after && before !== after) announce(after.replaceAll('_', ' '));
  const log = document.getElementById('log');
  const nextLog = page.getElementById('log');
  if (withLog && log && nextLog) {
    log.replaceWith(nextLog);
    localTimes(nextLog);
    reachAnchor();
  }
  seq = page.documentElement.dataset.seq;
}

function appendLine(data) {
  const section = document.getElementById('log');
  if (!section || !section.dataset.next) return;
  const box = section.querySelector('.log');
  const atBottom = window.innerHeight + window.scrollY >= document.body.scrollHeight - 8;
  const number = Number(section.dataset.next);
  const line = document.createElement('div');
  line.className = 'line';
  line.id = `L${number}`;
  const link = document.createElement('a');
  link.href = `#L${number}`;
  link.textContent = number;
  const text = document.createElement('span');
  // The server escapes the line and allows only style classes.
  text.innerHTML = data.html;
  line.append(link, text);
  box.append(line);
  section.dataset.next = number + 1;
  section.dataset.after = data.end;
  section.querySelector('.empty')?.remove();
  if (atBottom) line.scrollIntoView({ block: 'end' });
}

let failures = 0;

function disconnect() {
  if (source) source.close();
  source = null;
}

function connect() {
  if (document.hidden) return;
  disconnect();
  const log = document.getElementById('log');
  let url = '/events';
  if (log && log.dataset.after) url += `?run=${log.dataset.run}&after=${log.dataset.after}`;
  const stream = new EventSource(url);
  source = stream;
  stream.addEventListener('state', (event) => {
    failures = 0;
    stale(false);
    const state = JSON.parse(event.data);
    if (state.version !== root.dataset.version) {
      location.reload();
      return;
    }
    tabStatus(state.failing);
    if (String(state.seq) !== String(seq)) {
      seq = String(state.seq);
      refresh(false);
    }
  });
  stream.addEventListener('log', (event) => appendLine(JSON.parse(event.data)));
  stream.addEventListener('end', () => {
    const section = document.getElementById('log');
    if (!section) return;
    delete section.dataset.after;
    delete section.dataset.next;
    const empty = section.querySelector('.empty');
    if (empty) empty.textContent = 'No output.';
  });
  // Reconnect ourselves, so the stream resumes from the latest log offset.
  stream.onerror = () => {
    if (source !== stream) return;
    disconnect();
    // A single drop reconnects quietly; a failed retry shows as stale.
    failures += 1;
    if (failures > 1) stale(true);
    setTimeout(() => {
      if (!document.hidden && !source) refresh(true).then(connect);
    }, 3000);
  };
}

// Fetch earlier log pages until the anchored line is on the page.
async function reachAnchor() {
  document.querySelector('.line.target')?.classList.remove('target');
  const match = /^#L(\d+)$/.exec(location.hash);
  if (!match) return;
  const wanted = Number(match[1]);
  let target = document.getElementById(`L${wanted}`);
  while (!target) {
    const earlier = document.querySelector('#log .earlier');
    const first = document.querySelector('#log .line');
    if (!earlier || (first && Number(first.id.slice(1)) <= wanted)) return;
    const page = await fetchPage(earlier.href);
    if (!page) return;
    document.querySelector('#log .log').prepend(...page.querySelectorAll('#log .line'));
    const next = page.querySelector('#log .earlier');
    if (next) earlier.setAttribute('href', next.getAttribute('href'));
    else earlier.parentElement.remove();
    target = document.getElementById(`L${wanted}`);
  }
  target.classList.add('target');
  target.scrollIntoView({ block: 'center' });
}

function closeMenus(except) {
  for (const menu of document.querySelectorAll('details.menu[open]')) {
    if (menu !== except) menu.open = false;
  }
}

document.addEventListener('click', async (event) => {
  // Clicking outside a menu closes it; opening one closes the others.
  closeMenus(event.target.closest('details.menu'));

  // A linked row opens its link from anywhere but its own controls.
  const row = event.target.closest('tr.linked');
  if (row && !event.target.closest('a, button, input, select, summary, label') && !getSelection().toString()) {
    const link = row.querySelector('.row-link');
    if (link && (event.metaKey || event.ctrlKey)) window.open(link.href, '_blank');
    else link?.click();
    return;
  }

  const button = event.target.closest('[data-copy]');
  if (!button) return;
  try {
    await navigator.clipboard.writeText(button.dataset.copy);
    button.textContent = 'Copied';
  } catch {
    button.textContent = 'Copy failed';
  }
  setTimeout(() => { button.textContent = 'Copy'; }, 2000);
});

// Filters apply as soon as they change; without script, the button does.
document.addEventListener('change', (event) => {
  const form = event.target.closest('form.filter');
  if (form) form.requestSubmit();
});

document.addEventListener('visibilitychange', () => {
  if (document.hidden) disconnect();
  else refresh(true).then(connect);
});

// Forms with data-confirm ask in a dialog, then resubmit with confirm=1 and
// the clicked button's value. Without script the server asks instead.
document.addEventListener('submit', (event) => {
  const form = event.target;
  const dialog = document.getElementById('confirm');
  if (!form.dataset.confirm || !dialog || form.querySelector('input[name=confirm]')) return;
  event.preventDefault();
  const submitter = event.submitter;
  dialog.querySelector('p').textContent = form.dataset.confirm;
  dialog.returnValue = '';
  dialog.addEventListener('close', () => {
    if (dialog.returnValue !== 'ok') return;
    const add = (name, value) => {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = name;
      input.value = value;
      form.append(input);
    };
    add('confirm', '1');
    if (submitter?.name) add(submitter.name, submitter.value);
    form.submit();
  }, { once: true });
  dialog.showModal();
});

// j and k move between job rows; r, p, and c act on the focused row, or on
// the page's run when no row is focused. Escape closes an open menu.
document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape') {
    const menu = document.querySelector('details.menu[open]');
    if (!menu) return;
    menu.open = false;
    menu.querySelector('summary').focus();
    event.preventDefault();
    return;
  }
  if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey) return;
  if (event.target.closest('input, select, textarea, [contenteditable]') || document.querySelector('dialog[open]')) return;
  const row = document.activeElement?.closest('tr[data-job]');
  if (event.key === '?') {
    document.getElementById('help')?.showModal();
  } else if (event.key === 'j' || event.key === 'k') {
    const rows = [...document.querySelectorAll('tr[data-job]')];
    if (!rows.length) return;
    const index = rows.indexOf(row);
    const next = rows[index < 0 ? 0 : Math.min(Math.max(index + (event.key === 'j' ? 1 : -1), 0), rows.length - 1)];
    next.tabIndex = -1;
    next.focus();
  } else if (['r', 'p', 'c'].includes(event.key)) {
    const target = [...(row || document).querySelectorAll(`[data-key="${event.key}"]`)]
      .find((element) => row || !element.closest('tr[data-job]'));
    if (!target) return;
    target.click();
    // An opened menu takes focus, so Tab or Enter picks a choice.
    if (target.matches('summary') && target.parentElement.open) target.parentElement.querySelector('.popover button')?.focus();
  } else {
    return;
  }
  event.preventDefault();
});

document.addEventListener('toggle', (event) => {
  // An opened "Later" menu is for typing a time.
  if (event.target.matches?.('details.menu[open]')) event.target.querySelector('.popover input:not([type=hidden])')?.focus();
  resume();
}, true);
document.addEventListener('focusout', resume);
document.addEventListener('close', resume, true);

// Flash codes are for this visit only; drop them so reloads stay quiet.
const params = new URLSearchParams(location.search);
if (params.has('done')) {
  params.delete('done');
  const query = params.toString();
  history.replaceState(history.state, '', location.pathname + (query ? `?${query}` : '') + location.hash);
}

window.addEventListener('hashchange', reachAnchor);
setInterval(() => localTimes(document), 30000);
setInterval(() => elapsed(document), 1000);
localTimes(document);
elapsed(document);
reachAnchor();
connect();
