// Runs the dashboard page's script in a sandbox with a small fake DOM, against a live dashboard server (base URL in
// argv). Drives the Ayla control panel the way the owner does: the farm-map form (409 gate warning, then "Force
// anyway") and the Clear button. Every request goes to the real server with the dashboard's own Origin, as a browser
// sends it. Used by tests/dashboard/test_dashserve.py (OwnerTool.test_panel_script_fake_dom); prints one JSON object.
// Usage: node panel_dom.js <page-script.js> <base-url>
"use strict";
const vm = require("vm");
const fs = require("fs");

const [scriptPath, base] = process.argv.slice(2);
const src = fs.readFileSync(scriptPath, "utf8");
const out = {calls: [], msgs: [], errors: []};

function makeEl(id) {
  const listeners = {};
  return {
    id, value: "", innerHTML: "", textContent: "", className: "", disabled: false, hidden: false,
    dataset: {}, style: {}, options: [], listeners,
    classList: {add() {}, remove() {}, toggle() { return false; }, contains() { return false; }},
    addEventListener(type, fn) { (listeners[type] = listeners[type] || []).push(fn); },
    querySelector() { return makeEl("child"); },
    querySelectorAll() { return []; },
    closest() { return null; },
    setAttribute() {}, getAttribute() { return null; }, appendChild() {}, focus() {}, select() {},
  };
}
const els = new Map();
const byId = id => { if (!els.has(id)) els.set(id, makeEl(id)); return els.get(id); };

const sandbox = {
  document: {getElementById: byId, querySelector: sel => makeEl(sel), querySelectorAll: () => [],
    documentElement: makeEl("html"), body: makeEl("body"), addEventListener() {}},
  window: {addEventListener() {}, history: {replaceState() {}}, location: {hash: "", pathname: "/"}},
  location: {hash: "", pathname: "/", href: base + "/"},
  localStorage: {getItem: () => null, setItem() {}, removeItem() {}},
  getComputedStyle: () => ({getPropertyValue: () => ""}),
  setInterval: () => 0, clearInterval() {}, console,
  fetch: async (url, opts = {}) => {
    const {cache, ...rest} = opts;   // no cache option in node's fetch
    const headers = Object.assign({}, rest.headers || {}, {Origin: base});
    const resp = await fetch(base + url, Object.assign({}, rest, {headers}));
    const text = await resp.clone().text();
    let body = null;
    if (rest.body) { try { body = JSON.parse(rest.body); } catch (e) { body = rest.body; } }
    out.calls.push({method: rest.method || "GET", url, status: resp.status, body, resp: text.slice(0, 400)});
    return resp;
  },
};
const ctx = vm.createContext(sandbox);

// the page's chart code needs real layout; its refreshes fail on this fake DOM. Those are not the panel: record them.
process.on("unhandledRejection", e => { out.errors.push("background: " + String(e && e.message || e).slice(0, 120)); });

const sleep = ms => new Promise(r => setTimeout(r, ms));
async function until(pred, ms = 5000) {
  const end = Date.now() + ms;
  while (!pred() && Date.now() < end) await sleep(20);
  return pred();
}
const submitFarm = () => byId("aylaCtl").listeners.submit[0]({
  target: {closest: sel => (sel === "form[data-kind]" ? {dataset: {kind: "farm_map"}} : null)}, preventDefault() {}});

(async () => {
  try {
    vm.runInContext(src, ctx);                 // the page's own script: init, timers (no-op here), handlers
  } catch (e) { out.errors.push("load: " + e.message); }
  await sleep(300);                            // the first refreshes settle

  // her party, as refreshParties would fill it (base/max level come from it)
  vm.runInContext(`P.data = [{human: "Ayla", map: "pay_dun02", together: 2, online: 2, members: [
    {name: "Ayla", level: 83, online: true}, {name: "Bob", level: 78, online: true}]}];`, ctx);
  if (process.argv[4] === "confirm") {
    // an owner order: open its confirm form, read the prefill, confirm, then see that it has left the list
    vm.runInContext(`refreshRequests();`, ctx);
    await until(() => vm.runInContext(`REQ.list.length > 0`, ctx));
    out.listed = vm.runInContext(`REQ.list.map(r => r.id)`, ctx);
    byId("ctlReqKind").value = "farm_map";   // the owner's pick in the kind select (the fake DOM has no option defaults)
    vm.runInContext(`openReq("r_fresh");`, ctx);
    out.prefill = {value: byId("ctlReqValue").value, kind: byId("ctlReqKind").value, hours: byId("ctlReqHours").value,
      text: byId("ctlReqText").textContent, shown: !byId("ctlReqForm").hidden};
    byId("aylaCtl").listeners.submit[0]({target: {closest: sel => (sel === "form[data-kind]" ? {dataset: {kind: "request"}} : null)},
      preventDefault() {}});
    await until(() => out.calls.filter(c => c.method === "POST").length >= 2);
    await until(() => byId("ctlMsg").innerHTML.includes("stored") || byId("ctlMsg").innerHTML.includes("not"));
    out.msgs.push(byId("ctlMsg").innerHTML);
    out.formHidden = byId("ctlReqForm").hidden;
    await sleep(200);
    vm.runInContext(`refreshRequests();`, ctx);
    await sleep(400);
    out.listedAfter = vm.runInContext(`REQ.list.map(r => r.id)`, ctx);
    process.stdout.write(JSON.stringify(out) + "\n");
    process.exit(0);
  }
  vm.runInContext(`$("ctlMap").value = "moc_fild12"; $("ctlFarmHours").value = "24";`, ctx);

  // 1. the gate answers 409: a warning and a "Force anyway" button, nothing stored
  submitFarm();
  await until(() => out.calls.some(c => c.method === "POST"));
  await until(() => byId("ctlMsg").innerHTML.includes("Force anyway"));
  out.msgs.push(byId("ctlMsg").innerHTML);

  // 2. "Force anyway" re-POSTs the same body with confirm=true
  const force = {target: {closest: sel => (sel === "button[data-force]" ? {dataset: {force: "1"}} : null)}};
  byId("ctlMsg").listeners.click[0](force);
  await until(() => out.calls.filter(c => c.method === "POST").length >= 2);
  await until(() => byId("ctlMsg").innerHTML.includes("stored"));
  out.msgs.push(byId("ctlMsg").innerHTML);

  // 3. Clear the stored override (DELETE with the id the engine gave)
  const posts = out.calls.filter(c => c.method === "POST");
  const stored = posts.length ? JSON.parse(posts[posts.length - 1].resp) : {};
  const clear = {target: {closest: sel => (sel === "button[data-clear]" ? {dataset: {clear: stored.id || ""}} : null)}};
  byId("ctlActive").listeners.click[0](clear);
  await until(() => byId("ctlMsg").innerHTML.includes("cleared") || byId("ctlMsg").innerHTML.includes("not cleared"));
  out.msgs.push(byId("ctlMsg").innerHTML);

  // the page's own copy of the last text it showed the owner (no raw token or stack in it)
  out.stateHtml = byId("ctlState").innerHTML.slice(0, 200);
  out.flagsHtml = byId("ctlFlags").innerHTML;
  process.stdout.write(JSON.stringify(out) + "\n");
  process.exit(0);
})().catch(e => { out.errors.push(String(e && e.stack || e)); process.stdout.write(JSON.stringify(out) + "\n"); process.exit(1); });
