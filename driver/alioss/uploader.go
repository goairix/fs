package alioss

import (
	"bytes"
	"context"
	"io"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/goairix/fs"
)

func (driver *ossFs) Uploader() fs.Uploader {
	return driver
}

func (driver *ossFs) Upload(ctx context.Context, path string, reader io.Reader, opts ...fs.Option) error {
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

func (driver *ossFs) InitMultipartUpload(ctx context.Context, path string, opts ...fs.Option) (string, error) {
	path = driver.path(path)
	o := &fs.Options{}
	for _, opt := range opts {
		opt(o)
	}
	options := buildPutObjectOptions(ctx, o.ContentType, o.ContentDisposition, nil)

	initMultipartUploadResult, err := driver.bucket.InitiateMultipartUpload(path, options...)
	if err != nil {
		return "", err
	}
	return initMultipartUploadResult.UploadID, nil
}

func (driver *ossFs) UploadPart(ctx context.Context, path string, uploadID string, partNumber int, data io.Reader, opts ...fs.Option) (string, error) {
	path = driver.path(path)
	initMultipartUploadResult := oss.InitiateMultipartUploadResult{
		Key:      path,
		UploadID: uploadID,
		Bucket:   driver.bucket.BucketName,
	}

	// 获取数据大小
	var partSize int64
	if seeker, ok := data.(io.Seeker); ok {
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return "", err
		}
		_, err = seeker.Seek(0, io.SeekStart)
		if err != nil {
			return "", err
		}
		partSize = size
	} else {
		// 如果无法获取大小，则先将数据读入内存
		buf := new(bytes.Buffer)
		size, err := io.Copy(buf, data)
		if err != nil {
			return "", err
		}
		data = buf
		partSize = size
	}

	part, err := driver.bucket.UploadPart(initMultipartUploadResult, data, partSize, partNumber, oss.WithContext(ctx))
	if err != nil {
		return "", err
	}

	return part.ETag, nil
}

func (driver *ossFs) CompleteMultipartUpload(ctx context.Context, path string, uploadID string, parts []fs.MultipartPart, opts ...fs.Option) error {
	path = driver.path(path)
	initMultipartUploadResult := oss.InitiateMultipartUploadResult{
		Key:      path,
		UploadID: uploadID,
		Bucket:   driver.bucket.BucketName,
	}
	ossParts := make([]oss.UploadPart, len(parts))
	for i, part := range parts {
		ossParts[i] = oss.UploadPart{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		}
	}
	_, err := driver.bucket.CompleteMultipartUpload(initMultipartUploadResult, ossParts, oss.WithContext(ctx))
	return err
}

func (driver *ossFs) AbortMultipartUpload(ctx context.Context, path string, uploadID string, opts ...fs.Option) error {
	path = driver.path(path)
	initMultipartUploadResult := oss.InitiateMultipartUploadResult{
		Key:      path,
		UploadID: uploadID,
		Bucket:   driver.bucket.BucketName,
	}
	return driver.bucket.AbortMultipartUpload(initMultipartUploadResult, oss.WithContext(ctx))
}

func (driver *ossFs) ListMultipartUploads(ctx context.Context, opts ...fs.Option) ([]fs.MultipartUploadInfo, error) {
	var options fs.Options
	for _, opt := range opts {
		opt(&options)
	}
	prefix := options.MultipartPrefix
	if prefix != "" {
		prefix = driver.path(prefix)
	}
	var keyMarker, uploadIDMarker string
	uploads := make([]fs.MultipartUploadInfo, 0)
	for {
		result, err := driver.bucket.ListMultipartUploads(
			oss.WithContext(ctx), oss.Prefix(prefix),
			oss.KeyMarker(keyMarker), oss.UploadIDMarker(uploadIDMarker),
		)
		if err != nil {
			return nil, err
		}
		for _, upload := range result.Uploads {
			uploads = append(uploads, fs.MultipartUploadInfo{
				UploadID:   upload.UploadID,
				Path:       upload.Key,
				CreateTime: upload.Initiated,
			})
		}
		if !result.IsTruncated {
			return uploads, nil
		}
		keyMarker, uploadIDMarker = result.NextKeyMarker, result.NextUploadIDMarker
	}
}

func (driver *ossFs) ListUploadedParts(ctx context.Context, path string, uploadID string, opts ...fs.Option) ([]fs.MultipartPart, error) {
	path = driver.path(path)
	initMultipartUploadResult := oss.InitiateMultipartUploadResult{
		Key:      path,
		UploadID: uploadID,
		Bucket:   driver.bucket.BucketName,
	}

	// 列出已上传的分片
	lpr, err := driver.bucket.ListUploadedParts(initMultipartUploadResult, oss.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	parts := make([]fs.MultipartPart, 0, len(lpr.UploadedParts))
	for _, part := range lpr.UploadedParts {
		parts = append(parts, fs.MultipartPart{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
			Size:       int64(part.Size),
		})
	}
	return parts, nil
}
