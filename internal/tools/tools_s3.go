// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/s3"
)

// s3Client builds an S3 client from CLOUDRU_S3_ENDPOINT / _ACCESS_KEY /
// _SECRET_KEY / _REGION or the s3 section of the profile.
func (s *Registry) s3Client() (*s3.Client, error) {
	cfg, err := s.s3Config()
	if err != nil {
		return nil, err
	}
	return s3.New(cfg)
}

func (s *Registry) handleS3Buckets(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	bs, err := cli.ListBuckets(ctx)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"buckets": bs}), false
}

func (s *Registry) handleS3Ls(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	if bucket == "" {
		return usageResult(fmt.Errorf("'bucket' is required")), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	objs, err := cli.ListObjects(ctx, bucket, stringArg(args, "prefix"), pgInt(args, "max_keys", 1000))
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"bucket": bucket, "count": len(objs), "objects": objs}), false
}

// s3GetMaxBytes bounds the object s3_get reads: the answer carries it
// whole (base64 adds a third), and an object can be up to 5 TB.
const s3GetMaxBytes = 10 << 20

func (s *Registry) handleS3Get(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	if bucket == "" || key == "" {
		return usageResult(fmt.Errorf("'bucket' and 'key' are required")), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	data, err := cli.GetObject(ctx, bucket, key, s3GetMaxBytes)
	var tl *s3.TooLargeError
	if errors.As(err, &tl) {
		return errResult(clierrors.New(clierrors.CodeUsage,
			fmt.Sprintf("%s/%s: %v — s3_get returns objects up to %d MiB inline", bucket, key, err, s3GetMaxBytes>>20),
			"Download it with an S3 client (aws s3 cp, rclone; endpoint https://s3.cloud.ru, the same keys) or in the Cloud.ru console; s3_ls shows the sizes.")), true
	}
	if err != nil {
		return errResult(err), true
	}
	out := map[string]any{"bucket": bucket, "key": key, "size": len(data)}
	// Return text inline when it's valid UTF-8 and not too large; otherwise base64.
	if utf8.Valid(data) && len(data) <= 262144 {
		out["encoding"] = "utf-8"
		out["content"] = string(data)
	} else {
		out["encoding"] = "base64"
		out["content_base64"] = base64.StdEncoding.EncodeToString(data)
	}
	return toJSON(out), false
}

func (s *Registry) handleS3Put(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	if bucket == "" || key == "" {
		return usageResult(fmt.Errorf("'bucket' and 'key' are required")), true
	}
	// Body from inline text (content) or base64 (content_base64).
	var data []byte
	if b64 := stringArg(args, "content_base64"); b64 != "" {
		d, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return errResult(fmt.Errorf("invalid content_base64: %w", err)), true
		}
		data = d
	} else {
		data = []byte(stringArg(args, "content"))
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("s3_put will upload %d bytes to s3://%s/%s, overwriting the object if it exists. Call again with confirmed=true after the user approves.", len(data), bucket, key),
		}), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	n, err := cli.PutObject(ctx, bucket, key, data, stringArg(args, "content_type"))
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "ok", "bucket": bucket, "key": key, "size": n}), false
}

func (s *Registry) handleS3Rm(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	if bucket == "" || key == "" {
		return usageResult(fmt.Errorf("'bucket' and 'key' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("s3_rm permanently deletes s3://%s/%s. Call again with confirmed=true after the user approves.", bucket, key),
		}), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	if err := cli.RemoveObject(ctx, bucket, key); err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "deleted", "bucket": bucket, "key": key}), false
}

func (s *Registry) handleS3BucketCreate(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	if bucket == "" {
		return usageResult(fmt.Errorf("'bucket' is required (the new bucket name)")), true
	}
	// The rule the schema states, before the preview (QA-15).
	if err := bucketName(bucket); err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("s3_bucket_create creates bucket %q (billable storage). Call again with confirmed=true after the user approves.", bucket),
			"preview": s3BucketPreview(bucket, stringArg(args, "region")),
		}), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	if err := cli.MakeBucket(ctx, bucket, stringArg(args, "region")); err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "created", "bucket": bucket}), false
}

func (s *Registry) handleS3BucketDelete(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	if bucket == "" {
		return usageResult(fmt.Errorf("'bucket' is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("s3_bucket_delete permanently deletes bucket %q (it must be EMPTY). Call again with confirmed=true after the user approves.", bucket),
		}), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	if err := cli.RemoveBucket(ctx, bucket); err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "deleted", "bucket": bucket}), false
}

// s3BucketPreview is the preview of s3_bucket_create. Storage is billed by
// use, so there is no price up front.
func s3BucketPreview(bucket, region string) engine.CreatePreview {
	p := engine.CreatePreview{Resource: engine.ResourceS3Bucket, Name: bucket}
	p.Add("region", region)
	p.Price()
	return p
}
