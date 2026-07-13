package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"pulsar/internal/models"
	"pulsar/internal/repository"
	s3store "pulsar/internal/storage/s3"
)

// StorageDeps bundles collaborators for the storage service.
type StorageDeps struct {
	Buckets   *repository.BucketsRepo
	Objects   *repository.ObjectsRepo
	Audit     *repository.AuditLogRepo
	Usage     *repository.UsageRepo
	S3        s3store.Storage
	// PlanResolver returns the active plan for a user (max buckets, storage quota).
	PlanResolver func(ctx context.Context, userID uuid.UUID) (PlanLimits, error)
}

// PlanLimits is the quota-relevant slice of a plan.
type PlanLimits struct {
	Slug                string
	StorageBytes        int64
	BandwidthBytesMonth int64
	MaxBuckets          int // <=0 means unlimited
}

// StorageService handles buckets and object orchestration.
type StorageService struct {
	StorageDeps
}

// NewStorageService wires dependencies.
func NewStorageService(deps StorageDeps) *StorageService {
	return &StorageService{StorageDeps: deps}
}

// CreateBucket creates a bucket row and (optionally) the physical S3 prefix.
func (s *StorageService) CreateBucket(ctx context.Context, userID uuid.UUID, name, region string, visibility models.BucketVisibility, cdnEnabled bool, ip, ua string) (*models.Bucket, error) {
	if err := validateBucketName(name); err != nil {
		return nil, repository.Wrap(models.ErrValidation, err.Error())
	}
	// Quota: max buckets per plan.
	if s.PlanResolver != nil {
		limits, err := s.PlanResolver(ctx, userID)
		if err == nil && limits.MaxBuckets > 0 {
			count, err := s.Buckets.CountByUser(ctx, userID)
			if err != nil {
				return nil, err
			}
			if count >= limits.MaxBuckets {
				return nil, repository.Wrap(models.ErrQuotaExceeded, fmt.Sprintf("plan %s allows up to %d buckets", limits.Slug, limits.MaxBuckets))
			}
		}
	}
	if region == "" {
		region = "us-east-1"
	}
	b, err := s.Buckets.Create(ctx, userID, name, region, visibility, cdnEnabled)
	if err != nil {
		return nil, err
	}
	uid := userID
	_ = s.Audit.Record(ctx, &uid, models.AuditBucketCreated, ip, ua, nil)
	return b, nil
}

// ListBuckets returns the user's buckets.
func (s *StorageService) ListBuckets(ctx context.Context, userID uuid.UUID) ([]models.Bucket, error) {
	return s.Buckets.List(ctx, userID)
}

// GetBucket returns a bucket owned by the user.
func (s *StorageService) GetBucket(ctx context.Context, userID, id uuid.UUID) (*models.Bucket, error) {
	return s.Buckets.FindByID(ctx, userID, id)
}

// UpdateBucket mutates visibility / cdn settings.
func (s *StorageService) UpdateBucket(ctx context.Context, userID, id uuid.UUID, visibility models.BucketVisibility, cdnEnabled *bool) (*models.Bucket, error) {
	return s.Buckets.Update(ctx, userID, id, visibility, cdnEnabled)
}

// DeleteBucket removes the bucket and all underlying objects.
func (s *StorageService) DeleteBucket(ctx context.Context, userID, id uuid.UUID, ip, ua string) error {
	b, err := s.Buckets.FindByID(ctx, userID, id)
	if err != nil {
		return err
	}
	// Best-effort delete of all S3 objects under the bucket prefix.
	prefix := s3store.BuildKey(userID.String(), b.Name, "")
	items, _ := s.S3.List(ctx, prefix)
	for _, it := range items {
		_ = s.S3.Delete(ctx, it.Key)
	}
	if err := s.Buckets.Delete(ctx, userID, id); err != nil {
		return err
	}
	uid := userID
	_ = s.Audit.Record(ctx, &uid, models.AuditBucketDeleted, ip, ua, nil)
	return nil
}

// PresignUpload returns a signed PUT URL for a new object. The object metadata
// row is recorded after the upload completes (see ConfirmUpload).
func (s *StorageService) PresignUpload(ctx context.Context, userID, bucketID uuid.UUID, key, contentType string, size int64) (string, error) {
	b, err := s.Buckets.FindByID(ctx, userID, bucketID)
	if err != nil {
		return "", err
	}
	if err := validateObjectKey(key); err != nil {
		return "", repository.Wrap(models.ErrValidation, err.Error())
	}
	// Quota check: storage must not exceed plan.
	if s.PlanResolver != nil {
		limits, err := s.PlanResolver(ctx, userID)
		if err == nil && limits.StorageBytes > 0 {
			current, err := s.Objects.TotalSizeByUser(ctx, userID)
			if err != nil {
				return "", err
			}
			if current+size > limits.StorageBytes {
				return "", repository.Wrap(models.ErrQuotaExceeded, fmt.Sprintf("upload would exceed storage quota of %d bytes", limits.StorageBytes))
			}
		}
	}
	s3Key := s3store.BuildKey(userID.String(), b.Name, key)
	return s.S3.PresignUpload(ctx, s3Key, contentType, size)
}

