package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goairix/fs"
	"github.com/google/uuid"
)

type MultipartUpload struct {
	Path       string         `json:"path"`
	UploadID   string         `json:"upload_id"`
	Parts      map[int]string `json:"parts"` // partNumber -> tempFilePath
	CreateTime string         `json:"create_time"`
}

func (driver *localFs) Uploader() fs.Uploader {
	return driver
}

func (driver *localFs) Upload(ctx context.Context, path string, reader io.Reader, opts ...fs.Option) error {
	options := &fs.Options{}
	for _, opt := range opts {
		opt(options)
	}

	// 创建目标目录
	basePath := filepath.Dir(path)
	if ok, _ := driver.Exists(ctx, basePath); !ok {
		if err := os.MkdirAll(driver.fullPath(basePath), 0755); err != nil {
			return err
		}
	}

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

func (driver *localFs) InitMultipartUpload(ctx context.Context, path string, opts ...fs.Option) (string, error) {
	o := &fs.Options{}
	for _, opt := range opts {
		opt(o)
	}
	uploadID := uuid.New().String()
	upload := &MultipartUpload{
		Path:     path,
		UploadID: uploadID,
		Parts:    make(map[int]string),
	}
	if err := driver.multipartStorage.Save(upload); err != nil {
		return "", err
	}
	return uploadID, nil
}

func (driver *localFs) UploadPart(ctx context.Context, path string, uploadID string, partNumber int, data io.Reader, opts ...fs.Option) (string, error) {
	upload, err := driver.multipartStorage.Get(uploadID)
	if err != nil {
		return "", err
	}

	// 创建临时文件存储分片
	tempFile, err := os.CreateTemp("", fmt.Sprintf("part-%d-*", partNumber))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tempFile.Close()
	}()

	// 写入分片数据
	if _, err := io.Copy(tempFile, data); err != nil {
		_ = os.Remove(tempFile.Name())
		return "", err
	}

	upload.Parts[partNumber] = tempFile.Name()
	upload.CreateTime = time.Now().Format(time.RFC3339)
	if err := driver.multipartStorage.Save(upload); err != nil {
		_ = os.Remove(tempFile.Name())
		return "", err
	}
	return tempFile.Name(), nil
}

func (driver *localFs) CompleteMultipartUpload(ctx context.Context, path string, uploadID string, parts []fs.MultipartPart, opts ...fs.Option) error {
	upload, err := driver.multipartStorage.Get(uploadID)
	if err != nil {
		return err
	}
	defer func() {
		_ = driver.multipartStorage.Delete(uploadID)
	}()

	// 创建目标目录
	basePath := filepath.Dir(path)
	if ok, _ := driver.Exists(ctx, basePath); !ok {
		if err := os.MkdirAll(driver.fullPath(basePath), 0755); err != nil {
			return err
		}
	}

	destFile, err := os.Create(driver.fullPath(path))
	if err != nil {
		return err
	}
	defer func() {
		_ = destFile.Close()
	}()

	// 按顺序合并分片
	for _, part := range parts {
		tempPath, ok := upload.Parts[part.PartNumber]
		if !ok {
			return fmt.Errorf("part %d not found", part.PartNumber)
		}

		// 读取分片数据
		tempFile, err := os.Open(tempPath)
		if err != nil {
			return err
		}

		// 写入目标文件
		if _, err := io.Copy(destFile, tempFile); err != nil {
			_ = tempFile.Close()
			return err
		}
		_ = tempFile.Close()

		// 删除临时文件
		_ = os.Remove(tempPath)
	}

	return nil
}

func (driver *localFs) AbortMultipartUpload(ctx context.Context, path string, uploadID string, opts ...fs.Option) error {
	upload, err := driver.multipartStorage.Get(uploadID)
	if err != nil {
		return nil
	}

	// 删除所有临时文件
	for _, tempPath := range upload.Parts {
		_ = os.Remove(tempPath)
	}

	return driver.multipartStorage.Delete(uploadID)
}

func (driver *localFs) ListMultipartUploads(ctx context.Context, opts ...fs.Option) ([]fs.MultipartUploadInfo, error) {
	var options fs.Options
	for _, opt := range opts {
		opt(&options)
	}
	uploads, err := driver.multipartStorage.List()
	if err != nil {
		return nil, err
	}

	result := make([]fs.MultipartUploadInfo, 0, len(uploads))
	for _, upload := range uploads {
		if !strings.HasPrefix(upload.Path, options.MultipartPrefix) {
			continue
		}
		createTime, _ := time.Parse(time.RFC3339, upload.CreateTime)
		result = append(result, fs.MultipartUploadInfo{
			UploadID:   upload.UploadID,
			Path:       upload.Path,
			CreateTime: createTime,
		})
	}
	return result, nil
}

func (driver *localFs) ListUploadedParts(ctx context.Context, path string, uploadID string, opts ...fs.Option) ([]fs.MultipartPart, error) {
	upload, err := driver.multipartStorage.Get(uploadID)
	if err != nil {
		return nil, err
	}

	parts := make([]fs.MultipartPart, len(upload.Parts))
	for i, partPath := range upload.Parts {
		info, err := os.Stat(partPath)
		if err != nil {
			continue
		}
		parts[i] = fs.MultipartPart{
			PartNumber: i + 1,
			Size:       info.Size(),
		}
	}
	return parts, nil
}
