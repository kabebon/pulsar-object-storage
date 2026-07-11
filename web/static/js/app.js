// Pulsar — client-side enhancements.
// htmx handles most interactivity; this file wires a few conveniences:
//  - CSRF token injection for htmx non-GET requests
//  - toast notifications
//  - drag-and-drop helpers for the file manager (Phase 3)

(function () {
  "use strict";

  // --- CSRF: expose token to htmx for mutating requests ---
  function csrfToken() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? meta.getAttribute("content") : "";
  }
  // Exposed globally so inline scripts (and the uploader) can read it.
  window.csrfToken = csrfToken;

  document.addEventListener("htmx:configRequest", function (evt) {
    var method = (evt.detail.verb || "get").toLowerCase();
    if (method !== "get" && method !== "head") {
      evt.detail.headers["X-CSRF-Token"] = csrfToken();
    }
  });

  // --- Bucket uploader auto-init ---
  // The bucket detail page marks the dropzone with data-bucket-id. uploader.js
  // (a defer script loaded before this one) defines window.initBucketUploader;
  // this runs after the DOM is parsed, so both are ready.
  function initBucketDropzone() {
    var dz = document.getElementById("dropzone");
    var bucketID = dz && dz.getAttribute("data-bucket-id");
    if (!dz || !bucketID || typeof window.initBucketUploader !== "function") return;
    window.initBucketUploader({ token: csrfToken(), bucketID: bucketID });
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initBucketDropzone);
  } else {
    initBucketDropzone();
  }

  // --- Toast notifications ---
  function ensureToastContainer() {
    var c = document.getElementById("toast");
    if (!c) {
      c = document.createElement("div");
      c.id = "toast";
      document.body.appendChild(c);
    }
    return c;
  }

  window.pulsarToast = function (message, kind) {
    var c = ensureToastContainer();
    var el = document.createElement("div");
    el.className = "toast" + (kind ? " toast--" + kind : "");
    el.textContent = message;
    c.appendChild(el);
    setTimeout(function () {
      el.style.opacity = "0";
      el.style.transition = "opacity .3s";
      setTimeout(function () { el.remove(); }, 320);
    }, 4000);
  };

  // Surface htmx response errors as toasts.
  document.addEventListener("htmx:responseError", function (evt) {
    window.pulsarToast("Сетевая ошибка. Попробуйте ещё раз.", "error");
  });
  document.body.addEventListener("htmx:afterRequest", function (evt) {
    if (evt.detail.failed && evt.detail.target) {
      var msg = evt.detail.xhr.getResponseHeader("X-Toast-Error");
      if (msg) window.pulsarToast(decodeURIComponent(msg), "error");
    }
  });
})();
