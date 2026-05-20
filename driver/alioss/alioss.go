package alioss

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/goairix/fs"
)

type Config struct {
	Endpoint        string        // OSS服务地址
	AccessKeyID     string        // AccessKey
	SecretAccessKey string        // SecretKey
	BucketName      string        // 存储桶名称
	SubPath         string        // 子目录路径
	AccessMode      fs.AccessMode // 访问模式
}

// ossFs OSS文件系统
type ossFs struct {
	client *oss.Client
	bucket *oss.Bucket
	config Config
}

func New(config Config) (fs.FileSystem, error) {
	// 初始化OSS客户端
	client, err := oss.New(config.Endpoint, config.AccessKeyID, config.SecretAccessKey)
	if err != nil {
		return nil, err
	}

	// 获取存储桶
	bucket, err := client.Bucket(config.BucketName)
	if err != nil {
		return nil, err
	}

	return &ossFs{
		client: client,
		bucket: bucket,
		config: config,
	}, nil
}

func (driver *ossFs) List(ctx context.Context, path string, opts ...fs.Option) ([]fs.FileInfo, error) {
	path = driver.path(path)
	var fileInfos []fs.FileInfo
	prefix := strings.TrimRight(path, "/")
	if prefix != "" {
		prefix += "/"
	}

	marker := ""
	for {
		lsRes, err := driver.bucket.ListObjects(
			oss.Marker(marker),
			oss.Prefix(prefix),
			oss.Delimiter("/"),
			oss.WithContext(ctx),
		)
		if err != nil {
			return nil, err
		}

		// 添加文件
		for _, object := range lsRes.Objects {
			fileInfos = append(fileInfos, newOssFileInfo(object))
		}

		// 添加目录
		for _, prefix := range lsRes.CommonPrefixes {
			fileInfos = append(fileInfos, newOssFileInfo(oss.ObjectProperties{
				Key: prefix,
			}))
		}

		if !lsRes.IsTruncated {
			break
		}
		marker = lsRes.NextMarker
	}

	return fileInfos, nil
}

func (driver *ossFs) MakeDir(_ context.Context, _ string, _ os.FileMode, opts ...fs.Option) error {
	// OSS目录在写入文件时自动创建
	return nil
}

func (driver *ossFs) RemoveDir(ctx context.Context, path string, opts ...fs.Option) error {
	path = driver.path(path)
	prefix := strings.TrimRight(path, "/") + "/"
	marker := ""
	for {
		lsRes, err := driver.bucket.ListObjects(oss.Marker(marker), oss.Prefix(prefix), oss.WithContext(ctx))
		if err != nil {
			return err
		}

		for _, object := range lsRes.Objects {
			err = driver.bucket.DeleteObject(object.Key, oss.WithContext(ctx))
			if err != nil {
				return err
			}
		}

		if !lsRes.IsTruncated {
			break
		}
		marker = lsRes.NextMarker
	}
	return nil
}

func (driver *ossFs) CopyDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	srcPrefix := strings.TrimRight(driver.path(src), "/") + "/"
	dstPrefix := strings.TrimRight(driver.path(dst), "/") + "/"
	marker := ""
	for {
		lsRes, err := driver.bucket.ListObjects(
			oss.Marker(marker),
			oss.Prefix(srcPrefix),
			oss.WithContext(ctx),
		)
		if err != nil {
			return err
		}
		for _, object := range lsRes.Objects {
			dstKey := dstPrefix + strings.TrimPrefix(object.Key, srcPrefix)
			_, err = driver.bucket.CopyObject(object.Key, dstKey, oss.WithContext(ctx))
			if err != nil {
				return err
			}
		}
		if !lsRes.IsTruncated {
			break
		}
		marker = lsRes.NextMarker
	}
	return nil
}

func (driver *ossFs) MoveDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	if err := driver.CopyDir(ctx, src, dst, opts...); err != nil {
		return err
	}
	return driver.RemoveDir(ctx, src, opts...)
}

func (driver *ossFs) RenameDir(ctx context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return driver.MoveDir(ctx, oldPath, newPath, opts...)
}

func (driver *ossFs) Create(ctx context.Context, path string, opts ...fs.Option) (io.WriteCloser, error) {
	path = driver.path(path)
	return newOssWriter(ctx, driver.bucket, path, opts...), nil
}

func (driver *ossFs) Open(ctx context.Context, path string, opts ...fs.Option) (io.ReadCloser, error) {
	path = driver.path(path)
	return driver.bucket.GetObject(path, oss.WithContext(ctx))
}

