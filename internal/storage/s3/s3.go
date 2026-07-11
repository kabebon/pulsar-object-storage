// Package s3 wraps aws-sdk-go-v2 to provide object-storage operations used by
// the storage service: presigned uploads/downloads, list, delete, copy.
// The bucket layout uses {user_id}/{bucket_name}/{object_key} as the S3 key so
// objects remain isolated per user even when sharing one physical bucket.
package s3

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"pulsar/internal/config"
)

// Storage is the S3 operations interface. Mocked in tests; backed by
// *Client in production.
type Storage interface {
	// EnsureBucket creates the configured bucket if it does not exist.
	EnsureBucket(ctx context.Context) error
	// PresignUpload returns a signed PUT URL good for the configured TTL.
	PresignUpload(ctx context.Context, s3Key, contentType string, size int64) (string, error)
	// PresignDownload returns a signed GET URL.
	PresignDownload(ctx context.Context, s3Key string) (string, error)
	// Put stores a small object directly via the SDK (used by tests/admin).
	Put(ctx context.Context, s3Key string, body io.Reader, contentType string) error
	// Get returns the object body. Caller must close the reader.
	Get(ctx context.Context, s3Key string) (io.ReadCloser, *ObjectMeta, error)
	// Delete removes a single object.
	Delete(ctx context.Context, s3Key string) error
	// List enumerates objects under a prefix.
	List(ctx context.Context, prefix string) ([]ListItem, error)
	// Head returns metadata for a single object (exists-check).
	Head(ctx context.Context, s3Key string) (*ObjectMeta, error)
	// Ping verifies connectivity by listing the bucket head.
	Ping(ctx context.Context) error
}

// ObjectMeta describes an object returned by Head/Get.
type ObjectMeta struct {
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
}

// ListItem is a flat-listing entry.
type ListItem struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// Client implements Storage backed by aws-sdk-go-v2.
type Client struct {
	s3        *s3.Client
	presigner *s3.PresignClient
	// publicPresigner signs URLs against the public CDN host (cfg.PublicEndpoint).
	// nil when no public endpoint is configured — presigned URLs then use the
	// internal Endpoint (acceptable for local dev, leaks the origin in prod).
	publicPresigner *s3.PresignClient
	bucket          string
	presignTTL      time.Duration
}

// New builds an S3 client from configuration.
func New(ctx context.Context, cfg config.S3Config) (*Client, error) {
	awsConfig, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(cfg.Region),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
		// aws-sdk-go-v2 enables trailing checksums (x-amz-checksum-*) by
		// default since v1.32, which MinIO refuses with
		// "not found: ComputePayloadHash". Setting both to WhenRequired
		// disables the default checksum behavior so requests succeed against
		// MinIO (harmless on real AWS).
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	c := &Client{
		s3:        client,
		presigner: s3.NewPresignClient(client),
		bucket:    cfg.Bucket,
		presignTTL: cfg.PresignExpiry,
	}

	// When a public endpoint (CDN) is configured, build a second presigner that
	// signs against it. SigV4 binds the host header into the signature, so we
	// cannot rewrite an internally-signed URL afterwards — it must be signed
	// against the host clients will actually hit (the CDN), which forwards the
	// request to the origin (S3/MinIO) preserving Host.
	if cfg.PublicEndpoint != "" {
		publicClient := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.PublicEndpoint)
			// Path-style is required because the public URL carries the bucket
			// in the path (cdn.example.com/<bucket>/...) rather than as a host
			// label, which would need a per-bucket DNS record.
			o.UsePathStyle = true
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		})
		c.publicPresigner = s3.NewPresignClient(publicClient)
	}

	return c, nil
}

// Bucket returns the configured bucket name.
func (c *Client) Bucket() string { return c.bucket }

// EnsureBucket creates the bucket if missing. Ignores "already owned by you".
func (c *Client) EnsureBucket(ctx context.Context) error {
	_, err := c.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.bucket)})
	if err != nil && !isAlreadyOwned(err) {
		return err
	}
	return nil
}

