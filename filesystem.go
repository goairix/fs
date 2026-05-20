package fs

import (
	"context"
	"io"
	"os"
)

// FileSystem 文件系统接口
type FileSystem interface {
	// List 列出目录内容
	List(ctx context.Context, path string, opts ...Option) ([]FileInfo, error)
	// MakeDir 创建目录
	MakeDir(ctx context.Context, path string, perm os.FileMode, opts ...Option) error
	// RemoveDir 删除目录
	RemoveDir(ctx context.Context, path string, opts ...Option) error
	// CopyDir 复制目录
	CopyDir(ctx context.Context, src, dst string, opts ...Option) error
	// MoveDir 移动目录
	MoveDir(ctx context.Context, src, dst string, opts ...Option) error
	// RenameDir 重命名目录
	RenameDir(ctx context.Context, oldPath, newPath string, opts ...Option) error

	// Create 创建文件并返回io.WriteCloser
	Create(ctx context.Context, path string, opts ...Option) (io.WriteCloser, error)
	// Open 打开文件并返回io.ReadCloser
	Open(ctx context.Context, path string, opts ...Option) (io.ReadCloser, error)
	// OpenFile 以指定模式打开文件
	OpenFile(ctx context.Context, path string, flag int, perm os.FileMode, opts ...Option) (io.ReadWriteCloser, error)
	// Remove 删除文件
	Remove(ctx context.Context, path string, opts ...Option) error
	// Copy 复制文件
	Copy(ctx context.Context, src, dst string, opts ...Option) error
	// Move 移动文件
	Move(ctx context.Context, src, dst string, opts ...Option) error
	// Rename 重命名文件或目录
	Rename(ctx context.Context, oldPath, newPath string, opts ...Option) error

	// Stat 获取文件/目录信息
	Stat(ctx context.Context, path string, opts ...Option) (FileInfo, error)
	// GetMimeType 获取文件的 MIME 类型
	GetMimeType(ctx context.Context, path string, opts ...Option) (string, error)
	// SetMetadata 设置元数据
	SetMetadata(ctx context.Context, path string, metadata map[string]interface{}, opts ...Option) error
	// GetMetadata 获取元数据
	GetMetadata(ctx context.Context, path string, opts ...Option) (map[string]interface{}, error)

	// Exists 判断文件或目录是否存在
	Exists(ctx context.Context, path string, opts ...Option) (bool, error)
	// IsDir 判断是否为目录
	IsDir(ctx context.Context, path string, opts ...Option) (bool, error)
	// IsFile 判断是否为文件
	IsFile(ctx context.Context, path string, opts ...Option) (bool, error)

	// SignFullUrl 带签名的全路径文件访问url
	SignFullUrl(ctx context.Context, path string, opts ...Option) (string, error)
	// FullUrl 全路径文件访问url
	FullUrl(ctx context.Context, path string, opts ...Option) (string, error)
	// RelativePath 还原文件路径
	RelativePath(ctx context.Context, fullUrl string, opts ...Option) (string, error)

	// Uploader 文件上传
	Uploader() Uploader
}

// Uploader 文件上传器
type Uploader interface {
	// Upload 文件上传
	Upload(ctx context.Context, path string, reader io.Reader, opts ...Option) error
	// InitMultipartUpload 初始化分片上传
	InitMultipartUpload(ctx context.Context, path string, opts ...Option) (string, error)
	// UploadPart 上传分片
	UploadPart(ctx context.Context, path string, uploadID string, partNumber int, data io.Reader, opts ...Option) (string, error)
	// CompleteMultipartUpload 完成分片上传
	CompleteMultipartUpload(ctx context.Context, path string, uploadID string, parts []MultipartPart, opts ...Option) error
	// AbortMultipartUpload 取消分片上传
	AbortMultipartUpload(ctx context.Context, path string, uploadID string, opts ...Option) error
	// ListMultipartUploads 列出所有未完成的分片上传
	ListMultipartUploads(ctx context.Context, opts ...Option) ([]MultipartUploadInfo, error)
	// ListUploadedParts 列出已上传的分片
	ListUploadedParts(ctx context.Context, path string, uploadID string, opts ...Option) ([]MultipartPart, error)
}
