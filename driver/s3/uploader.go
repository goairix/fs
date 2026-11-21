package s3

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/goairix/fs"
)

func (driver *s3Fs) Uploader() fs.Uploader {
	return driver
}

func (driver *s3Fs) Upload(ctx context.Context, path string, reader io.Reader, opts ...fs.Option) error {
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

func buildCreateMultipartUploadInput(bucket, path string, options *fs.Options) *s3.CreateMultipartUploadInput {
	input := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(path),
	}
	if options.ContentType != "" {
		input.ContentType = aws.String(options.ContentType)
	}
	if options.ContentDisposition != "" {
		input.ContentDisposition = aws.String(options.ContentDisposition)
	}
	return input
}

func (driver *s3Fs) InitMultipartUpload(ctx context.Context, path string, opts ...fs.Option) (string, error) {
	path = driver.path(path)
	o := &fs.Options{}
	for _, opt := range opts {
		opt(o)
	}
	input := buildCreateMultipartUploadInput(driver.config.BucketName, path, o)
	output, err := driver.client.CreateMultipartUpload(ctx, input)
	if err != nil {
		return "", err
	}
	return *output.UploadId, nil
}

func (driver *s3Fs) UploadPart(ctx context.Context, path string, uploadID string, partNumber int, data io.Reader, opts ...fs.Option) (string, error) {
	path = driver.path(path)
	input := &s3.UploadPartInput{
		Bucket:     aws.String(driver.config.BucketName),
		Key:        aws.String(path),
		PartNumber: aws.Int32(int32(partNumber)),
		UploadId:   aws.String(uploadID),
		Body:       data,
	}
	output, err := driver.client.UploadPart(ctx, input)
	if err != nil {
		return "", err
	}
	return *output.ETag, nil
}

func (driver *s3Fs) CompleteMultipartUpload(ctx context.Context, path string, uploadID string, parts []fs.MultipartPart, opts ...fs.Option) error {
	path = driver.path(path)
	completedParts := make([]types.CompletedPart, len(parts))
	for i, part := range parts {
		completedParts[i] = types.CompletedPart{
			PartNumber: aws.Int32(int32(part.PartNumber)),
			ETag:       aws.String(part.ETag),
		}
	}
	input := &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(driver.config.BucketName),
		Key:             aws.String(path),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completedParts},
	}
	_, err := driver.client.CompleteMultipartUpload(ctx, input)
	return err
}

func (driver *s3Fs) AbortMultipartUpload(ctx context.Context, path string, uploadID string, opts ...fs.Option) error {
	path = driver.path(path)
	input := &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(driver.config.BucketName),
		Key:      aws.String(path),
		UploadId: aws.String(uploadID),
	}
	_, err := driver.client.AbortMultipartUpload(ctx, input)
	return err
}

func (driver *s3Fs) ListMultipartUploads(ctx context.Context, opts ...fs.Option) ([]fs.MultipartUploadInfo, error) {
	var options fs.Options
	for _, opt := range opts {
		opt(&options)
	}
	prefix := options.MultipartPrefix
	if prefix != "" {
		prefix = driver.path(prefix)
	}
	input := &s3.ListMultipartUploadsInput{
		Bucket: aws.String(driver.config.BucketName),
	}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	uploads := make([]fs.MultipartUploadInfo, 0)
	for {
		result, err := driver.client.ListMultipartUploads(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, upload := range result.Uploads {
			uploads = append(uploads, fs.MultipartUploadInfo{
				UploadID:   *upload.UploadId,
				Path:       *upload.Key,
				CreateTime: *upload.Initiated,
			})
		}
		if !aws.ToBool(result.IsTruncated) {
			return uploads, nil
		}
		input.KeyMarker, input.UploadIdMarker = result.NextKeyMarker, result.NextUploadIdMarker
	}
}

func (driver *s3Fs) ListUploadedParts(ctx context.Context, path string, uploadID string, opts ...fs.Option) ([]fs.MultipartPart, error) {
	path = driver.path(path)
	input := &s3.ListPartsInput{
		Bucket:   aws.String(driver.config.BucketName),
		Key:      aws.String(path),
		UploadId: aws.String(uploadID),
	}

	result, err := driver.client.ListParts(ctx, input)
	if err != nil {
		return nil, err
	}

	parts := make([]fs.MultipartPart, len(result.Parts))
	for i, part := range result.Parts {
		parts[i] = fs.MultipartPart{
			PartNumber: int(*part.PartNumber),
			ETag:       *part.ETag,
			Size:       *part.Size,
		}
	}
	return parts, nil
}
