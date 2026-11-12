// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
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
		return usageT("'bucket' is required", "нужен 'bucket'"), true
	}
	if err := s3.CheckBucketName(bucket); err != nil {
		return errResult(err), true
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
		return usageT("'bucket' and 'key' are required", "нужны 'bucket' и 'key'"), true
	}
	if err := s3.CheckBucketName(bucket); err != nil {
		return errResult(err), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	data, err := cli.GetObject(ctx, bucket, key, s3GetMaxBytes)
	var tl *s3.TooLargeError
	if errors.As(err, &tl) {
		return errResult(clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%s/%s: %v — s3_get returns objects up to %d MiB inline", "%s/%s: %v — s3_get возвращает объекты до %d МиБ", bucket, key, err, s3GetMaxBytes>>20),
			i18n.T("Download it with an S3 client (aws s3 cp, rclone; endpoint https://s3.cloud.ru, the same keys) or in the Cloud.ru console; s3_ls shows the sizes.", "Скачайте объект клиентом S3 (aws s3 cp, rclone; endpoint https://s3.cloud.ru, те же ключи) или в консоли Cloud.ru; размеры — в выводе s3_ls."))), true
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

// planS3Put plans s3_put (D35, release plan 1.7 F5a): the arguments and
// the S3 settings of the profile before the question — without keys the
// upload cannot run, and the refusal came after consent.
func (s *Registry) planS3Put(_ context.Context, args map[string]any) (*Plan, error) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	if bucket == "" || key == "" {
		return nil, usageErr("'bucket' and 'key' are required", "нужны 'bucket' и 'key'")
	}
	if err := s3.CheckBucketName(bucket); err != nil {
		return nil, err
	}
	// Body from inline text (content) or base64 (content_base64).
	var data []byte
	if b64 := stringArg(args, "content_base64"); b64 != "" {
		d, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("invalid content_base64: %w", "content_base64 — не base64: %w"), err)
		}
		data = d
	} else {
		data = []byte(stringArg(args, "content"))
	}
	cli, err := s.s3Client()
	if err != nil {
		return nil, err
	}
	preview := map[string]any{
		"message": i18n.Tf("s3_put will upload %d bytes to s3://%s/%s, overwriting the object if it exists.",
			"s3_put загрузит %d байт в s3://%s/%s и перезапишет объект, если он есть.", len(data), bucket, key),
		"preview": willPreview(engine.ResourceS3Object, "s3://"+bucket+"/"+key, "", map[string]any{"size_bytes": len(data)}, []string{"size_bytes"}),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		n, err := cli.PutObject(ctx, bucket, key, data, stringArg(args, "content_type"))
		if err != nil {
			return errResult(err), true
		}
		return toJSON(map[string]any{"status": "ok", "bucket": bucket, "key": key, "size": n}), false
	}}, nil
}

func (s *Registry) planS3Rm(ctx context.Context, args map[string]any) (*Plan, error) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	if bucket == "" || key == "" {
		return nil, usageErr("'bucket' and 'key' are required", "нужны 'bucket' и 'key'")
	}
	if err := s3.CheckBucketName(bucket); err != nil {
		return nil, err
	}
	cli, err := s.s3Client()
	if err != nil {
		return nil, err
	}
	// The object first, before and after consent: S3 answers the delete of
	// a key that is not there as done, and a bucket that is not there is
	// the call's error (NOT_FOUND), not a repeat (D26). The first key at or
	// after key is key itself when it is there.
	objs, err := cli.ListObjects(ctx, bucket, key, 1)
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 || objs[0].Key != key {
		return answered(s.deleteFailed("s3_rm", key, clierrors.New(clierrors.CodeNotFound,
			i18n.Tf("no object s3://%s/%s", "объекта s3://%s/%s нет", bucket, key), ""))), nil
	}
	preview := map[string]any{"message": i18n.Tf("s3_rm permanently deletes s3://%s/%s.",
		"s3_rm безвозвратно удаляет s3://%s/%s.", bucket, key),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		if err := cli.RemoveObject(ctx, bucket, key); err != nil {
			return errResult(err), true
		}
		return toJSON(map[string]any{"status": "deleted", "bucket": bucket, "key": key}), false
	}}, nil
}

