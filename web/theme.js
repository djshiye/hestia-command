"use strict";
// Applies the saved appearance before the page paints, so there is no flash.
try {
  const t = localStorage.getItem("hestia-theme");
  if (t === "light" || t === "dark") document.documentElement.dataset.theme = t;
} catch { /* storage unavailable */ }
