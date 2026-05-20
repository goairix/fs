package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/goairix/fs"
)

type Config struct {
	Region          string        // AWS 区域
	Endpoint        string        // S3 服务地址（可选，用于兼容其他 S3 协议的存储服务）
	AccessKeyID     string        // AccessKey
	SecretAccessKey string        // SecretKey
	BucketName      string        // 存储桶名称
	SubPath         string        // 子目录路径
	UsePathStyle    bool          // 是否使用路径样式访问
	AccessMode      fs.AccessMode // 访问模式
}

// s3Fs S3文件系统
type s3Fs struct {
	client *s3.Client
	config Config
}

func New(conf Config) (fs.FileSystem, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(conf.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			conf.AccessKeyID,
			conf.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, err
	}

	// 如果指定了自定义endpoint，则使用自定义endpoint
	if conf.Endpoint != "" {
		cfg.BaseEndpoint = aws.String(conf.Endpoint)
	}

	client := s3.NewFromConfig(
		cfg,
		func(o *s3.Options) {
			o.UsePathStyle = conf.UsePathStyle
		},
	)

	return &s3Fs{
		client: client,
		config: conf,
	}, nil
}

func (driver *s3Fs) List(ctx context.Context, path string, opts ...fs.Option) ([]fs.FileInfo, error) {
	path = driver.path(path)
	var fileInfos []fs.FileInfo
	prefix := strings.TrimRight(path, "/")
	if prefix != "" {
		prefix += "/"
	}

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(driver.config.BucketName),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	}

	paginator := s3.NewListObjectsV2Paginator(driver.client, input)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		// 添加文件
		for _, object := range output.Contents {
			fileInfos = append(fileInfos, newS3FileInfo(object))
		}

		// 添加目录
		for _, prefix := range output.CommonPrefixes {
			fileInfos = append(fileInfos, newS3FileInfo(types.Object{
				Key: prefix.Prefix,
			}))
		}
	}

	return fileInfos, nil
}

func (driver *s3Fs) MakeDir(_ context.Context, _ string, _ os.FileMode, opts ...fs.Option) error {
	// S3目录在写入文件时自动创建
	return nil
}

func (driver *s3Fs) RemoveDir(ctx context.Context, path string, opts ...fs.Option) error {
	path = driver.path(path)
	prefix := strings.TrimRight(path, "/") + "/"

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(driver.config.BucketName),
		Prefix: aws.String(prefix),
	}

	paginator := s3.NewListObjectsV2Paginator(driver.client, input)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}

		for _, object := range output.Contents {
			_, err = driver.client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(driver.config.BucketName),
				Key:    object.Key,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (driver *s3Fs) CopyDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	srcPrefix := strings.TrimRight(driver.path(src), "/") + "/"
	dstPrefix := strings.TrimRight(driver.path(dst), "/") + "/"
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(driver.config.BucketName),
		Prefix: aws.String(srcPrefix),
	}
	paginator := s3.NewListObjectsV2Paginator(driver.client, input)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, object := range output.Contents {
			dstKey := dstPrefix + strings.TrimPrefix(*object.Key, srcPrefix)
			_, err = driver.client.CopyObject(ctx, &s3.CopyObjectInput{
				Bucket:     aws.String(driver.config.BucketName),
				Key:        aws.String(dstKey),
				CopySource: aws.String(fmt.Sprintf("%s/%s", driver.config.BucketName, *object.Key)),
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (driver *s3Fs) MoveDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	if err := driver.CopyDir(ctx, src, dst, opts...); err != nil {
		return err
	}
	return driver.RemoveDir(ctx, src, opts...)
}

func (driver *s3Fs) RenameDir(ctx context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return driver.MoveDir(ctx, oldPath, newPath, opts...)
}

func (driver *s3Fs) Create(ctx context.Context, path string, opts ...fs.Option) (io.WriteCloser, error) {
	path = driver.path(path)
	return newS3Writer(ctx, driver.client, driver.config.BucketName, path, opts...), nil
}

func (driver *s3Fs) Open(ctx context.Context, path string, opts ...fs.Option) (io.ReadCloser, error) {
	path = driver.path(path)
	output, err := driver.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(driver.config.BucketName),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

func (driver *s3Fs) OpenFile(ctx context.Context, path string, flag int, perm os.FileMode, opts ...fs.Option) (io.ReadWriteCloser, error) {
	if flag&os.O_RDWR != 0 {
		return newS3ReadWriter(ctx, driver.client, driver.config.BucketName, driver.path(path), opts...), nil
	}
	if flag&os.O_WRONLY != 0 {
		return newS3ReadWriter(ctx, driver.client, driver.config.BucketName, driver.path(path), opts...), nil
	}
	reader, err := driver.Open(ctx, path, opts...)
	if err != nil {
		return nil, err
	}
	return newS3ReadOnlyWrapper(reader), nil
}

func (driver *s3Fs) Remove(ctx context.Context, path string, opts ...fs.Option) error {
	path = driver.path(path)
	_, err := driver.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(driver.config.BucketName),
		Key:    aws.String(path),
	})
	return err
}

func (driver *s3Fs) Copy(ctx context.Context, src, dst string, opts ...fs.Option) error {
	src = driver.path(src)
	dst = driver.path(dst)
	_, err := driver.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(driver.config.BucketName),
		Key:        aws.String(dst),
		CopySource: aws.String(fmt.Sprintf("%s/%s", driver.config.BucketName, src)),
	})
	return err
}