func (driver *ossFs) OpenFile(ctx context.Context, path string, flag int, _ os.FileMode, opts ...fs.Option) (io.ReadWriteCloser, error) {
	if flag&os.O_RDWR != 0 {
		return newOssReadWriter(ctx, driver.bucket, driver.path(path), opts...), nil
	}
	if flag&os.O_WRONLY != 0 {
		return newOssReadWriter(ctx, driver.bucket, driver.path(path), opts...), nil
	}
	reader, err := driver.Open(ctx, path, opts...)
	if err != nil {
		return nil, err
	}
	return newOssReadOnlyWrapper(reader), nil
}

func (driver *ossFs) Remove(ctx context.Context, path string, opts ...fs.Option) error {
	return driver.bucket.DeleteObject(driver.path(path), oss.WithContext(ctx))
}

func (driver *ossFs) Copy(ctx context.Context, src, dst string, opts ...fs.Option) error {
	src = driver.path(src)
	dst = driver.path(dst)
	_, err := driver.bucket.CopyObject(src, dst, oss.WithContext(ctx))
	return err
}

func (driver *ossFs) Move(ctx context.Context, src, dst string, opts ...fs.Option) error {
	if err := driver.Copy(ctx, src, dst); err != nil {
		return err
	}
	return driver.Remove(ctx, src)
}

func (driver *ossFs) Rename(ctx context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return driver.Move(ctx, oldPath, newPath)
}

func (driver *ossFs) Stat(ctx context.Context, path string, opts ...fs.Option) (fs.FileInfo, error) {
	path = driver.path(path)
	header, err := driver.bucket.GetObjectMeta(path, oss.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	lastModified, _ := time.ParseInLocation(time.RFC1123, header.Get("Last-Modified"), time.Local)
	fileSize, _ := strconv.ParseInt(header.Get("Content-Length"), 10, 64)

	return newOssFileInfo(oss.ObjectProperties{
		Key:          path,
		Size:         fileSize,
		LastModified: lastModified,
	}), nil
}

func (driver *ossFs) GetMimeType(ctx context.Context, path string, opts ...fs.Option) (string, error) {
	header, err := driver.bucket.GetObjectDetailedMeta(path, oss.WithContext(ctx))
	if err != nil {
		return "", err
	}

	contentType := header.Get("Content-Type")
	if contentType != "" {
		return contentType, nil
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
	_, err = obj.Read(buffer)
	if err != nil && err != io.EOF {
		return "", err
	}

	return http.DetectContentType(buffer), nil
}

func (driver *ossFs) SetMetadata(ctx context.Context, path string, metadata map[string]any, opts ...fs.Option) error {
	options := []oss.Option{
		oss.WithContext(ctx),
	}
	for k, v := range metadata {
		options = append(options, oss.Meta(k, fmt.Sprintf("%v", v)))
	}

	// OSS中需要通过复制对象来更新元数据
	_, err := driver.bucket.CopyObject(driver.path(path), driver.path(path+"_tmp"), options...)
	if err != nil {
		return err
	}

	return driver.Move(ctx, path+"_tmp", path)
}

func (driver *ossFs) GetMetadata(ctx context.Context, path string, opts ...fs.Option) (map[string]any, error) {
	path = driver.path(path)
	header, err := driver.bucket.GetObjectMeta(path, oss.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	metadata := make(map[string]interface{})
	for k, v := range header {
		if strings.HasPrefix(k, "X-Oss-Meta-") {
			key := strings.TrimPrefix(k, "X-Oss-Meta-")
			metadata[key] = v[0]
		}
	}
	return metadata, nil
}

func (driver *ossFs) Exists(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	// 判断文件是否存在
	if ok, err := driver.IsFile(ctx, path); err == nil && ok {
		return true, nil
	}

	// 如果不是文件，检查是否为目录
	return driver.IsDir(ctx, path)
}

func (driver *ossFs) IsDir(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	path = driver.path(path)
	prefix := strings.TrimRight(path, "/") + "/"
	lsRes, err := driver.bucket.ListObjects(oss.Prefix(prefix), oss.MaxKeys(1), oss.WithContext(ctx))
	if err != nil {
		return false, err
	}
	return len(lsRes.Objects) > 0 || len(lsRes.CommonPrefixes) > 0, nil
}

func (driver *ossFs) IsFile(ctx context.Context, path string, opts ...fs.Option) (bool, error) {
	path = driver.path(path)
	exist, err := driver.bucket.IsObjectExist(path, oss.WithContext(ctx))
	if err != nil {
		return false, err
	}
	return exist, nil
}

func (driver *ossFs) path(path string) string {
	if driver.config.SubPath != "" {
		return strings.Trim(driver.config.SubPath, "/") + "/" + path
	}
	return path
}
