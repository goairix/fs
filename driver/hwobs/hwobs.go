package hwobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/goairix/fs"
	"github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"
)

type Config struct {
	Endpoint        string        // OBS服务地址
	AccessKeyID     string        // AccessKey
	SecretAccessKey string        // SecretKey
	BucketName      string        // 存储桶名称
	SubPath         string        // 子目录路径
	AccessMode      fs.AccessMode // 访问模式
}

// obsFs OBS文件系统
type obsFs struct {
	client *obs.ObsClient
	config Config
}

func New(config Config) (fs.FileSystem, error) {
	// 初始化OBS客户端
	client, err := obs.New(config.AccessKeyID, config.SecretAccessKey, config.Endpoint)
	if err != nil {
		return nil, err
	}

	return &obsFs{
		client: client,
		config: config,
	}, nil
}

func (driver *obsFs) List(ctx context.Context, path string, opts ...fs.Option) ([]fs.FileInfo, error) {
	path = driver.path(path)
	var fileInfos []fs.FileInfo
	prefix := strings.TrimRight(path, "/")
	if prefix != "" {
		prefix += "/"
	}

	marker := ""
	for {
		input := &obs.ListObjectsInput{
			Bucket: driver.config.BucketName,
			Marker: marker,
		}
		input.Prefix = prefix

		output, err := driver.client.ListObjects(input)
		if err != nil {
			return nil, err
		}

		// 添加文件
		for _, object := range output.Contents {
			fileInfos = append(fileInfos, newObsFileInfo(object))
		}

		// 添加目录
		for _, prefix := range output.CommonPrefixes {
			fileInfos = append(fileInfos, newObsFileInfo(obs.Content{
				Key: prefix,
			}))
		}

		if !output.IsTruncated {
			break
		}
		marker = output.NextMarker
	}

	return fileInfos, nil
}

func (driver *obsFs) MakeDir(ctx context.Context, path string, perm os.FileMode, opts ...fs.Option) error {
	// OBS目录在写入文件时自动创建
	return nil
}

func (driver *obsFs) RemoveDir(ctx context.Context, path string, opts ...fs.Option) error {
	path = driver.path(path)
	prefix := strings.TrimRight(path, "/") + "/"
	marker := ""
	for {
		input := &obs.ListObjectsInput{
			Bucket: driver.config.BucketName,
			Marker: marker,
		}
		input.Prefix = prefix

		output, err := driver.client.ListObjects(input)
		if err != nil {
			return err
		}

		for _, object := range output.Contents {
			_, err = driver.client.DeleteObject(&obs.DeleteObjectInput{
				Bucket: driver.config.BucketName,
				Key:    object.Key,
			})
			if err != nil {
				return err
			}
		}

		if !output.IsTruncated {
			break
		}
		marker = output.NextMarker
	}
	return nil
}

func (driver *obsFs) CopyDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	srcPrefix := strings.TrimRight(driver.path(src), "/") + "/"
	dstPrefix := strings.TrimRight(driver.path(dst), "/") + "/"
	marker := ""
	for {
		input := &obs.ListObjectsInput{
			Bucket: driver.config.BucketName,
			Marker: marker,
		}
		input.Prefix = srcPrefix
		output, err := driver.client.ListObjects(input)
		if err != nil {
			return err
		}
		for _, object := range output.Contents {
			dstKey := dstPrefix + strings.TrimPrefix(object.Key, srcPrefix)
			copyInput := &obs.CopyObjectInput{}
			copyInput.Bucket = driver.config.BucketName
			copyInput.Key = dstKey
			copyInput.CopySourceBucket = driver.config.BucketName
			copyInput.CopySourceKey = object.Key
			_, err = driver.client.CopyObject(copyInput)
			if err != nil {
				return err
			}
		}
		if !output.IsTruncated {
			break
		}
		marker = output.NextMarker
	}
	return nil
}

func (driver *obsFs) MoveDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	if err := driver.CopyDir(ctx, src, dst, opts...); err != nil {
		return err
	}
	return driver.RemoveDir(ctx, src, opts...)
}

func (driver *obsFs) RenameDir(ctx context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return driver.MoveDir(ctx, oldPath, newPath, opts...)
}

func (driver *obsFs) Create(ctx context.Context, path string, opts ...fs.Option) (io.WriteCloser, error) {
	path = driver.path(path)
	return newObsWriter(ctx, driver.client, driver.config.BucketName, path, opts...), nil
}