func (driver *s3Fs) Move(ctx context.Context, src, dst string, opts ...fs.Option) error {
	if err := driver.Copy(ctx, src, dst); err != nil {
		return err
	}
	return driver.Remove(ctx, src)
}

func (driver *s3Fs) Rename(ctx context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return driver.Move(ctx, oldPath, newPath)
}

func (driver *s3Fs) Stat(ctx context.Context, path string, opts ...fs.Option) (fs.FileInfo, error) {
	path = driver.path(path)
	output, err := driver.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(driver.config.BucketName),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, err
	}

	return newS3FileInfo(types.Object{
		Key:          aws.String(path),
		Size:         output.ContentLength,
		LastModified: output.LastModified,
	}), nil
}

func (driver *s3Fs) GetMimeType(ctx context.Context, path string, opts ...fs.Option) (string, error) {
	output, err := driver.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(driver.config.BucketName),
		Key:    aws.String(driver.path(path)),
	})
	if err != nil {
		return "", err
	}

	if output.ContentType != nil {
		return *output.ContentType, nil
	}

	// 如果对象没有ContentType，则读取文件内容进行检测
	obj, err := driver.Open(ctx, path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = obj.Close()
	}()

	buffer := make([]byte, 512)
	n, err := obj.Read(buffer)
	if err != nil && err != io.EOF {
		return "", err
	}

	return http.DetectContentType(buffer[:n]), nil
}

func (driver *s3Fs) SetMetadata(ctx context.Context, path string, metadata map[string]any, opts ...fs.Option) error {
	input := &s3.CopyObjectInput{
		Bucket:     aws.String(driver.config.BucketName),
		Key:        aws.String(driver.path(path + "_tmp")),
		CopySource: aws.String(fmt.Sprintf("%s/%s", driver.config.BucketName, path)),
		Metadata:   make(map[string]string),
	}

	for k, v := range metadata {
		input.Metadata[k] = fmt.Sprintf("%v", v)
	}

	_, err := driver.client.CopyObject(ctx, input)
	if err != nil {
		return err
	}

	return driver.Move(ctx, path+"_tmp", path)
}

func (driver *s3Fs) GetMetadata(ctx context.Context, path string, opts ...fs.Option) (map[string]any, error) {
	path = driver.path(path)
	output, err := driver.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(driver.config.BucketName),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, err
	}

	metadata := make(map[string]interface{})
	for k, v := range output.Metadata {
		metadata[k] = v
	}
	return metadata, nil
}

func (driver *s3Fs) Exists(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	if ok, err := driver.IsFile(ctx, path); err == nil && ok {
		return true, nil
	}
	return driver.IsDir(ctx, path)
}

func (driver *s3Fs) IsDir(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	path = driver.path(path)
	path = strings.TrimRight(path, "/") + "/"
	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(driver.config.BucketName),
		Prefix:    aws.String(path),
		Delimiter: aws.String("/"),
		MaxKeys:   aws.Int32(1),
	}

	output, err := driver.client.ListObjectsV2(ctx, input)
	if err != nil {
		return false, err
	}
	return len(output.Contents) > 0 || len(output.CommonPrefixes) > 0, nil
}

func (driver *s3Fs) IsFile(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	path = driver.path(path)
	_, err := driver.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(driver.config.BucketName),
		Key:    aws.String(path),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (driver *s3Fs) path(path string) string {
	if driver.config.SubPath != "" {
		return strings.Trim(driver.config.SubPath, "/") + "/" + path
	}
	return path
}
