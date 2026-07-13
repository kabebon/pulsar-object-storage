package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	httperr "pulsar/internal/errors"
	"pulsar/internal/middleware"
	"pulsar/internal/models"
	"pulsar/internal/service"
)

// BucketsHandler exposes bucket + object endpoints over REST.
type BucketsHandler struct {
	storage *service.StorageService
}

// NewBucketsHandler wires dependencies.
func NewBucketsHandler(storage *service.StorageService) *BucketsHandler {
	return &BucketsHandler{storage: storage}
}

// Routes registers bucket routes under /buckets and returns the sub-router.
func (h *BucketsHandler) Routes() http.Handler {
	r := chi.NewRouter()
	r.With(middleware.RequireScope("buckets:read")).Get("/", h.list)
	r.With(middleware.RequireScope("buckets:write")).Post("/", h.create)
	r.Route("/{id}", func(r chi.Router) {
		r.With(middleware.RequireScope("buckets:read")).Get("/", h.get)
		r.With(middleware.RequireScope("buckets:write")).Patch("/", h.update)
		r.With(middleware.RequireScope("buckets:write")).Delete("/", h.delete)
		r.With(middleware.RequireScope("objects:read")).Get("/objects", h.listObjects)
		r.With(middleware.RequireScope("objects:write")).Post("/objects/presign-upload", h.presignUpload)
		r.With(middleware.RequireScope("objects:write")).Post("/objects/confirm", h.confirmUpload)
		r.With(middleware.RequireScope("objects:read")).Get("/objects/{key}/presign-download", h.presignDownload)
		r.With(middleware.RequireScope("objects:write")).Delete("/objects/{key}", h.deleteObject)
	})
	return r
}

func (h *BucketsHandler) list(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	bs, err := h.storage.ListBuckets(r.Context(), uid)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"buckets": bs, "count": len(bs)})
}

func (h *BucketsHandler) create(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	var req struct {
		Name       string `json:"name"`
		Region     string `json:"region"`
		Visibility string `json:"visibility"`
		CDN        bool   `json:"cdn_enabled"`
	}
	if e := decode(r, &req); e != nil {
		writeError(w, e)
		return
	}
	vis := models.BucketPrivate
	if req.Visibility == "public" {
		vis = models.BucketPublic
	}
	b, err := h.storage.CreateBucket(r.Context(), uid, req.Name, req.Region, vis, req.CDN, clientIP(r), r.UserAgent())
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bucket": b})
}

func (h *BucketsHandler) get(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, e := parseID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	b, err := h.storage.GetBucket(r.Context(), uid, id)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bucket": b})
}

func (h *BucketsHandler) update(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, e := parseID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req struct {
		Visibility string `json:"visibility"`
		CDN        *bool  `json:"cdn_enabled"`
	}
	if e := decode(r, &req); e != nil {
		writeError(w, e)
		return
	}
	vis := models.BucketVisibility(req.Visibility)
	b, err := h.storage.UpdateBucket(r.Context(), uid, id, vis, req.CDN)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bucket": b})
}

func (h *BucketsHandler) delete(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, e := parseID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if err := h.storage.DeleteBucket(r.Context(), uid, id, clientIP(r), r.UserAgent()); err != nil {
		writeError(w, httperr.From(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *BucketsHandler) listObjects(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, e := parseID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	prefix := r.URL.Query().Get("prefix")
	objects, err := h.storage.ListObjects(r.Context(), uid, id, prefix, 500, 0)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"objects": objects, "count": len(objects)})
}

func (h *BucketsHandler) presignUpload(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, e := parseID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req struct {
		Key         string `json:"key"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
	}
	if e := decode(r, &req); e != nil {
		writeError(w, e)
		return
	}
	url, err := h.storage.PresignUpload(r.Context(), uid, id, req.Key, req.ContentType, req.Size)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url": url, "method": "PUT", "bucket_id": id, "key": req.Key,
		"confirm_endpoint": "/v1/buckets/" + id.String() + "/objects/confirm",
	})
}

func (h *BucketsHandler) confirmUpload(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, e := parseID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req struct {
		Key         string `json:"key"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
		ETag        string `json:"etag"`
		SHA256      string `json:"sha256"`
	}
	if e := decode(r, &req); e != nil {
		writeError(w, e)
		return
	}
	obj, err := h.storage.ConfirmUpload(r.Context(), uid, id, req.Key, req.ContentType, req.ETag, req.SHA256, req.Size)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"object": obj})
}

func (h *BucketsHandler) presignDownload(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, e := parseID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	key := chi.URLParam(r, "key")
	url, err := h.storage.PresignDownload(r.Context(), uid, id, key)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "method": "GET"})
}

func (h *BucketsHandler) deleteObject(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, e := parseID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	key := chi.URLParam(r, "key")
	if err := h.storage.DeleteObject(r.Context(), uid, id, key, clientIP(r), r.UserAgent()); err != nil {
		writeError(w, httperr.From(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func currentUserID(r *http.Request) uuid.UUID {
	uid, _ := r.Context().Value(middleware.CtxUserID).(string)
	id, _ := uuid.Parse(uid)
	return id
}

func parseID(r *http.Request) (uuid.UUID, *httperr.AppError) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, httperr.BadRequest("bad_id", "Invalid bucket id")
	}
	return id, nil
}

func clientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if idx := strings.LastIndexByte(ip, ':'); idx > 0 {
		ip = ip[:idx]
	}
	return ip
}
