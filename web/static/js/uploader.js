// Pulsar — bucket file uploader.
// Orchestrates presigned-URL uploads: request a PUT URL, stream the file
// directly to S3/MinIO, then POST a confirm callback so metadata is recorded.

(function () {
  "use strict";

  /**
   * @param {{token:string, bucketID:string}} opts
   */
  window.initBucketUploader = function (opts) {
    var dropzone = document.getElementById("dropzone");
    var fileInput = document.getElementById("file-input");
    var progress = document.getElementById("upload-progress");
    if (!dropzone || !fileInput) return;

    dropzone.addEventListener("click", function () { fileInput.click(); });
    fileInput.addEventListener("change", function () {
      handleFiles(fileInput.files);
      fileInput.value = "";
    });

    ["dragenter", "dragover"].forEach(function (evt) {
      dropzone.addEventListener(evt, function (e) {
        e.preventDefault();
        dropzone.classList.add("is-dragover");
      });
    });
    ["dragleave", "drop"].forEach(function (evt) {
      dropzone.addEventListener(evt, function (e) {
        e.preventDefault();
        dropzone.classList.remove("is-dragover");
      });
    });
    dropzone.addEventListener("drop", function (e) {
      var files = e.dataTransfer && e.dataTransfer.files;
      if (files && files.length) handleFiles(files);
    });

    function handleFiles(fileList) {
      Array.prototype.forEach.call(fileList, function (file) {
        uploadOne(file);
      });
    }

    function uploadOne(file) {
      var row = document.createElement("div");
      row.className = "flex items-center justify-between gap-2";
      row.innerHTML =
        '<span class="truncate">' + escapeHTML(file.name) + " · " + formatBytes(file.size) + "</span>" +
        '<span class="spinner"></span>';
      progress.appendChild(row);

      // 1) Request presigned PUT URL.
      fetch("/app/buckets/" + opts.bucketID + "/objects/presign-upload", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": opts.token,
        },
        body: JSON.stringify({
          key: file.webkitRelativePath || file.name,
          content_type: file.type || "application/octet-stream",
          size: file.size,
        }),
      })
        .then(function (r) { return r.json(); })
        .then(function (info) {
          if (!info || !info.url) throw new Error("no upload url");
          return fetch(info.url, { method: "PUT", body: file, headers: { "Content-Type": file.type || "application/octet-stream" } })
            .then(function () { return info; });
        })
        .then(function (info) {
          // 3) Confirm metadata.
          return fetch(info.confirm_url, {
            method: "POST",
            credentials: "same-origin",
            headers: { "Content-Type": "application/json", "X-CSRF-Token": opts.token },
            body: JSON.stringify({
              key: info.key,
              content_type: info.content_type,
              size: file.size,
            }),
          });
        })
        .then(function () {
          row.querySelector(".spinner").remove();
          var ok = document.createElement("span");
          ok.className = "text-emerald-400 text-xs";
          ok.textContent = "загружено";
          row.appendChild(ok);
          // Refresh the object list.
          setTimeout(function () { window.location.reload(); }, 500);
        })
        .catch(function (err) {
          row.querySelector(".spinner").remove();
          var bad = document.createElement("span");
          bad.className = "text-rose-400 text-xs";
          bad.textContent = "ошибка: " + (err && err.message ? err.message : "network");
          row.appendChild(bad);
        });
    }

    function escapeHTML(s) {
      return String(s).replace(/[&<>"']/g, function (c) {
        return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
      });
    }

    function formatBytes(n) {
      if (n < 1024) return n + " B";
      var units = ["KiB", "MiB", "GiB", "TiB"];
      var i = -1;
      do { n /= 1024; i++; } while (n >= 1024 && i < units.length - 1);
      return n.toFixed(1) + " " + units[i];
    }
  };
})();
