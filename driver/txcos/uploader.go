package txcos

import (
	"context"
	"io"
	"time"

	"github.com/goairix/fs"
	"github.com/tencentyun/cos-go-sdk-v5"
)

func (driver *cosFs) Uploader() fs.Uploader {
	return driver
}

func (driver *cosFs) Upload(ctx context.Context, path string, reader io.Reader, opts ...fs.Option) error {
	file, err := driver.Create(ctx, path, opts...)
	if err != nil {
		return err
	}

	_, err = io.Copy(file, reader)
	if err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}

func (driver *cosFs) InitMultipartUpload(ctx context.Context, path string, opts ...fs.Option) (string, error) {
	path = driver.path(path)
	o := &fs.Options{}
	for _, opt := range opts {
		opt(o)
	}
	options := &cos.InitiateMultipartUploadOptions{
		ObjectPutHeaderOptions: buildObjectPutHeaderOptions(o.ContentType, o.ContentDisposition),
	}
	res, _, err := driver.client.Object.InitiateMultipartUpload(ctx, path, options)
	if err != nil {
		return "", err
	}
	return res.UploadID, nil
}

func (driver *cosFs) UploadPart(ctx context.Context, path string, uploadID string, partNumber int, data io.Reader, opts ...fs.Option) (string, error) {
	path = driver.path(path)
	res, err := driver.client.Object.UploadPart(ctx, path, uploadID, partNumber, data, nil)
	if err != nil {
		return "", err
	}
	return res.Header.Get("ETag"), nil
}

func (driver *cosFs) CompleteMultipartUpload(ctx context.Context, path string, uploadID string, parts []fs.MultipartPart, opts ...fs.Option) error {
	path = driver.path(path)
	opt := &cos.CompleteMultipartUploadOptions{
		Parts: make([]cos.Object, len(parts)),
	}
	for i, part := range parts {
		opt.Parts[i] = cos.Object{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		}
	}
	_, _, err := driver.client.Object.CompleteMultipartUpload(ctx, path, uploadID, opt)
	return err
}

func (driver *cosFs) AbortMultipartUpload(ctx context.Context, path string, uploadID string, opts ...fs.Option) error {
	path = driver.path(path)
	_, err := driver.client.Object.AbortMultipartUpload(ctx, path, uploadID)
	return err
}

func (driver *cosFs) ListMultipartUploads(ctx context.Context, opts ...fs.Option) ([]fs.MultipartUploadInfo, error) {
	var options fs.Options
	for _, opt := range opts {
		opt(&options)
	}
	prefix := options.MultipartPrefix
	if prefix != "" {
		prefix = driver.path(prefix)
	}
	opt := &cos.ListMultipartUploadsOptions{Prefix: prefix}
	uploads := make([]fs.MultipartUploadInfo, 0)
	for {
		result, _, err := driver.client.Bucket.ListMultipartUploads(ctx, opt)
		if err != nil {
			return nil, err
		}
		for _, upload := range result.Uploads {
			createTime, _ := time.Parse(time.RFC3339, upload.Initiated)
			uploads = append(uploads, fs.MultipartUploadInfo{
				UploadID:   upload.UploadID,
				Path:       upload.Key,
				CreateTime: createTime,
			})
		}
		if !result.IsTruncated {
			return uploads, nil
		}
		opt.KeyMarker, opt.UploadIDMarker = result.NextKeyMarker, result.NextUploadIDMarker
	}
}

func (driver *cosFs) ListUploadedParts(ctx context.Context, path string, uploadID string, opts ...fs.Option) ([]fs.MultipartPart, error) {
	path = driver.path(path)
	opt := &cos.ObjectListPartsOptions{}
	v, _, err := driver.client.Object.ListParts(ctx, path, uploadID, opt)
	if err != nil {
		return nil, err
	}

	parts := make([]fs.MultipartPart, len(v.Parts))
	for i, part := range v.Parts {
		parts[i] = fs.MultipartPart{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
			Size:       part.Size,
		}
	}
	return parts, nil
}
