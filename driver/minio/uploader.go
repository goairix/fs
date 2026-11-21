package minio

import (
	"bytes"
	"context"
	"io"

	"github.com/goairix/fs"
	"github.com/minio/minio-go/v7"
)

func (driver *minioFs) Uploader() fs.Uploader {
	return driver
}

func (driver *minioFs) Upload(ctx context.Context, path string, reader io.Reader, opts ...fs.Option) error {
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

func (driver *minioFs) InitMultipartUpload(ctx context.Context, path string, opts ...fs.Option) (string, error) {
	path = driver.path(path)
	o := &fs.Options{}
	for _, opt := range opts {
		opt(o)
	}
	options := buildPutObjectOptions(o.ContentType, o.ContentDisposition, nil)
	uploadID, err := driver.core.NewMultipartUpload(ctx, driver.config.BucketName, path, options)
	if err != nil {
		return "", err
	}
	return uploadID, nil
}

func (driver *minioFs) UploadPart(ctx context.Context, path string, uploadID string, partNumber int, data io.Reader, opts ...fs.Option) (string, error) {
	path = driver.path(path)
	// 计算数据大小
	var size int64
	if seeker, ok := data.(io.Seeker); ok {
		var err error
		size, err = seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return "", err
		}
		_, err = seeker.Seek(0, io.SeekStart)
		if err != nil {
			return "", err
		}
	} else {
		// 如果无法获取大小，则先将数据读入内存
		buf := new(bytes.Buffer)
		var err error
		size, err = io.Copy(buf, data)
		if err != nil {
			return "", err
		}
		data = buf
	}

	part, err := driver.core.PutObjectPart(ctx, driver.config.BucketName, path, uploadID, partNumber, data, size, minio.PutObjectPartOptions{})
	if err != nil {
		return "", err
	}

	return part.ETag, nil
}

func (driver *minioFs) CompleteMultipartUpload(ctx context.Context, path string, uploadID string, parts []fs.MultipartPart, opts ...fs.Option) error {
	path = driver.path(path)
	// 转换分片信息格式
	completeParts := make([]minio.CompletePart, len(parts))
	for i, part := range parts {
		completeParts[i] = minio.CompletePart{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		}
	}

	_, err := driver.core.CompleteMultipartUpload(ctx, driver.config.BucketName, path, uploadID, completeParts, minio.PutObjectOptions{})
	return err
}

func (driver *minioFs) AbortMultipartUpload(ctx context.Context, path string, uploadID string, opts ...fs.Option) error {
	path = driver.path(path)
	return driver.core.AbortMultipartUpload(ctx, driver.config.BucketName, path, uploadID)
}

func (driver *minioFs) ListMultipartUploads(ctx context.Context, opts ...fs.Option) ([]fs.MultipartUploadInfo, error) {
	var options fs.Options
	for _, opt := range opts {
		opt(&options)
	}
	prefix := options.MultipartPrefix
	if prefix != "" {
		prefix = driver.path(prefix)
	}
	var uploads []fs.MultipartUploadInfo
	for multipart := range driver.client.ListIncompleteUploads(ctx, driver.config.BucketName, prefix, true) {
		if multipart.Err != nil {
			return nil, multipart.Err
		}
		uploads = append(uploads, fs.MultipartUploadInfo{
			UploadID:   multipart.UploadID,
			Path:       multipart.Key,
			CreateTime: multipart.Initiated,
		})
	}
	return uploads, nil
}

func (driver *minioFs) ListUploadedParts(ctx context.Context, path string, uploadID string, opts ...fs.Option) ([]fs.MultipartPart, error) {
	path = driver.path(path)

	var parts []fs.MultipartPart

	// 初始化参数
	partNumberMarker := 0
	maxParts := 1000 // 每次获取的最大分片数

	for {
		// 获取分片列表
		result, err := driver.core.ListObjectParts(ctx, driver.config.BucketName, path, uploadID, partNumberMarker, maxParts)
		if err != nil {
			return nil, err
		}

		// 添加分片信息
		for _, part := range result.ObjectParts {
			parts = append(parts, fs.MultipartPart{
				PartNumber: part.PartNumber,
				ETag:       part.ETag,
				Size:       part.Size,
			})
		}

		// 如果所有分片都已获取，退出循环
		if !result.IsTruncated {
			break
		}

		// 更新partNumberMarker，继续获取下一批分片
		partNumberMarker = result.NextPartNumberMarker
	}

	return parts, nil
}
