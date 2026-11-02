(function () {
  "use strict";

  var body = document.body;
  if (!body) return;

  var userAgent = navigator.userAgent || "";
  var android = /Android/i.test(userAgent);
  var ios = /iPhone|iPad|iPod/i.test(userAgent) ||
    (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  var store = android ? body.dataset.playStore : ios ? body.dataset.appStore : "";
  var download = document.getElementById("download-app");
  var openApp = document.getElementById("open-app");
  var timer;

  body.dataset.platform = android ? "android" : ios ? "ios" : "desktop";
  if (download && store) download.href = store;

  function cancelRedirect() {
    if (timer) {
      window.clearTimeout(timer);
      timer = null;
    }
  }

  // A manual tap is required for custom schemes in most in-app browsers.
  // Leave enough time to choose that action before an ordinary browser opens a store.
  if (openApp) openApp.addEventListener("click", cancelRedirect);
  if (download) download.addEventListener("click", cancelRedirect);
  document.addEventListener("visibilitychange", function () {
    if (document.hidden) cancelRedirect();
  });
  window.addEventListener("pagehide", cancelRedirect);

  var crawler = /bot|crawler|spider|slurp|preview|facebookexternalhit|facebot|WhatsApp|TelegramBot|Twitterbot|LinkedInBot|vkShare|Viber/i.test(userAgent);
  var embedded = /Telegram|WhatsApp|FBAN|FBAV|Instagram|\bwv\b|Line\//i.test(userAgent) ||
    (ios && /AppleWebKit/i.test(userAgent) && !/Safari/i.test(userAgent));

  if (body.dataset.status === "200" && store && !crawler && !embedded && !document.hidden) {
    timer = window.setTimeout(function () {
      timer = null;
      if (!document.hidden) window.location.replace(store);
    }, 1800);
  }
}());