// planS3BucketCreate plans s3_bucket_create (D35, release plan 1.7 F5a):
// the name by the schema's rule (QA-15) and the S3 settings of the
// profile before the question — without keys the create cannot run, and
// the refusal came after consent.
func (s *Registry) planS3BucketCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	bucket := stringArg(args, "bucket")
	if bucket == "" {
		return nil, usageErr("'bucket' is required (the new bucket name)", "нужен 'bucket' — имя нового бакета")
	}
	if err := bucketName(bucket); err != nil {
		return nil, err
	}
	cli, err := s.s3Client()
	if err != nil {
		return nil, err
	}
	// A bucket the profile has already: nothing to create and nothing to
	// ask (D101). A bucket of another tenant is not in this list: S3
	// refuses its create.
	// A key that may not list the buckets goes on as before D101: the
	// question, then the create, where S3 itself says whose the name is.
	own, err := cli.ListBuckets(ctx)
	if err != nil && !s3.AccessDenied(err) {
		return nil, err
	}
	if slices.ContainsFunc(own, func(b s3.Bucket) bool { return b.Name == bucket }) {
		return answered(s3BucketExists(bucket), false), nil
	}
	region := stringArg(args, "region")
	preview, err := s.completePreview(ctx, nil, s3BucketPreview(bucket, region))
	if err != nil {
		return nil, err
	}
	return &Plan{
		Preview: map[string]any{
			"message": i18n.Tf("s3_bucket_create creates bucket %q (billable storage).",
				"s3_bucket_create создаёт бакет %q (хранение платное).", bucket),
			"preview": preview,
		},
		Execute: func(ctx context.Context) (string, bool) {
			existed, err := cli.MakeBucket(ctx, bucket, region)
			if err != nil {
				return errResult(err), true
			}
			// Made by another call since the plan.
			if existed {
				return s3BucketExists(bucket), false
			}
			return toJSON(map[string]any{"status": "created", "bucket": bucket}), false
		},
	}, nil
}

// s3BucketExists is the answer of s3_bucket_create for a bucket the
// profile has already (D101): not created by this call, so a cleanup of
// «what I created» must not delete it.
func s3BucketExists(bucket string) string {
	return toJSON(map[string]any{"status": "already_exists", "bucket": bucket,
		"message": i18n.Tf("bucket %q exists already and is yours; this call created nothing — do not count it as created by this call.",
			"бакет %q уже существует и принадлежит вам; этот вызов ничего не создал — не считайте бакет созданным этим вызовом.", bucket)})
}

// planS3BucketDelete plans s3_bucket_delete (D35): a bucket that is not
// there is already deleted (D26), and one with objects is refused before
// the question — S3 refuses to delete it (BucketNotEmpty), and the refusal
// came after consent (W6, G13-3).
func (s *Registry) planS3BucketDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	bucket := stringArg(args, "bucket")
	if bucket == "" {
		return nil, usageErr("'bucket' is required", "нужен 'bucket'")
	}
	if err := s3.CheckBucketName(bucket); err != nil {
		return nil, err
	}
	cli, err := s.s3Client()
	if err != nil {
		return nil, err
	}
	objs, err := cli.ListObjects(ctx, bucket, "", 1)
	if err != nil {
		return answered(s.deleteFailed("s3_bucket_delete", bucket, err)), nil
	}
	if len(objs) > 0 {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("bucket %q is not empty, and S3 deletes only an empty bucket; nothing was deleted",
				"бакет %q не пуст, а S3 удаляет только пустой бакет; ничего не удалено", bucket),
			i18n.Tf("List its objects with s3_ls bucket=%[1]s (evo s3 ls %[1]s): only an empty bucket goes; whether to delete its objects (s3_rm) — ask the person.",
				"Объекты бакета — в s3_ls bucket=%[1]s (evo s3 ls %[1]s): удаляется только пустой бакет; удалять ли объекты (s3_rm) — спросите человека.", bucket))
	}
	return &Plan{
		Preview: map[string]any{
			"message": i18n.Tf("s3_bucket_delete permanently deletes bucket %q (it must be EMPTY).",
				"s3_bucket_delete безвозвратно удаляет бакет %q (он должен быть ПУСТЫМ).", bucket),
		},
		Execute: func(ctx context.Context) (string, bool) {
			if err := cli.RemoveBucket(ctx, bucket); err != nil {
				return s.deleteFailed("s3_bucket_delete", bucket, err)
			}
			return toJSON(map[string]any{"status": "deleted", "bucket": bucket}), false
		},
	}, nil
}

// s3BucketPreview is the preview of s3_bucket_create. Storage is billed by
// use, so there is no price up front.
func s3BucketPreview(bucket, region string) engine.CreatePreview {
	p := engine.CreatePreview{Resource: engine.ResourceS3Bucket, Name: bucket}
	p.Add("region", region)
	p.Price()
	return p
}
