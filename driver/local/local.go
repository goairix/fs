package local

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/goairix/fs"
)

// localFs 本地文件系统
type localFs struct {
	rootPath         string
	subPath          string
	multipartStorage MultipartStorage
}

type Config struct {
	RootPath         string // 根目录路径
	SubPath          string // 子目录路径
	MultipartStorage MultipartStorage
}

func New(conf Config) (fs.FileSystem, error) {
	if conf.MultipartStorage == nil {
		conf.MultipartStorage, _ = NewFileMultipartStorage(filepath.Join(conf.RootPath, ".multipart"))
	}
	return &localFs{
		rootPath:         conf.RootPath,
		subPath:          conf.SubPath,
		multipartStorage: conf.MultipartStorage,
	}, nil
}

func (driver *localFs) List(_ context.Context, path string, opts ...fs.Option) ([]fs.FileInfo, error) {
	fullPath := driver.fullPath(path)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	var files []fs.FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, info)
	}
	return files, nil
}

func (driver *localFs) MakeDir(_ context.Context, path string, perm os.FileMode, opts ...fs.Option) error {
	return os.MkdirAll(driver.fullPath(path), perm)
}

func (driver *localFs) RemoveDir(_ context.Context, path string, opts ...fs.Option) error {
	return os.RemoveAll(driver.fullPath(path))
}

func (driver *localFs) CopyDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	srcPath := driver.fullPath(src)
	dstPath := driver.fullPath(dst)
	return filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcPath, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstPath, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return driver.Copy(ctx, filepath.Join(src, rel), filepath.Join(dst, rel), opts...)
	})
}

func (driver *localFs) MoveDir(ctx context.Context, src, dst string, opts ...fs.Option) error {
	if err := driver.CopyDir(ctx, src, dst, opts...); err != nil {
		return err
	}
	return os.RemoveAll(driver.fullPath(src))
}

func (driver *localFs) RenameDir(ctx context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return driver.MoveDir(ctx, oldPath, newPath, opts...)
}

func (driver *localFs) Create(ctx context.Context, path string, opts ...fs.Option) (io.WriteCloser, error) {
	options := &fs.Options{}
	for _, opt := range opts {
		opt(options)
	}

	file, err := os.Create(driver.fullPath(path))
	if err != nil {
		return nil, err
	}

	// 本地文件系统不处理 ContentType，只处理 Metadata
	if options.Metadata != nil {
		if err = driver.SetMetadata(ctx, path, options.Metadata); err != nil {
			_ = file.Close()
			return nil, err
		}
	}

	return file, nil
}

func (driver *localFs) Open(_ context.Context, path string, opts ...fs.Option) (io.ReadCloser, error) {
	return os.Open(driver.fullPath(path))
}

func (driver *localFs) OpenFile(_ context.Context, path string, flag int, perm os.FileMode, opts ...fs.Option) (io.ReadWriteCloser, error) {
	return os.OpenFile(driver.fullPath(path), flag, perm)
}

func (driver *localFs) Remove(_ context.Context, path string, opts ...fs.Option) error {
	return os.Remove(driver.fullPath(path))
}

func (driver *localFs) Copy(ctx context.Context, src, dst string, opts ...fs.Option) error {
	sourceFile, err := driver.Open(ctx, src)
	if err != nil {
		return err
	}
	defer func() {
		_ = sourceFile.Close()
	}()

	destFile, err := driver.Create(ctx, dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = destFile.Close()
	}()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func (driver *localFs) Move(_ context.Context, src, dst string, opts ...fs.Option) error {
	return os.Rename(driver.fullPath(src), driver.fullPath(dst))
}

func (driver *localFs) Rename(_ context.Context, oldPath, newPath string, opts ...fs.Option) error {
	return os.Rename(driver.fullPath(oldPath), driver.fullPath(newPath))
}

func (driver *localFs) Stat(_ context.Context, path string, opts ...fs.Option) (fs.FileInfo, error) {
	return os.Stat(driver.fullPath(path))
}

func (driver *localFs) GetMimeType(_ context.Context, path string, opts ...fs.Option) (string, error) {
	file, err := os.Open(driver.fullPath(path))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()

	// 读取文件前512字节用于检测文件类型
	buffer := make([]byte, 512)
	_, err = file.Read(buffer)
	if err != nil && err != io.EOF {
		return "", err
	}

	// 使用 http.DetectContentType 检测 MIME 类型
	return http.DetectContentType(buffer), nil
}

func (driver *localFs) SetMetadata(_ context.Context, path string, metadata map[string]any, opts ...fs.Option) error {
	// 本地文件系统只支持修改文件权限和时间戳
	if mode, ok := metadata["mode"]; ok {
		if m, ok := mode.(os.FileMode); ok {
			if err := os.Chmod(driver.fullPath(path), m); err != nil {
				return err
			}
		}
	}
	return nil
}

func (driver *localFs) GetMetadata(_ context.Context, path string, opts ...fs.Option) (map[string]any, error) {
	info, err := os.Stat(driver.fullPath(path))
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"name":        info.Name(),
		"size":        info.Size(),
		"mode":        info.Mode(),
		"modify_time": info.ModTime(),
		"is_dir":      info.IsDir(),
	}, nil
}

func (driver *localFs) Exists(_ context.Context, path string, opts ...fs.Option) (bool, error) {
	_, err := os.Stat(driver.fullPath(path))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (driver *localFs) IsDir(_ context.Context, path string, opts ...fs.Option) (bool, error) {
	info, err := os.Stat(driver.fullPath(path))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

func (driver *localFs) IsFile(_ context.Context, path string, opts ...fs.Option) (bool, error) {
	info, err := os.Stat(driver.fullPath(path))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

// fullPath 获取完整路径
func (driver *localFs) fullPath(path string, opts ...fs.Option) string {
	return filepath.Join(driver.rootPath, driver.path(path))
}

func (driver *localFs) path(path string) string {
	if driver.subPath != "" {
		return strings.Trim(driver.subPath, "/") + "/" + path
	}
	return path
}