// ConfirmUpload records metadata after the client has PUT the object.
func (s *StorageService) ConfirmUpload(ctx context.Context, userID, bucketID uuid.UUID, key, contentType, etag, sha256 string, size int64) (*models.Object, error) {
	b, err := s.Buckets.FindByID(ctx, userID, bucketID)
	if err != nil {
		return nil, err
	}

	s3Key := s3store.BuildKey(userID.String(), b.Name, key)
	meta, err := s.S3.Head(ctx, s3Key)
	if err != nil {
		return nil, repository.Wrap(models.ErrValidation, "object not found on S3")
	}

	// Quota check: storage must not exceed plan.
	if s.PlanResolver != nil {
		limits, err := s.PlanResolver(ctx, userID)
		if err == nil && limits.StorageBytes > 0 {
			current, err := s.Objects.TotalSizeByUser(ctx, userID)
			if err != nil {
				return nil, err
			}
			var oldSize int64
			if oldObj, err := s.Objects.FindByKey(ctx, bucketID, key); err == nil {
				oldSize = oldObj.Size
			}
			if current-oldSize+meta.Size > limits.StorageBytes {
				_ = s.S3.Delete(ctx, s3Key)
				return nil, repository.Wrap(models.ErrQuotaExceeded, fmt.Sprintf("upload exceeded storage quota of %d bytes", limits.StorageBytes))
			}
		}
	}

	return s.Objects.Upsert(ctx, bucketID, key, meta.ContentType, meta.ETag, sha256, meta.Size, "STANDARD")
}

// PresignDownload returns a signed GET URL for an object.
func (s *StorageService) PresignDownload(ctx context.Context, userID, bucketID uuid.UUID, key string) (string, error) {
	b, err := s.Buckets.FindByID(ctx, userID, bucketID)
	if err != nil {
		return "", err
	}
	if _, err := s.Objects.FindByKey(ctx, bucketID, key); err != nil {
		return "", err
	}
	s3Key := s3store.BuildKey(userID.String(), b.Name, key)
	// Record bandwidth usage (best-effort).
	bucketIDCopy := bucketID
	_ = s.Usage.Record(ctx, userID, models.UsageBandwidthBytes, 0, &bucketIDCopy)
	return s.S3.PresignDownload(ctx, s3Key)
}

// ListObjects enumerates objects in a bucket.
func (s *StorageService) ListObjects(ctx context.Context, userID, bucketID uuid.UUID, prefix string, limit, offset int) ([]models.Object, error) {
	if _, err := s.Buckets.FindByID(ctx, userID, bucketID); err != nil {
		return nil, err
	}
	return s.Objects.List(ctx, bucketID, prefix, limit, offset)
}

// DeleteObject removes the S3 object and its metadata.
func (s *StorageService) DeleteObject(ctx context.Context, userID, bucketID uuid.UUID, key, ip, ua string) error {
	b, err := s.Buckets.FindByID(ctx, userID, bucketID)
	if err != nil {
		return err
	}
	if _, err := s.Objects.FindByKey(ctx, bucketID, key); err != nil {
		return err
	}
	s3Key := s3store.BuildKey(userID.String(), b.Name, key)
	if err := s.S3.Delete(ctx, s3Key); err != nil {
		return err
	}
	if err := s.Objects.Delete(ctx, bucketID, key); err != nil {
		return err
	}
	uid := userID
	_ = s.Audit.Record(ctx, &uid, models.AuditObjectDeleted, ip, ua, nil)
	return nil
}

// validateBucketName enforces DNS-compatible bucket naming (S3 requirement).
// Names must be lowercase by S3 convention; we reject uppercase explicitly.
func validateBucketName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 3 || len(name) > 63 {
		return errors.New("bucket name must be 3-63 characters")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
		default:
			return errors.New("bucket name may only contain lowercase a-z, 0-9, '-', '.'")
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, ".") ||
		strings.HasSuffix(name, "-") || strings.HasSuffix(name, ".") {
		return errors.New("bucket name must not start or end with '-' or '.'")
	}
	if strings.Contains(name, "..") {
		return errors.New("bucket name must not contain consecutive dots")
	}
	return nil
}

// validateObjectKey forbids empty keys and path traversal.
func validateObjectKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("object key is required")
	}
	if strings.Contains(key, "..") {
		return errors.New("object key must not contain '..'")
	}
	if len(key) > 1024 {
		return errors.New("object key too long (max 1024)")
	}
	return nil
}
