// Runs the dashboard page's script in a sandbox with a small fake DOM, against a live dashboard server (base URL in
// argv), the way panel_dom.js does for the owner panel. Opens the Market tab (DB11) and lets its refresh fill the five
// cards. Prints one JSON object: each card's HTML and the note line. Used by tests/dashboard/test_market.py (MarketDom).
// Usage: node market_dom.js <page-script.js> <base-url>
"use strict";
const vm = require("vm");
const fs = require("fs");

const [scriptPath, base] = process.argv.slice(2);
const src = fs.readFileSync(scriptPath, "utf8");
const out = {errors: []};

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
    return fetch(base + url, Object.assign({}, rest, {headers}));
  },
};
const ctx = vm.createContext(sandbox);
process.on("unhandledRejection", e => { out.errors.push("background: " + String(e && e.message || e).slice(0, 120)); });

(async () => {
  try {
    vm.runInContext(src, ctx);   // the page's own script: init, timers (no-op here), handlers
  } catch (e) { out.errors.push("load: " + e.message); }
  await new Promise(r => setTimeout(r, 300));
  vm.runInContext("showTab('market');", ctx);
  await vm.runInContext("refreshMarket()", ctx);
  out.cards = {};
  for (const id of ["mkEngine", "mkFarmers", "mkPrices", "mkBroadcast", "mkFacts"]) out.cards[id] = byId(id).innerHTML;
  out.note = byId("mkNote").textContent;
  out.tabShown = vm.runInContext("curTab", ctx);
  process.stdout.write(JSON.stringify(out) + "\n");
  process.exit(0);
})().catch(e => { out.errors.push(String(e && e.stack || e)); process.stdout.write(JSON.stringify(out) + "\n"); process.exit(1); });
