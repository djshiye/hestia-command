"use strict";
(() => {
  // ---- session token (passed in the URL fragment, never sent to the network) ----
  const m = location.hash.match(/t=([0-9a-f]{64})/);
  if (m) {
    sessionStorage.setItem("hestia-token", m[1]);
    history.replaceState(null, "", location.pathname);
  }
  const token = sessionStorage.getItem("hestia-token") || "";

  const $ = (id) => document.getElementById(id);
  // Build elements with textContent only: device data comes from the network.
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
  const SVGNS = "http://www.w3.org/2000/svg";
  function powerIcon() {
    const s = document.createElementNS(SVGNS, "svg");
    s.setAttribute("viewBox", "0 0 24 24");
    s.setAttribute("aria-hidden", "true");
    const p = document.createElementNS(SVGNS, "path");
    p.setAttribute("d", "M12 3v9M6.3 6.8a8 8 0 1 0 11.4 0");
    p.setAttribute("fill", "none");
    p.setAttribute("stroke", "currentColor");
    p.setAttribute("stroke-width", "2.2");
    p.setAttribute("stroke-linecap", "round");
    s.append(p);
    return s;
  }

  async function api(method, path, body) {
    const res = await fetch(path, {
      method,
      headers: { "X-Hestia-Token": token, "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
      cache: "no-store",
    });
    let data = null;
    try { data = await res.json(); } catch { /* not JSON */ }
    if (!res.ok) throw new Error((data && data.error) || (res.status === 403 ? "This window has expired. Open Hestia again." : "Request failed"));
    return data;
  }

  let toastTimer;
  function toast(msg, bad) {
    const t = $("toast");
    t.textContent = msg;
    t.className = "toast" + (bad ? " bad" : "");
    t.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { t.hidden = true; }, bad ? 5000 : 2600);
  }

  const ago = (iso) => {
    if (!iso) return "never";
    const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
    if (s < 10) return "just now";
    if (s < 60) return Math.round(s) + " s ago";
    if (s < 3600) return Math.round(s / 60) + " min ago";
    if (s < 86400) return Math.round(s / 3600) + " h ago";
    return Math.round(s / 86400) + " d ago";
  };
  const duration = (iso) => {
    if (!iso) return "";
    let s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
    const d = Math.floor(s / 86400); s -= d * 86400;
    const hh = Math.floor(s / 3600); s -= hh * 3600;
    const mm = Math.floor(s / 60);
    return d ? `${d}d ${hh}h` : hh ? `${hh}h ${mm}m` : `${mm}m`;
  };
  const clock = (iso) => new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });

  // ---- navigation ----
  let tab = sessionStorage.getItem("hestia-tab") || "test";
  function show(name) {
    tab = name;
    sessionStorage.setItem("hestia-tab", name);
    for (const s of document.querySelectorAll(".tab")) s.hidden = s.id !== "tab-" + name;
    for (const b of document.querySelectorAll(".step")) b.classList.toggle("active", b.dataset.tab === name);
    if (state) render();
  }
  for (const b of document.querySelectorAll(".step")) b.addEventListener("click", () => show(b.dataset.tab));

  // ---- state ----
  let state = null;
  let lastTest = null;
  const busy = new Set();

  function controlReady(s) {
    return s.testPassed && s.devices.some((d) => d.verified);
  }

  function renderSteps(s) {
    const steps = document.querySelectorAll(".step");
    const verified = s.devices.some((d) => d.verified);
    const ready = controlReady(s);
    steps[0].classList.toggle("done", s.testPassed);
    steps[1].classList.toggle("done", verified);
    steps[2].classList.toggle("is-locked", !ready);
    $("stepTestSub").textContent = s.testPassed ? "Passed" : "Check the network";
    const keys = s.devices.filter((d) => d.hasKey).length;
    $("stepSetupSub").textContent = verified ? "Key confirmed" : keys ? "Key saved, confirming" : "Save the switch key";
    $("stepControlSub").textContent = ready ? "Ready" : "Unlocks after 1 and 2";
  }

  function statusEl(d) {
    if (!d.discovered && !d.manualIp) return h("span", { class: "status bad", text: "Not found" });
    return d.online ? h("span", { class: "status on", text: "Online" }) : h("span", { class: "status bad", text: "Offline" });
  }

  function renderTest(s) {
    const seen = s.devices.filter((d) => d.discovered);
    $("scanInfo").textContent = `Listening on ${s.interfaces.length ? s.interfaces.join(", ") : "no network"}`;
    const list = $("seenList");
    list.replaceChildren();
    if (!seen.length) {
      list.append(h("div", { class: "empty" },
        h("b", { text: "No Sonoff switches found yet" }),
        "Hestia keeps listening. Run the test to ask every switch to announce itself."));
    } else {
      list.append(h("table", { class: "table" },
        h("thead", {}, h("tr", {}, ["Switch", "Address", "Type", "Local mode", "Key", "Last seen"].map((t) => h("th", { text: t })))),
        h("tbody", {}, seen.map((d) => h("tr", {},
          h("td", {}, h("div", { text: d.name }), h("div", { class: "muted small mono", text: d.id })),
          h("td", { class: "mono", text: `${d.ip}:${d.port || 8081}` }),
          h("td", { text: d.type || "unknown" }),
          h("td", {}, h("span", { class: "tag good", text: d.encrypted ? "Yes, encrypted" : "Yes, open" })),
          h("td", {}, d.verified ? h("span", { class: "tag good", text: "Confirmed" }) : d.hasKey ? h("span", { class: "tag warn", text: "Saved" }) : h("span", { class: "tag", text: "Not yet" })),
          h("td", {}, statusEl(d), h("div", { class: "muted small", text: ago(d.lastSeen) })),
        )))));
    }
    const res = $("testResult");
    if (lastTest) {
      res.hidden = false;
      res.replaceChildren(h("div", { class: "card" },
        h("div", { class: "banner " + (lastTest.passed ? "pass" : "fail") },
          h("span", { class: "ico big " + (lastTest.passed ? "pass" : "fail"), text: lastTest.passed ? "✓" : "!" }),
          h("div", {},
            h("h2", { text: lastTest.passed ? "Test passed: this switch supports local control" : "Test did not pass yet" }),
            h("div", { class: "muted", text: lastTest.passed
              ? (s.devices.some((d) => d.verified) ? "Everything is ready. Open Control." : "Next, save the switch's key in Setup.")
              : "Check that this PC is on the same Wi-Fi as the switch (not a guest network), allow Hestia through the Windows firewall if asked, then run the test again." }),
            lastTest.passed ? h("div", { class: "row" }, h("button", { class: "btn primary", text: s.devices.some((d) => d.verified) ? "Go to Control" : "Go to Setup", onclick: () => show(s.devices.some((d) => d.verified) ? "control" : "setup") })) : null,
          )),
        h("ul", { class: "checks" }, lastTest.checks.map((c) => h("li", {},
          h("span", { class: "ico " + c.status, text: c.status === "pass" ? "✓" : c.status === "fail" ? "✕" : "!" }),
          h("div", {}, h("div", { text: c.name }), h("div", { class: "detail", text: c.detail }))))),
      ));
    } else if (s.testPassed) {
      res.hidden = false;
      res.replaceChildren(h("div", { class: "banner pass" },
        h("span", { class: "ico big pass", text: "✓" }),
        h("div", {}, h("h2", { text: "A previous test passed on " + new Date(s.lastTest).toLocaleString() }),
          h("div", { class: "muted", text: "You can run it again at any time." }))));
    } else {
      res.hidden = true;
    }
  }

  function renderSetup(s) {
    const saved = s.devices.filter((d) => d.saved);
    const list = $("savedList");
    list.replaceChildren();
    if (!saved.length) {
      list.append(h("div", { class: "empty" }, h("b", { text: "No keys saved yet" }), "Follow the steps above."));
      return;
    }
    list.append(h("table", { class: "table" },
      h("thead", {}, h("tr", {}, ["Switch", "Key", "On network", ""].map((t) => h("th", { text: t })))),
      h("tbody", {}, saved.map((d) => h("tr", {},
        h("td", {}, h("div", { text: d.name }), h("div", { class: "muted small mono", text: d.id })),
        h("td", {}, d.verified ? h("span", { class: "tag good", text: "Confirmed" })
          : d.decryptError ? h("span", { class: "tag bad", text: "Wrong key" })
          : h("span", { class: "tag warn", text: "Saved, waiting for the switch" })),
        h("td", {}, statusEl(d)),
        h("td", { class: "small actions" },
          h("button", { class: "btn small", text: "Show key", onclick: () => revealKey(d) }),
          h("button", { class: "btn small", text: "Remove", onclick: () => removeDevice(d) })),
      )))));
  }

  function lockReason(ok, text, action, label) {
    return h("li", {}, h("span", { class: "ico " + (ok ? "pass" : "warn"), text: ok ? "✓" : "•" }), h("span", { text }),
      !ok && action ? h("button", { class: "btn small", text: label, onclick: action }) : null);
  }

  function renderControl(s) {
    const ready = controlReady(s);
    $("locked").hidden = ready;
    $("controlArea").hidden = !ready;
    if (!ready) {
      $("lockReasons").replaceChildren(
        lockReason(s.testPassed, "Network test passed", () => show("test"), "Run test"),
        lockReason(s.devices.some((d) => d.verified), "A switch key is saved and confirmed", () => show("setup"), "Open Setup"),
      );
      return;
    }
    $("bgToggle").checked = s.background;
    $("autostartRow").hidden = !s.autostartSupported;
    $("autostartToggle").checked = s.autostart;
    const tiles = $("tiles");
    if (tiles.contains(document.activeElement) && document.activeElement.tagName !== "BUTTON") return;
    tiles.replaceChildren(...s.devices.filter((d) => d.saved).map((d) => tile(d, s)));
  }

  function tile(d, s) {
    const on = d.switch === "on";
    const known = d.switch === "on" || d.switch === "off";
    const canControl = (d.discovered || d.manualIp) && d.hasKey;
    const name = h("input", { class: "tile-name", value: d.name, maxlength: "40", "aria-label": "Switch name", spellcheck: "false",
      onchange: (e) => saveSetting(d.id, { name: e.target.value }), onkeydown: (e) => { if (e.key === "Enter") e.target.blur(); } });
    const power = h("button", {
      class: "power" + (on ? " on" : "") + (busy.has(d.id) ? " busy" : "") + (!d.online ? " offline" : ""),
      "aria-pressed": on ? "true" : "false",
      "aria-label": `Turn ${d.name} ${on ? "off" : "on"}`,
      disabled: !canControl,
      onclick: () => setSwitch(d, !on),
    }, powerIcon());
    const hotkey = h("select", { "aria-label": "Keyboard shortcut", disabled: !s.hotkeysSupported, onchange: (e) => saveSetting(d.id, { hotkey: e.target.value }) },
      h("option", { value: "", text: s.hotkeysSupported ? "No shortcut" : "Windows only" }),
      s.hotkeyChoices.map((k) => h("option", { value: k, text: k, selected: k === d.hotkey })));
    const manual = h("input", { value: d.manualIp || "", placeholder: "Automatic", spellcheck: "false", "aria-label": "Fixed address",
      onchange: (e) => saveSetting(d.id, { manualIp: e.target.value }) });
    return h("div", { class: "card tile" },
      h("div", { class: "tile-top" },
        h("div", { class: "grow" }, name,
          h("div", { class: "tile-meta" }, statusEl(d), " ", h("span", { class: "mono", text: d.ip ? ` ${d.ip}` : "" }),
            d.since ? h("span", { text: ` · ${d.online ? "up" : "down"} ${duration(d.since)}` }) : null))),
      power,
      h("div", { class: "power-label muted", text: known ? (on ? "On" : "Off") : "State unknown" }),
      h("div", { class: "tile-foot" },
        h("label", {}, "Shortcut", hotkey),
        h("label", {}, "Fixed address", manual),
        d.hotkeyError ? h("div", { class: "err", text: d.hotkeyError }) : null),
    );
  }

  function renderActivity(s) {
    const st = $("stability");
    const saved = s.devices.filter((d) => d.saved || d.discovered);
    st.replaceChildren(saved.length ? h("table", { class: "table" },
      h("thead", {}, h("tr", {}, ["Switch", "Now", "Uptime", "Drop-offs"].map((t) => h("th", { text: t })))),
      h("tbody", {}, saved.map((d) => h("tr", {},
        h("td", {}, h("div", { text: d.name }), h("div", { class: "muted small mono", text: d.id })),
        h("td", {}, statusEl(d), d.since ? h("div", { class: "muted small", text: "for " + duration(d.since) }) : null),
        h("td", { text: d.uptime != null ? d.uptime.toFixed(1) + "%" : "measuring" }),
        h("td", {}, h("span", { class: "tag " + (d.drops ? "warn" : "good"), text: String(d.drops) })),
      )))) : h("div", { class: "empty", text: "No switches yet." }));
    const ev = $("events");
    const items = s.events.slice().reverse();
    ev.replaceChildren(...(items.length ? items.map((e) => h("li", {},
      h("time", { text: clock(e.t) }), h("span", { class: "k " + e.kind }),
      h("span", {}, e.text, e.device ? h("span", { class: "dev mono", text: e.device }) : null))) : [h("li", { class: "empty", text: "Nothing yet." })]));
  }

  function renderUpdate(s) {
    const u = s.update || {};
    $("updateRow").hidden = !u.supported;
    $("updateBanner").hidden = !u.ready;
    if (u.ready) {
      $("updateTitle").textContent = `Hestia ${u.ready} is ready to install`;
    }
    if (!u.supported) return;
    const t = $("autoUpdateToggle");
    if (document.activeElement !== t) t.checked = !!u.enabled;
    let status;
    if (u.checking) status = "Checking\u2026";
    else if (u.downloading) status = `Downloading ${u.available}\u2026`;
    else if (u.ready) status = `${u.ready} ready`;
    else if (u.error) status = u.error;
    else if (u.available) status = `${u.available} available`;
    else if (u.lastCheck && !u.lastCheck.startsWith("0001")) status = `Up to date, checked ${ago(u.lastCheck)}`;
    else status = "Not checked yet";
    $("updateStatus").textContent = status;
    $("updateCheck").disabled = !!u.checking || !!u.downloading;
  }

  function renderInstall(s) {
    const i = s.install || {};
    const show = i.supported && !i.installed && sessionStorage.getItem("hestia-install-later") !== "1";
    $("installBanner").hidden = !show;
    if (!show) return;
    $("installTitle").textContent = i.present ? "Update the installed Hestia" : "Install Hestia on this PC";
    $("installText").textContent = i.present
      ? `Replaces the installed copy with this version (${s.version}). Your switches, keys and settings are kept.`
      : "Adds Hestia to your Start menu and desktop. No admin rights needed, and you can uninstall it from Windows Settings like any app.";
    $("installBtn").textContent = i.present ? "Update" : "Install";
  }

  function applyTheme(theme) {
    const root = document.documentElement;
    if (theme) root.dataset.theme = theme; else delete root.dataset.theme;
    try { if (theme) localStorage.setItem("hestia-theme", theme); else localStorage.removeItem("hestia-theme"); } catch { /* private mode */ }
    for (const b of document.querySelectorAll("#themeSeg button")) b.setAttribute("aria-checked", b.dataset.theme === theme ? "true" : "false");
  }

  function render() {
    const s = state;
    applyTheme(s.theme || "");
    renderInstall(s);
    renderUpdate(s);
    renderSteps(s);
    $("ver").textContent = "Hestia " + s.version;
    if (tab === "test") renderTest(s);
    if (tab === "setup") renderSetup(s);
    if (tab === "control") renderControl(s);
    if (tab === "activity") renderActivity(s);
  }

  async function refresh() {
    try {
      state = await api("GET", "/api/state");
      render();
    } catch (e) {
      toast(e.message, true);
    }
  }

  // ---- actions ----
  $("runTest").addEventListener("click", async (e) => {
    const b = e.currentTarget;
    b.disabled = true;
    b.replaceChildren(h("span", { class: "spinner" }), "Testing…");
    try {
      lastTest = await api("POST", "/api/test");
      await refresh();
    } catch (err) { toast(err.message, true); }
    b.disabled = false;
    b.replaceChildren("Run test again");
  });

  let polling = null;
  $("readKey").addEventListener("click", async (e) => {
    const b = e.currentTarget;
    if (polling) { clearInterval(polling); polling = null; b.textContent = "Read key from switch"; $("readStatus").textContent = ""; return; }
    const started = Date.now();
    b.replaceChildren(h("span", { class: "spinner" }), "Waiting… (click to stop)");
    $("readStatus").textContent = "Looking for the switch's setup hotspot";
    const attempt = async () => {
      try {
        const r = await api("POST", "/api/setup/read");
        if (r.found) {
          clearInterval(polling); polling = null;
          b.textContent = "Read key from switch";
          $("readStatus").textContent = "";
          toast(`Key saved for switch ${r.deviceid}`);
          refresh();
          return;
        }
      } catch (err) { $("readStatus").textContent = err.message; }
      if (Date.now() - started > 180000) {
        clearInterval(polling); polling = null;
        b.textContent = "Read key from switch";
        $("readStatus").textContent = "No switch hotspot found after 3 minutes. Check steps 1 and 2, then try again.";
      }
    };
    polling = setInterval(attempt, 2500);
    attempt();
  });

  $("manualForm").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = new FormData(e.target);
    try {
      await api("POST", "/api/setup/manual", { id: f.get("id"), key: f.get("key") });
      e.target.reset();
      toast("Key saved");
      refresh();
    } catch (err) { toast(err.message, true); }
  });

  async function revealKey(d) {
    if (!window.confirm(`Show the key for ${d.name}? Anyone who sees it can control this switch on your network. Use it to set up another PC, for example one connected by cable.`)) return;
    try {
      const r = await api("POST", `/api/device/${encodeURIComponent(d.id)}/reveal`);
      const box = $("keyReveal");
      const copy = h("button", { class: "btn small", text: "Copy key", onclick: async () => {
        try { await navigator.clipboard.writeText(r.key); toast("Key copied"); } catch { toast("Select the key and press Ctrl+C", true); }
      } });
      box.replaceChildren(
        h("div", {}, h("b", { text: "Device ID " }), h("span", { class: "mono", text: r.id })),
        h("div", {}, h("b", { text: "Key " }), h("span", { class: "mono", text: r.key })),
        h("div", { class: "row" }, copy, h("button", { class: "btn small", text: "Hide", onclick: () => { box.hidden = true; box.replaceChildren(); } })),
        h("p", { class: "muted small", text: "On the other PC, open Setup, choose \"Enter a key by hand\" and paste these. The key hides itself in 60 seconds." }));
      box.hidden = false;
      setTimeout(() => { box.hidden = true; box.replaceChildren(); }, 60000);
    } catch (err) { toast(err.message, true); }
  }

  async function removeDevice(d) {
    if (!window.confirm(`Remove ${d.name} and delete its saved key from this PC?`)) return;
    try { await api("POST", `/api/device/${encodeURIComponent(d.id)}/remove`); toast("Removed"); refresh(); }
    catch (err) { toast(err.message, true); }
  }

  async function setSwitch(d, on) {
    busy.add(d.id); render();
    try {
      await api("POST", `/api/device/${encodeURIComponent(d.id)}/switch`, { on });
      d.switch = on ? "on" : "off";
    } catch (err) { toast(err.message, true); }
    busy.delete(d.id);
    refresh();
  }

  async function saveSetting(id, body) {
    try {
      await api("POST", `/api/device/${encodeURIComponent(id)}/settings`, body);
      toast("Saved");
      document.activeElement && document.activeElement.blur();
      refresh();
    } catch (err) { toast(err.message, true); refresh(); }
  }

  $("bgToggle").addEventListener("change", async (e) => {
    try { await api("POST", "/api/settings", { background: e.target.checked }); toast(e.target.checked ? "Hestia will stay in the tray" : "Hestia will close with this window"); }
    catch (err) { toast(err.message, true); }
  });
  $("autostartToggle").addEventListener("change", async (e) => {
    try {
      await api("POST", "/api/settings", { autostart: e.target.checked });
      toast(e.target.checked ? "Hestia will start when you sign in to Windows" : "Hestia won't start with Windows");
      refresh();
    } catch (err) { toast(err.message, true); refresh(); }
  });
  $("quitBtn").addEventListener("click", async () => {
    try { await api("POST", "/api/quit"); } catch { /* already gone */ }
    document.body.replaceChildren(h("div", { class: "empty" }, h("b", { text: "Hestia has stopped." }), "You can close this window."));
  });
  $("exportLog").addEventListener("click", async () => {
    try {
      const r = await api("GET", "/api/log");
      const url = URL.createObjectURL(new Blob([r.text], { type: "text/plain" }));
      const a = h("a", { href: url, download: `hestia-log-${new Date().toISOString().slice(0, 16).replace(/[:T]/g, "-")}.txt` });
      document.body.append(a); a.click(); a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 2000);
    } catch (err) { toast(err.message, true); }
  });

  $("installLater").addEventListener("click", () => {
    sessionStorage.setItem("hestia-install-later", "1");
    $("installBanner").hidden = true;
  });
  $("installBtn").addEventListener("click", async (e) => {
    const b = e.currentTarget;
    b.disabled = true;
    b.replaceChildren(h("span", { class: "spinner" }), "Installing\u2026");
    try {
      await api("POST", "/api/install");
      document.body.replaceChildren(h("div", { class: "empty" },
        h("b", { text: "Hestia is installed." }),
        "It's opening from your Start menu copy now. You can close this window and delete the downloaded file."));
      setTimeout(() => window.close(), 2500);
    } catch (err) {
      toast(err.message, true);
      b.disabled = false;
      b.textContent = "Install";
    }
  });

  for (const b of document.querySelectorAll("#themeSeg button")) {
    b.addEventListener("click", async () => {
      applyTheme(b.dataset.theme);
      try { await api("POST", "/api/settings", { theme: b.dataset.theme }); } catch (err) { toast(err.message, true); }
    });
  }
  $("autoUpdateToggle").addEventListener("change", async (e) => {
    try {
      await api("POST", "/api/settings", { autoUpdate: e.target.checked });
      toast(e.target.checked ? "Hestia will update itself" : "Automatic updates are off");
      refresh();
    } catch (err) { toast(err.message, true); refresh(); }
  });
  $("updateCheck").addEventListener("click", async () => {
    try { await api("POST", "/api/update/check"); setTimeout(refresh, 500); }
    catch (err) { toast(err.message, true); }
  });
  $("updateApply").addEventListener("click", async (e) => {
    const b = e.currentTarget;
    b.disabled = true;
    b.replaceChildren(h("span", { class: "spinner" }), "Restarting\u2026");
    try {
      await api("POST", "/api/update/apply");
      document.body.replaceChildren(h("div", { class: "empty" },
        h("b", { text: "Hestia is restarting with the new version." }),
        "This window will close. Hestia opens again by itself in a few seconds."));
      setTimeout(() => window.close(), 3000);
    } catch (err) {
      toast(err.message, true);
      b.disabled = false;
      b.textContent = "Restart and update";
    }
  });

  // ---- start ----
  if (!token) {
    document.body.replaceChildren(h("div", { class: "empty" }, h("b", { text: "Open Hestia from its program icon." }), "This page needs the link Hestia opens for you."));
    return;
  }
  show(tab);
  refresh();
  setInterval(refresh, 2000);
  const beat = () => api("POST", "/api/heartbeat").catch(() => {});
  beat();
  setInterval(beat, 5000);
})();
