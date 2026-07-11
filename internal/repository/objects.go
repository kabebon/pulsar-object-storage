package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pulsar/internal/models"
)

// ObjectsRepo persists object metadata.
type ObjectsRepo struct {
	db *DB
}

func NewObjectsRepo(db *DB) *ObjectsRepo { return &ObjectsRepo{db: db} }

// Upsert inserts or updates an object's metadata after a successful upload.
func (r *ObjectsRepo) Upsert(ctx context.Context, bucketID uuid.UUID, key, contentType, etag, sha256 string, size int64, storageClass string) (*models.Object, error) {
	if storageClass == "" {
		storageClass = "STANDARD"
	}
	const q = `
        INSERT INTO objects (bucket_id, key, size, content_type, etag, sha256, storage_class)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
        ON CONFLICT (bucket_id, key) DO UPDATE
          SET size = EXCLUDED.size,
              content_type = EXCLUDED.content_type,
              etag = EXCLUDED.etag,
              sha256 = EXCLUDED.sha256,
              version = objects.version + 1,
              updated_at = now()
        RETURNING id, bucket_id, key, size, content_type, etag, sha256, version, storage_class, uploaded_at`
	var o models.Object
	err := r.db.Pool.QueryRow(ctx, q, bucketID, key, size, contentType, etag, sha256, storageClass).Scan(
		&o.ID, &o.BucketID, &o.Key, &o.Size, &o.ContentType, &o.ETag, &o.SHA256, &o.Version, &o.StorageClass, &o.UploadedAt,
	)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// FindByKey returns metadata for a single object within a bucket.
func (r *ObjectsRepo) FindByKey(ctx context.Context, bucketID uuid.UUID, key string) (*models.Object, error) {
	const q = `
        SELECT id, bucket_id, key, size, content_type, etag, sha256, version, storage_class, uploaded_at
        FROM objects WHERE bucket_id = $1 AND key = $2`
	var o models.Object
	err := r.db.Pool.QueryRow(ctx, q, bucketID, key).Scan(
		&o.ID, &o.BucketID, &o.Key, &o.Size, &o.ContentType, &o.ETag, &o.SHA256, &o.Version, &o.StorageClass, &o.UploadedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// List enumerates objects in a bucket, optionally filtered by prefix.
func (r *ObjectsRepo) List(ctx context.Context, bucketID uuid.UUID, prefix string, limit, offset int) ([]models.Object, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `
        SELECT id, bucket_id, key, size, content_type, etag, sha256, version, storage_class, uploaded_at
        FROM objects WHERE bucket_id = $1`
	args := []any{bucketID}
	if prefix != "" {
		args = append(args, prefix+"%")
		q += fmt.Sprintf(` AND key LIKE $%d`, len(args))
	}
	args = append(args, limit, offset)
	q += fmt.Sprintf(` ORDER BY key ASC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := r.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Object
	for rows.Next() {
		var o models.Object
		if err := rows.Scan(&o.ID, &o.BucketID, &o.Key, &o.Size, &o.ContentType, &o.ETag, &o.SHA256, &o.Version, &o.StorageClass, &o.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Delete removes object metadata (the S3 object is removed separately).
func (r *ObjectsRepo) Delete(ctx context.Context, bucketID uuid.UUID, key string) error {
	const q = `DELETE FROM objects WHERE bucket_id = $1 AND key = $2`
	tag, err := r.db.Pool.Exec(ctx, q, bucketID, key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// TotalSizeByUser sums the size of all objects owned by the user (via their
// buckets). Used for quota enforcement.
func (r *ObjectsRepo) TotalSizeByUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	const q = `
        SELECT COALESCE(SUM(o.size), 0)
        FROM objects o
        JOIN buckets b ON b.id = o.bucket_id
        WHERE b.user_id = $1`
	var total int64
	err := r.db.Pool.QueryRow(ctx, q, userID).Scan(&total)
	return total, err
}
