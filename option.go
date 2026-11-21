package fs

import "time"

// AccessMode 访问模式
type AccessMode uint8

const (
	Private         AccessMode = iota // 私有读写
	PublicRead                        // 公共读
	PublicReadWrite                   // 公共读写
)

type Option func(*Options)

type Options struct {
	Metadata           Metadata
	ContentType        string
	ContentDisposition string
	CdnDomain          string
	SignUrlExpires     time.Duration
	MultipartPrefix    string
}

// WithMultipartPrefix 为未完成上传查询设置相对于驱动 SubPath 的路径前缀。
// 空前缀沿用原查询范围。返回的对象名仍需由调用方按需精确匹配。
func WithMultipartPrefix(prefix string) Option {
	return func(o *Options) {
		o.MultipartPrefix = prefix
	}
}

// WithMetadata 设置元数据
func WithMetadata(metadata Metadata) Option {
	return func(o *Options) {
		o.Metadata = metadata
	}
}

// WithContentType 设置文件类型
func WithContentType(contentType string) Option {
	return func(o *Options) {
		o.ContentType = contentType
	}
}

// WithContentDisposition 设置文件的 Content-Disposition
func WithContentDisposition(contentDisposition string) Option {
	return func(o *Options) {
		o.ContentDisposition = contentDisposition
	}
}

// WithCdnDomain 设置cdn域名
func WithCdnDomain(cdnDomain string) Option {
	return func(o *Options) {
		o.CdnDomain = cdnDomain
	}
}

func WithSignUrlExpires(expires time.Duration) Option {
	return func(o *Options) {
		o.SignUrlExpires = expires
	}
}
