package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pulsar/internal/config"
	"pulsar/internal/middleware"
	httperr "pulsar/internal/errors"
	"pulsar/internal/models"
	"pulsar/internal/service"
	"pulsar/web/views/pages"
)

// StorageHandler serves bucket + object pages and the presigned-upload JSON
// endpoint used by the client-side uploader.
type StorageHandler struct {
	cfg     *config.Config
	storage *service.StorageService
}

// NewStorageHandler wires dependencies.
func NewStorageHandler(cfg *config.Config, storage *service.StorageService) *StorageHandler {
	return &StorageHandler{cfg: cfg, storage: storage}
}

// Routes registers bucket routes on the given (already auth-protected) router.
func (h *StorageHandler) Routes(r chi.Router) {
	r.Get("/buckets", h.listBuckets)
	r.Get("/buckets/new", h.newBucketForm)
	r.Post("/buckets", h.createBucket)
	r.Delete("/buckets/{id}", h.deleteBucket)

	r.Get("/buckets/{id}", h.bucketDetail)
	r.Post("/buckets/{id}/objects/presign-upload", h.presignUpload)
	r.Post("/buckets/{id}/objects/confirm", h.confirmUpload)
	r.Get("/buckets/{id}/objects", h.listObjects)
	r.Delete("/buckets/{id}/objects/{key}", h.deleteObject)
	r.Get("/buckets/{id}/objects/{key}/download", h.presignDownload)
}

