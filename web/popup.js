"use strict";
(() => {
  const m = location.hash.match(/t=([0-9a-f]{64})/);
  if (m) {
    sessionStorage.setItem("hestia-token", m[1]);
    history.replaceState(null, "", location.pathname);
  }
  const token = sessionStorage.getItem("hestia-token") || "";
  const $ = (id) => document.getElementById(id);

  function h(tag, attrs, ...kids) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v === undefined || v === null || v === false) continue;
      if (k === "class") el.className = v;
      else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
      else if (k === "text") el.textContent = v;
      else el.setAttribute(k, v === true ? "" : v);
    }
    for (const kid of kids.flat()) {
      if (kid === null || kid === undefined || kid === false) continue;
      el.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
    }
    return el;
  }

  async function api(method, path, body) {
    const res = await fetch(path, {
      method, cache: "no-store",
      headers: { "X-Hestia-Token": token, "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
    });
    let data = null;
    try { data = await res.json(); } catch { /* not JSON */ }
    if (!res.ok) throw new Error((data && data.error) || "Request failed");
    return data;
  }

  let toastTimer;
  function toast(msg) {
    let t = document.querySelector(".toast");
    if (!t) { t = h("div", { class: "toast", role: "alert" }); document.body.append(t); }
    t.textContent = msg;
    t.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => t.classList.remove("show"), 3500);
  }

  function duration(since) {
    const s = Math.max(0, (Date.now() - new Date(since).getTime()) / 1000);
    if (s < 90) return "just now";
    if (s < 5400) return `${Math.round(s / 60)} min`;
    if (s < 48 * 3600) return `${Math.round(s / 3600)} h`;
    return `${Math.round(s / 86400)} d`;
  }

  const busy = new Set();
  let state = null;

  function stateText(d) {
    if (!d.online) return "Offline" + (d.since ? ` for ${duration(d.since)}` : "");
    const on = d.switch === "on";
    const known = on || d.switch === "off";
    return (known ? (on ? "On" : "Off") : "Online") + (d.hotkey ? ` · ${d.hotkey}` : "");
  }

  function row(d) {
    const on = d.switch === "on";
    const can = d.online && d.hasKey;
    const sw = h("button", {
      class: "sw" + (busy.has(d.id) ? " busy" : ""), role: "switch",
      "aria-checked": on ? "true" : "false", "aria-label": `${d.name}: turn ${on ? "off" : "on"}`,
      disabled: !can, onclick: () => setSwitch(d, !on),
    });
    return h("div", { class: "row" },
      h("div", { class: "grow" },
        h("div", { class: "name" }, h("span", { class: "dot " + (!d.online ? "down" : on ? "on" : "off") }), d.name),
        h("div", { class: "state", text: stateText(d) })),
      sw);
  }

  function render() {
    if (!state) return;
    if (state.theme) document.documentElement.dataset.theme = state.theme; else delete document.documentElement.dataset.theme;
    const list = $("list");
    const devices = state.devices.filter((d) => d.saved);
    if (!devices.length) {
      list.replaceChildren(h("div", { class: "empty" }, h("b", { text: "No switches yet" }), "Open Hestia to run the test and set one up."));
    } else {
      list.replaceChildren(...devices.map(row));
    }
    const online = devices.filter((d) => d.online).length;
    $("status").textContent = devices.length ? `${online} of ${devices.length} online · Hestia ${state.version}` : `Hestia ${state.version}`;
  }

  async function refresh() {
    try {
      state = await api("GET", "/api/state");
      render();
    } catch (err) { $("status").textContent = err.message; }
  }

  async function setSwitch(d, on) {
    busy.add(d.id); render();
    try {
      await api("POST", `/api/device/${encodeURIComponent(d.id)}/switch`, { on });
      d.switch = on ? "on" : "off";
    } catch (err) { toast(err.message); }
    busy.delete(d.id);
    render();
    refresh();
  }

  async function close() {
    try { await api("POST", "/api/popup/close"); } catch { /* already gone */ }
    window.close();
  }

  $("openBtn").addEventListener("click", async () => {
    try { await api("POST", "/api/window/open"); } catch (err) { toast(err.message); }
  });
  $("quitBtn").addEventListener("click", async () => {
    try { await api("POST", "/api/quit"); } catch { /* already gone */ }
    window.close();
  });
  document.addEventListener("keydown", (e) => { if (e.key === "Escape") close(); });
  // The popup behaves like a menu: it goes away when you click elsewhere.
  window.addEventListener("blur", () => setTimeout(() => { if (!document.hasFocus()) close(); }, 150));

  refresh();
  setInterval(refresh, 2000);
})();