// presignerToUse returns the public presigner (CDN host) when configured, else
// the internal one. Presigned URLs handed to clients must point at the host they
// will actually request; server-side operations (Put/Get/Delete/...) always use
// the internal client directly.
func (c *Client) presignerToUse() *s3.PresignClient {
	if c.publicPresigner != nil {
		return c.publicPresigner
	}
	return c.presigner
}

// PresignUpload produces a PUT URL. contentType may be empty.
func (c *Client) PresignUpload(ctx context.Context, s3Key, contentType string, size int64) (string, error) {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(s3Key),
		ContentType: aws.String(contentType),
	}
	req, err := c.presignerToUse().PresignPutObject(ctx, input, s3.WithPresignExpires(c.presignTTL))
	if err != nil {
		return "", fmt.Errorf("presign put: %w", err)
	}
	return req.URL, nil
}

// PresignDownload produces a GET URL.
func (c *Client) PresignDownload(ctx context.Context, s3Key string) (string, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(s3Key),
	}
	req, err := c.presignerToUse().PresignGetObject(ctx, input, s3.WithPresignExpires(c.presignTTL))
	if err != nil {
		return "", fmt.Errorf("presign get: %w", err)
	}
	return req.URL, nil
}

// Put uploads the body directly via the SDK.
func (c *Client) Put(ctx context.Context, s3Key string, body io.Reader, contentType string) error {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(s3Key),
		Body:        body,
		ContentType: aws.String(contentType),
	}
	if _, err := c.s3.PutObject(ctx, input); err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// Get returns the object body and metadata.
func (c *Client) Get(ctx context.Context, s3Key string) (io.ReadCloser, *ObjectMeta, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("get object: %w", err)
	}
	meta := &ObjectMeta{
		Key:          s3Key,
		Size:         aws.ToInt64(out.ContentLength),
		ContentType:  aws.ToString(out.ContentType),
		ETag:         strings.Trim(aws.ToString(out.ETag), "\""),
		LastModified: aws.ToTime(out.LastModified),
	}
	return out.Body, meta, nil
}

// Delete removes a single object.
func (c *Client) Delete(ctx context.Context, s3Key string) error {
	if _, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(s3Key),
	}); err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// List enumerates objects under the prefix (non-recursive is fine for our use).
func (c *Client) List(ctx context.Context, prefix string) ([]ListItem, error) {
	out, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	items := make([]ListItem, 0, len(out.Contents))
	for _, obj := range out.Contents {
		items = append(items, ListItem{
			Key:          aws.ToString(obj.Key),
			Size:         aws.ToInt64(obj.Size),
			LastModified: aws.ToTime(obj.LastModified),
		})
	}
	return items, nil
}

// Head returns metadata for an object.
func (c *Client) Head(ctx context.Context, s3Key string) (*ObjectMeta, error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return nil, fmt.Errorf("head object: %w", err)
	}
	return &ObjectMeta{
		Key:         s3Key,
		Size:        aws.ToInt64(out.ContentLength),
		ContentType: aws.ToString(out.ContentType),
		ETag:        strings.Trim(aws.ToString(out.ETag), "\""),
	}, nil
}

// Ping checks connectivity by listing the bucket head.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	return err
}

// BuildKey composes the S3 object key for a given (user, bucket, objectKey).
// We nest under the user id so different users never collide even when the
// physical bucket is shared.
func BuildKey(userID, bucketName, objectKey string) string {
	return strings.TrimPrefix(userID+"/"+bucketName+"/"+objectKey, "/")
}

// isAlreadyOwned returns true when CreateBucket reports the bucket exists.
func isAlreadyOwned(err error) bool {
	if err == nil {
		return false
	}
	var bae *types.BucketAlreadyExists
	var bao *types.BucketAlreadyOwnedByYou
	return err == bae || err == bao || strings.Contains(err.Error(), "AlreadyOwnedByYou")
}