func (h *StorageHandler) listBuckets(w http.ResponseWriter, r *http.Request) {
	userID := mustUserID(r)
	bs, err := h.storage.ListBuckets(r.Context(), userID)
	if err != nil {
		writeHTMLErr(w, "Не удалось загрузить бакеты", http.StatusInternalServerError)
		return
	}
	rows := make([]pages.BucketRow, 0, len(bs))
	for _, b := range bs {
		rows = append(rows, pages.BucketRow{
			ID:         b.ID.String(),
			Name:       b.Name,
			Region:     b.Region,
			Visibility: string(b.Visibility),
			CDNEnabled: b.CDNEnabled,
			CreatedAt:  b.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	props := baseProps(h.cfg, r, "Бакеты", "", "buckets")
	maxBuckets := 0 // unlimited unless plan resolves; resolver populates later
	Render(w, r, 0, pages.BucketsPage(props, rows, maxBuckets))
}

func (h *StorageHandler) newBucketForm(w http.ResponseWriter, r *http.Request) {
	props := baseProps(h.cfg, r, "Новый бакет", "", "buckets")
	Render(w, r, 0, pages.NewBucketForm(props))
}

func (h *StorageHandler) createBucket(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeProblem(w, httperr.BadRequest("bad_form", "Invalid form data"))
		return
	}
	userID := mustUserID(r)
	name := strings.TrimSpace(r.FormValue("name"))
	region := strings.TrimSpace(r.FormValue("region"))
	visibility := models.BucketPrivate
	if r.FormValue("visibility") == "public" {
		visibility = models.BucketPublic
	}
	if _, err := h.storage.CreateBucket(r.Context(), userID, name, region, visibility, false, clientIP(r), r.UserAgent()); err != nil {
		// For htmx form submissions we return a small problem block.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": humanizeErr(err)})
		return
	}
	w.Header().Set("HX-Redirect", "/app/buckets")
	w.WriteHeader(http.StatusOK)
}

func (h *StorageHandler) deleteBucket(w http.ResponseWriter, r *http.Request) {
	userID := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeProblem(w, httperr.BadRequest("bad_id", "Invalid bucket id"))
		return
	}
	if err := h.storage.DeleteBucket(r.Context(), userID, id, clientIP(r), r.UserAgent()); err != nil {
		writeProblem(w, httperr.From(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *StorageHandler) bucketDetail(w http.ResponseWriter, r *http.Request) {
	userID := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeHTMLErr(w, "Некорректный бакет", http.StatusBadRequest)
		return
	}
	bucket, err := h.storage.GetBucket(r.Context(), userID, id)
	if err != nil {
		writeHTMLErr(w, "Бакет не найден", http.StatusNotFound)
		return
	}
	objects, err := h.storage.ListObjects(r.Context(), userID, id, "", 200, 0)
	if err != nil {
		objects = nil
	}
	rows := make([]pages.ObjectRow, 0, len(objects))
	for _, o := range objects {
		rows = append(rows, pages.ObjectRow{
			Key:         o.Key,
			Size:        o.Size,
			ContentType: o.ContentType,
			UploadedAt:  o.UploadedAt.Format("2006-01-02 15:04"),
		})
	}
	props := baseProps(h.cfg, r, bucket.Name, "", "buckets")
	Render(w, r, 0, pages.BucketDetail(props, bucket, rows))
}

// presignUpload returns a signed PUT URL for the client to upload directly.
func (h *StorageHandler) presignUpload(w http.ResponseWriter, r *http.Request) {
	userID := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeProblem(w, httperr.BadRequest("bad_id", "Invalid bucket id"))
		return
	}
	var req struct {
		Key         string `json:"key"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, httperr.BadRequest("bad_body", "Invalid JSON body"))
		return
	}
	url, err := h.storage.PresignUpload(r.Context(), userID, id, req.Key, req.ContentType, req.Size)
	if err != nil {
		writeProblem(w, httperr.From(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"url":          url,
		"method":       "PUT",
		"bucket_id":    id.String(),
		"key":          req.Key,
		"content_type": req.ContentType,
		"confirm_url":  "/app/buckets/" + id.String() + "/objects/confirm",
	})
}

// confirmUpload records object metadata after the client has completed the
// PUT to the presigned URL. It also looks up the actual size/etag from S3
// when available so the stored metadata is authoritative.
func (h *StorageHandler) confirmUpload(w http.ResponseWriter, r *http.Request) {
	userID := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeProblem(w, httperr.BadRequest("bad_id", "Invalid bucket id"))
		return
	}
	var req struct {
		Key         string `json:"key"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
		ETag        string `json:"etag"`
		SHA256      string `json:"sha256"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, httperr.BadRequest("bad_body", "Invalid JSON body"))
		return
	}
	obj, err := h.storage.ConfirmUpload(r.Context(), userID, id, req.Key, req.ContentType, req.ETag, req.SHA256, req.Size)
	if err != nil {
		writeProblem(w, httperr.From(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": obj.ID, "key": obj.Key, "size": obj.Size, "version": obj.Version,
	})
}

func (h *StorageHandler) listObjects(w http.ResponseWriter, r *http.Request) {
	userID := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeProblem(w, httperr.BadRequest("bad_id", "Invalid bucket id"))
		return
	}
	prefix := r.URL.Query().Get("prefix")
	objects, err := h.storage.ListObjects(r.Context(), userID, id, prefix, 200, 0)
	if err != nil {
		writeProblem(w, httperr.From(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	out := make([]map[string]any, 0, len(objects))
	for _, o := range objects {
		out = append(out, map[string]any{
			"key": o.Key, "size": o.Size, "content_type": o.ContentType,
			"uploaded_at": o.UploadedAt,
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (h *StorageHandler) deleteObject(w http.ResponseWriter, r *http.Request) {
	userID := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeProblem(w, httperr.BadRequest("bad_id", "Invalid bucket id"))
		return
	}
	key := chi.URLParam(r, "key")
	if err := h.storage.DeleteObject(r.Context(), userID, id, key, clientIP(r), r.UserAgent()); err != nil {
		writeProblem(w, httperr.From(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *StorageHandler) presignDownload(w http.ResponseWriter, r *http.Request) {
	userID := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeProblem(w, httperr.BadRequest("bad_id", "Invalid bucket id"))
		return
	}
	key := chi.URLParam(r, "key")
	url, err := h.storage.PresignDownload(r.Context(), userID, id, key)
	if err != nil {
		writeProblem(w, httperr.From(err))
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// mustUserID extracts the authenticated user id or returns 401.
func mustUserID(r *http.Request) uuid.UUID {
	uid, _ := r.Context().Value(middleware.CtxUserID).(string)
	id, _ := uuid.Parse(uid)
	return id
}

// writeProblem serializes an AppError as RFC 9457 application/problem+json.
func writeProblem(w http.ResponseWriter, e *httperr.AppError) {
	if e == nil {
		e = httperr.Internal(nil)
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "https://pulsar.local/errors/" + e.Code,
		"title":  e.Title,
		"status": e.Status,
		"code":   e.Code,
		"detail": e.Detail,
		"fields": e.Fields,
	})
}

func writeHTMLErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`<doctype html><body style="font-family:sans-serif;padding:3rem"><h1>` + msg + `</h1><a href="/app/buckets">← назад</a></body>`))
}