func (driver *obsFs) Open(ctx context.Context, path string, opts ...fs.Option) (io.ReadCloser, error) {
	path = driver.path(path)
	input := &obs.GetObjectInput{}
	input.Bucket = driver.config.BucketName
	input.Key = path
	output, err := driver.client.GetObject(input)
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

func (driver *obsFs) OpenFile(ctx context.Context, path string, flag int, perm os.FileMode, opts ...fs.Option) (io.ReadWriteCloser, error) {
	if flag&os.O_RDWR != 0 {
		return newObsReadWriter(ctx, driver.client, driver.config.BucketName, driver.path(path), opts...), nil
	}
	if flag&os.O_WRONLY != 0 {
		return newObsReadWriter(ctx, driver.client, driver.config.BucketName, path, opts...), nil
	}
	reader, err := driver.Open(ctx, path, opts...)
	if err != nil {
		return nil, err
	}
	return newObsReadOnlyWrapper(reader), nil
}

func (driver *obsFs) Remove(ctx context.Context, path string, opts ...fs.Option) error {
	path = driver.path(path)
	_, err := driver.client.DeleteObject(&obs.DeleteObjectInput{
		Bucket: driver.config.BucketName,
		Key:    path,
	})
	return err
}

func (driver *obsFs) Copy(ctx context.Context, src, dst string, opts ...fs.Option) error {
	src = driver.path(src)
	dst = driver.path(dst)
	input := &obs.CopyObjectInput{}
	input.Bucket = driver.config.BucketName
	input.Key = dst
	input.CopySourceBucket = driver.config.BucketName
	input.CopySourceKey = src
	_, err := driver.client.CopyObject(input)
	return err
}

func (driver *obsFs) Move(ctx context.Context, src, dst string, opts ...fs.Option) error {
	if err := driver.Copy(ctx, src, dst); err != nil {
		return err
	}
	return driver.Remove(ctx, src)
}

func (driver *obsFs) Rename(ctx context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return driver.Move(ctx, oldPath, newPath)
}

func (driver *obsFs) Stat(ctx context.Context, path string, opts ...fs.Option) (fs.FileInfo, error) {
	path = driver.path(path)
	input := &obs.GetObjectMetadataInput{
		Bucket: driver.config.BucketName,
		Key:    path,
	}
	output, err := driver.client.GetObjectMetadata(input)
	if err != nil {
		return nil, err
	}

	return newObsFileInfo(obs.Content{
		Key:          path,
		Size:         output.ContentLength,
		LastModified: output.LastModified,
	}), nil
}

func (driver *obsFs) GetMimeType(ctx context.Context, path string, opts ...fs.Option) (string, error) {
	input := &obs.GetObjectMetadataInput{
		Bucket: driver.config.BucketName,
		Key:    driver.path(path),
	}
	output, err := driver.client.GetObjectMetadata(input)
	if err != nil {
		return "", err
	}

	if output.ContentType != "" {
		return output.ContentType, nil
	}

	// 如果对象没有 Content-Type，则读取文件内容进行检测
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

func (driver *obsFs) SetMetadata(ctx context.Context, path string, metadata map[string]any, opts ...fs.Option) error {
	input := &obs.CopyObjectInput{}
	input.Bucket = driver.config.BucketName
	input.Key = driver.path(path)
	input.CopySourceBucket = driver.config.BucketName
	input.CopySourceKey = path + "_tmp"

	input.Metadata = make(map[string]string)
	for k, v := range metadata {
		input.Metadata[k] = fmt.Sprintf("%v", v)
	}

	_, err := driver.client.CopyObject(input)
	if err != nil {
		return err
	}

	return driver.Move(ctx, path+"_tmp", path)
}

func (driver *obsFs) GetMetadata(ctx context.Context, path string, opts ...fs.Option) (map[string]any, error) {
	path = driver.path(path)
	input := &obs.GetObjectMetadataInput{
		Bucket: driver.config.BucketName,
		Key:    path,
	}
	output, err := driver.client.GetObjectMetadata(input)
	if err != nil {
		return nil, err
	}

	metadata := make(map[string]interface{})
	for k, v := range output.Metadata {
		metadata[k] = v
	}
	return metadata, nil
}

func (driver *obsFs) Exists(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	if ok, err := driver.IsFile(ctx, path); err == nil && ok {
		return true, nil
	}
	return driver.IsDir(ctx, path)
}

func (driver *obsFs) IsDir(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	path = strings.TrimRight(driver.path(path), "/") + "/"
	input := &obs.ListObjectsInput{
		Bucket: driver.config.BucketName,
	}
	input.Prefix = path
	input.Delimiter = "/"
	input.MaxKeys = 1
	output, err := driver.client.ListObjects(input)
	if err != nil {
		return false, err
	}
	return len(output.Contents) > 0 || len(output.CommonPrefixes) > 0, nil
}

func (driver *obsFs) IsFile(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	path = driver.path(path)
	_, err := driver.client.GetObjectMetadata(&obs.GetObjectMetadataInput{
		Bucket: driver.config.BucketName,
		Key:    path,
	})
	if err != nil {
		var obsErr obs.ObsError
		if errors.As(err, &obsErr) && obsErr.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (driver *obsFs) path(path string) string {
	if driver.config.SubPath != "" {
		return strings.Trim(driver.config.SubPath, "/") + "/" + path
	}
	return path
}
