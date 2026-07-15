#!/bin/sh
# MinIO bootstrap: create the default bucket and apply a CORS policy generated
# from the space-separated CORS_ALLOWED_ORIGINS env var.
#
# Why this exists: the web uploader (web/static/js/uploader.js) PUTs files
# directly to the storage host via a presigned URL. That is a cross-origin
# request (app origin -> storage origin), so the bucket needs a CORS policy or
# the browser blocks it with "failed to fetch". The allowed origins differ per
# environment (localhost in dev, real domains in prod), so we build the XML at
# runtime instead of shipping a static file.
#
# Env:
#   MC_HOST_local            — already set by docker-compose (alias form)
#   MINIO_BUCKET             — bucket to create/configure (default: pulsar)
#   CORS_ALLOWED_ORIGINS     — space-separated origins, e.g.
#                              "http://localhost:8080 https://app.example.com"
set -eu

MC_BIN="${MC_BIN:-mc}"
BUCKET="${MINIO_BUCKET:-pulsar}"
ORIGINS="${CORS_ALLOWED_ORIGINS:-http://localhost:8080 http://127.0.0.1:8080}"

echo "[minio-init] ensuring bucket '${BUCKET}' exists"
"${MC_BIN}" mb --ignore-existing "local/${BUCKET}"
# Keep the bucket private by default; public access is opt-in per-object.
"${MC_BIN}" anonymous set none "local/${BUCKET}" >/dev/null 2>&1 || true

# Build the <AllowedOrigin> block with real newlines. The trailing newline after
# each line keeps the XML readable; it is placed inside the quoted assignment so
# the shell preserves it.
ALLOWED_ORIGINS=""
for origin in ${ORIGINS}; do
	ALLOWED_ORIGINS="${ALLOWED_ORIGINS}    <AllowedOrigin>${origin}</AllowedOrigin>
"
done

# Write the CORS policy to a temp file via an expanding heredoc. AllowedHeader=*
# covers Content-Type/Content-MD5 and any client headers; ETag is exposed so the
# uploader can read it after a PUT. mc cors set reads the XML from the file path.
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
cat > "$TMP" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<CORSConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <CORSRule>
${ALLOWED_ORIGINS}    <AllowedMethod>GET</AllowedMethod>
    <AllowedMethod>PUT</AllowedMethod>
    <AllowedMethod>POST</AllowedMethod>
    <AllowedMethod>HEAD</AllowedMethod>
    <AllowedHeader>*</AllowedHeader>
    <ExposeHeader>ETag</ExposeHeader>
    <ExposeHeader>x-amz-request-id</ExposeHeader>
    <MaxAgeSeconds>300</MaxAgeSeconds>
  </CORSRule>
</CORSConfiguration>
EOF

echo "[minio-init] applying CORS policy for origins: ${ORIGINS}"
"${MC_BIN}" cors set "local/${BUCKET}" "$TMP"
echo "[minio-init] done"
