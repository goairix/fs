package minio

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/goairix/fs"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint        string        // MinIO服务地址
	AccessKeyID     string        // AccessKey
	SecretAccessKey string        // SecretKey
	UseSSL          bool          // 是否使用SSL
	BucketName      string        // 存储桶名称
	SubPath         string        // 子目录路径
	Location        string        // 区域
	AccessMode      fs.AccessMode // 访问模式
}

// minioFs MinIO文件系统
type minioFs struct {
	client *minio.Client
	core   *minio.Core
	config Config
}

func New(config Config) (fs.FileSystem, error) {
	// 初始化MinIO客户端
	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	// 确保bucket存在
	exists, err := client.BucketExists(context.Background(), config.BucketName)
	if err != nil {
		return nil, err
	}

	if !exists {
		err = client.MakeBucket(context.Background(), config.BucketName, minio.MakeBucketOptions{
			Region: config.Location,
		})
		if err != nil {
			return nil, err
		}
	}

	return &minioFs{
		client: client,
		core:   &minio.Core{Client: client},
		config: config,
	}, nil
}

func (driver *minioFs) List(ctx context.Context, path string, opts ...fs.Option) ([]fs.FileInfo, error) {
	path = driver.path(path)
	var fileInfos []fs.FileInfo

	// 使用ListObjects来获取指定前缀的对象
	prefix := strings.TrimRight(path, "/")
	if prefix != "" {
		prefix += "/"
	}
	options := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	}

	for object := range driver.client.ListObjects(ctx, driver.config.BucketName, options) {
		if object.Err != nil {
			return nil, object.Err
		}
		fileInfos = append(fileInfos, newMinioFileInfo(object))
	}

	return fileInfos, nil
}

func (driver *minioFs) MakeDir(_ context.Context, _ string, _ os.FileMode, opts ...fs.Option) error {
	// MinIO目录在写入文件时自动创建
	return nil
}

