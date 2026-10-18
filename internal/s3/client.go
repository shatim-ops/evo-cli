// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package s3 is a client for Cloud.ru Object Storage (S3-compatible, SigV4),
// replacing the legacy python boto3 path in cloudru_api.py. Endpoint + keys
// come from the env (CLOUDRU_S3_ENDPOINT / _ACCESS_KEY / _SECRET_KEY / _REGION),
// exported by the MCP wrapper from .cloudru/credentials.json. Uses minio-go,
// which is purpose-built for S3-compatible endpoints like s3.cloud.ru.
package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// Client wraps a minio S3 client.
type Client struct {
	mc     *minio.Client
	region string
}

// Config holds the S3 connection parameters.
type Config struct {
	Endpoint  string // full URL, e.g. https://s3.cloud.ru
	AccessKey string
	SecretKey string
	Region    string
}

// New builds an S3 client from config. Endpoint may include a scheme; the
// scheme decides TLS (https → Secure).
func New(cfg Config) (*Client, error) {
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		// A configuration problem, not an API error: exit 2, like a missing
		// Cloud.ru key.
		return nil, clierrors.New(clierrors.CodeConfigInvalid,
			"S3 credentials not configured (set CLOUDRU_S3_ACCESS_KEY / CLOUDRU_S3_SECRET_KEY, or the s3 section of the profile)", "")
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://s3.cloud.ru"
	}
	secure := true
	if strings.HasPrefix(endpoint, "http://") {
		secure = false
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else {
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}
	endpoint = strings.TrimSuffix(endpoint, "/")
	region := cfg.Region
	if region == "" {
		region = "ru-central-1"
	}
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: region,
		// evo's User-Agent, and a 2xx HTML page (a gateway's) read as 502.
		Transport: httpretry.UserAgent(nil),
	})
	if err != nil {
		return nil, fmt.Errorf("init S3 client: %w", err)
	}
	return &Client{mc: mc, region: region}, nil
}

// Bucket describes a bucket.
type Bucket struct {
	Name    string `json:"name"`
	Created string `json:"created,omitempty"`
}

// ListBuckets lists all buckets.
func (c *Client) ListBuckets(ctx context.Context) ([]Bucket, error) {
	bs, err := c.mc.ListBuckets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}
	out := make([]Bucket, 0, len(bs))
	for _, b := range bs {
		out = append(out, Bucket{Name: b.Name, Created: b.CreationDate.Format(time.RFC3339)})
	}
	return out, nil
}

// Object describes an object entry.
type Object struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified,omitempty"`
}

// ListObjects lists up to maxKeys objects under prefix.
func (c *Client) ListObjects(ctx context.Context, bucket, prefix string, maxKeys int) ([]Object, error) {
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	opts := minio.ListObjectsOptions{Prefix: prefix, Recursive: true, MaxKeys: maxKeys}
	var out []Object
	for obj := range c.mc.ListObjects(ctx, bucket, opts) {
		if obj.Err != nil {
			return nil, notFound(fmt.Errorf("list objects: %w", obj.Err), bucket, prefix)
		}
		out = append(out, Object{Key: obj.Key, Size: obj.Size, LastModified: obj.LastModified.Format(time.RFC3339)})
		if len(out) >= maxKeys {
			break
		}
	}
	return out, nil
}

// TooLargeError: the object is over the limit GetObject was given.
type TooLargeError struct {
	Size  int64 // the object's size; 0 when unknown
	Limit int64
}

func (e *TooLargeError) Error() string {
	if e.Size > 0 {
		return fmt.Sprintf("object is %d bytes, over the %d-byte limit", e.Size, e.Limit)
	}
	return fmt.Sprintf("object is over the %d-byte limit", e.Limit)
}

// GetObject returns an object's bytes, at most limit of them (limit <= 0:
// no limit). A larger object is a *TooLargeError, read no further than
// the limit: an object can be up to 5 TB (docs, Object Storage limits).
func (c *Client) GetObject(ctx context.Context, bucket, key string, limit int64) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, notFound(fmt.Errorf("get object: %w", err), bucket, key)
	}
	defer obj.Close()
	var r io.Reader = obj
	if limit > 0 {
		// The size from the answer's headers, before any body is read.
		st, serr := obj.Stat()
		var ce *clierrors.CLIError
		if serr != nil {
			if e := notFound(serr, bucket, key); errors.As(e, &ce) {
				return nil, e
			}
		} else if st.Size > limit {
			return nil, &TooLargeError{Size: st.Size, Limit: limit}
		}
		r = io.LimitReader(obj, limit+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, notFound(fmt.Errorf("read object %s/%s: %w", bucket, key, err), bucket, key)
	}
	if limit > 0 && int64(len(data)) > limit {
		return nil, &TooLargeError{Limit: limit}
	}
	return data, nil
}

// notFound gives a missing bucket or object its code: NOT_FOUND (exit 4),
// not API_ERROR (release audit, L-15: s3_get of a missing object was rc 5).
// Any other error is returned as it is.
func notFound(err error, bucket, key string) error {
	var resp minio.ErrorResponse
	if !errors.As(err, &resp) {
		return err
	}
	switch {
	case resp.Code == "NoSuchBucket":
		return clierrors.Wrap(clierrors.CodeNotFound, fmt.Sprintf("bucket %q not found", bucket),
			"s3_buckets lists the buckets of these keys.", err)
	case resp.Code == "NoSuchKey" || resp.StatusCode == http.StatusNotFound:
		return clierrors.Wrap(clierrors.CodeNotFound, fmt.Sprintf("object %s/%s not found", bucket, key),
			"s3_ls lists the objects of the bucket.", err)
	}
	return err
}

// PutObject uploads bytes to bucket/key.
func (c *Client) PutObject(ctx context.Context, bucket, key string, data []byte, contentType string) (int64, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	info, err := c.mc.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return 0, fmt.Errorf("put object: %w", err)
	}
	return info.Size, nil
}

// RemoveObject deletes bucket/key.
func (c *Client) RemoveObject(ctx context.Context, bucket, key string) error {
	if err := c.mc.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object: %w", err)
	}
	return nil
}

// MakeBucket creates a bucket. region defaults to the client's configured region.
func (c *Client) MakeBucket(ctx context.Context, bucket, region string) error {
	if region == "" {
		region = c.region
	}
	if err := c.mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		// idempotent: a bucket that already belongs to us is not an error
		exists, errEx := c.mc.BucketExists(ctx, bucket)
		if errEx == nil && exists {
			return nil
		}
		return fmt.Errorf("create bucket: %w", err)
	}
	return nil
}

// RemoveBucket deletes an EMPTY bucket.
func (c *Client) RemoveBucket(ctx context.Context, bucket string) error {
	if err := c.mc.RemoveBucket(ctx, bucket); err != nil {
		return fmt.Errorf("remove bucket: %w", err)
	}
	return nil
}