func (driver *minioFs) RemoveDir(ctx context.Context, path string, opts ...fs.Option) error {
	path = driver.path(path)
	options := minio.ListObjectsOptions{
		Prefix:    filepath.Clean(path) + "/",
		Recursive: true,
	}

	// 删除目录下的所有对象
	for object := range driver.client.ListObjects(ctx, driver.config.BucketName, options) {
		err := driver.client.RemoveObject(ctx, driver.config.BucketName, object.Key, minio.RemoveObjectOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (driver *minioFs) CopyDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	srcPrefix := strings.TrimRight(driver.path(src), "/") + "/"
	dstPrefix := strings.TrimRight(driver.path(dst), "/") + "/"
	listOpts := minio.ListObjectsOptions{
		Prefix:    srcPrefix,
		Recursive: true,
	}
	for object := range driver.client.ListObjects(ctx, driver.config.BucketName, listOpts) {
		if object.Err != nil {
			return object.Err
		}
		dstKey := dstPrefix + strings.TrimPrefix(object.Key, srcPrefix)
		_, err := driver.client.CopyObject(ctx,
			minio.CopyDestOptions{Bucket: driver.config.BucketName, Object: dstKey},
			minio.CopySrcOptions{Bucket: driver.config.BucketName, Object: object.Key},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (driver *minioFs) MoveDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	if err := driver.CopyDir(ctx, src, dst, opts...); err != nil {
		return err
	}
	return driver.RemoveDir(ctx, src, opts...)
}

func (driver *minioFs) RenameDir(ctx context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return driver.MoveDir(ctx, oldPath, newPath, opts...)
}

func (driver *minioFs) Create(ctx context.Context, path string, opts ...fs.Option) (io.WriteCloser, error) {
	path = driver.path(path)
	return newMinioWriter(ctx, driver.client, driver.config.BucketName, path, opts...), nil
}

func (driver *minioFs) Open(ctx context.Context, path string, opts ...fs.Option) (io.ReadCloser, error) {
	path = driver.path(path)
	return driver.client.GetObject(ctx, driver.config.BucketName, path, minio.GetObjectOptions{})
}

func (driver *minioFs) OpenFile(ctx context.Context, path string, flag int, _ os.FileMode, opts ...fs.Option) (io.ReadWriteCloser, error) {
	// MinIO不支持追加模式，这里实现读写功能
	if flag&os.O_RDWR != 0 {
		return newMinioReadWriter(ctx, driver.client, driver.config.BucketName, driver.path(path), opts...), nil
	}
	if flag&os.O_WRONLY != 0 {
		// 对于只写模式，也返回 ReadWriter，但读取时会返回错误
		return newMinioReadWriter(ctx, driver.client, driver.config.BucketName, driver.path(path), opts...), nil
	}
	// 对于只读模式，包装成 ReadWriteCloser
	reader, err := driver.Open(ctx, path, opts...)
	if err != nil {
		return nil, err
	}
	return newMinioReadOnlyWrapper(reader), nil
}

func (driver *minioFs) Remove(ctx context.Context, path string, opts ...fs.Option) error {
	path = driver.path(path)
	return driver.client.RemoveObject(ctx, driver.config.BucketName, path, minio.RemoveObjectOptions{})
}

func (driver *minioFs) Copy(ctx context.Context, src, dst string, opts ...fs.Option) error {
	src = driver.path(src)
	dst = driver.path(dst)
	_, err := driver.client.CopyObject(ctx,
		minio.CopyDestOptions{
			Bucket: driver.config.BucketName,
			Object: dst,
		},
		minio.CopySrcOptions{
			Bucket: driver.config.BucketName,
			Object: src,
		})
	return err
}

func (driver *minioFs) Move(ctx context.Context, src, dst string, opts ...fs.Option) error {
	// 先复制后删除来实现移动
	if err := driver.Copy(ctx, src, dst); err != nil {
		return err
	}
	return driver.Remove(ctx, src)
}

func (driver *minioFs) Rename(ctx context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return driver.Move(ctx, oldPath, newPath)
}

func (driver *minioFs) Stat(ctx context.Context, path string, opts ...fs.Option) (fs.FileInfo, error) {
	path = driver.path(path)
	info, err := driver.client.StatObject(ctx, driver.config.BucketName, path, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	return newMinioFileInfo(info), nil
}

func (driver *minioFs) GetMimeType(ctx context.Context, path string, opts ...fs.Option) (string, error) {
	stat, err := driver.client.StatObject(ctx, driver.config.BucketName, driver.path(path), minio.StatObjectOptions{})
	if err != nil {
		return "", err
	}

	if stat.ContentType != "" {
		return stat.ContentType, nil
	}

	// 如果对象没有 ContentType，则读取文件内容进行检测
	obj, err := driver.Open(ctx, path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = obj.Close()
	}()

	buffer := make([]byte, 512)
	_, err = obj.Read(buffer)
	if err != nil && err != io.EOF {
		return "", err
	}

	return http.DetectContentType(buffer), nil
}

func (driver *minioFs) SetMetadata(ctx context.Context, path string, metadata map[string]any, opts ...fs.Option) error {
	// 将metadata转换为字符串map
	strMetadata := make(map[string]string)
	for k, v := range metadata {
		strMetadata[k] = fmt.Sprintf("%v", v)
	}

	// MinIO中需要通过复制对象来更新元数据
	_, err := driver.client.CopyObject(ctx,
		minio.CopyDestOptions{
			Bucket:          driver.config.BucketName,
			Object:          driver.path(path + "_tmp"),
			ReplaceMetadata: true,
			UserMetadata:    strMetadata,
		},
		minio.CopySrcOptions{
			Bucket: driver.config.BucketName,
			Object: driver.path(path),
		})
	if err != nil {
		return err
	}
	return driver.Move(ctx, path+"_tmp", path)
}

func (driver *minioFs) GetMetadata(ctx context.Context, path string, opts ...fs.Option) (map[string]any, error) {
	path = driver.path(path)
	info, err := driver.client.StatObject(ctx, driver.config.BucketName, path, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}

	metadata := make(map[string]interface{})
	for k, v := range info.UserMetadata {
		metadata[k] = v
	}
	return metadata, nil
}

func (driver *minioFs) Exists(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	// 先检查是否为文件
	if ok, err := driver.IsFile(ctx, path); err == nil && ok {
		return true, nil
	}

	// 如果不是文件，检查是否为目录
	return driver.IsDir(ctx, path)
}

func (driver *minioFs) IsDir(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	path = driver.path(path)
	options := minio.ListObjectsOptions{
		Prefix:    strings.TrimRight(path, "/") + "/",
		Recursive: false,
		MaxKeys:   1,
	}

	objectChan := driver.client.ListObjects(ctx, driver.config.BucketName, options)
	object, ok := <-objectChan
	if !ok {
		return false, nil
	}
	if object.Err != nil {
		return false, object.Err
	}
	return true, nil
}

func (driver *minioFs) IsFile(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	path = driver.path(path)
	_, err := driver.client.StatObject(ctx, driver.config.BucketName, path, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "The specified key does not exist.") {
		return false, nil
	}
	return false, err
}

func (driver *minioFs) path(path string) string {
	if driver.config.SubPath != "" {
		return strings.Trim(driver.config.SubPath, "/") + "/" + path
	}
	return path
}
