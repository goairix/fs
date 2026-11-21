package fs_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/goairix/fs"
	"github.com/goairix/fs/driver/alioss"
	"github.com/goairix/fs/driver/hwobs"
	"github.com/goairix/fs/driver/local"
	"github.com/goairix/fs/driver/minio"
	"github.com/goairix/fs/driver/s3"
	"github.com/goairix/fs/driver/txcos"
)

type multipartTransport func(*http.Request) (*http.Response, error)

func (f multipartTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

var cloudDrivers = []struct {
	name string
	new  func(string, string, http.RoundTripper) (fs.FileSystem, error)
}{
	{"alioss", func(endpoint, sub string, transport http.RoundTripper) (fs.FileSystem, error) {
		return alioss.New(alioss.Config{Endpoint: endpoint, BucketName: "test-bucket", AccessKeyID: "test", SecretAccessKey: "secret", SubPath: sub, Transport: transport})
	}},
	{"hwobs", func(endpoint, sub string, transport http.RoundTripper) (fs.FileSystem, error) {
		return hwobs.New(hwobs.Config{Endpoint: endpoint, BucketName: "test-bucket", AccessKeyID: "test", SecretAccessKey: "secret", SubPath: sub, Transport: transport})
	}},
	{"s3", func(endpoint, sub string, transport http.RoundTripper) (fs.FileSystem, error) {
		return s3.New(s3.Config{Endpoint: endpoint, Region: "us-east-1", BucketName: "test-bucket", AccessKeyID: "test", SecretAccessKey: "secret", SubPath: sub, Transport: transport})
	}},
	{"txcos", func(endpoint, sub string, transport http.RoundTripper) (fs.FileSystem, error) {
		return txcos.New(txcos.Config{BucketURL: endpoint, SecretID: "test", SecretKey: "secret", SubPath: sub, Transport: transport})
	}},
	{"minio", func(endpoint, sub string, transport http.RoundTripper) (fs.FileSystem, error) {
		return minio.New(minio.Config{Endpoint: strings.TrimPrefix(endpoint, "http://"), BucketName: "test-bucket", AccessKeyID: "test", SecretAccessKey: "secret", SubPath: sub, Transport: transport})
	}},
}

func multipartResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}

func TestCloudMultipartUploads(t *testing.T) {
	for _, driver := range cloudDrivers {
		t.Run(driver.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, sub, prefix string
				opts              []fs.Option
				failSecondPage    bool
				emptyResult       bool
			}{
				{name: "legacy", sub: "tenant"},
				{name: "empty", sub: "tenant", opts: []fs.Option{fs.WithMultipartPrefix("")}},
				{name: "prefix", prefix: "reports/", opts: []fs.Option{fs.WithMultipartPrefix("reports/")}},
				{name: "scoped", sub: "/tenant/", prefix: "tenant/reports/", opts: []fs.Option{fs.WithMultipartPrefix("reports/")}},
				{name: "last option wins", prefix: "reports/", opts: []fs.Option{fs.WithMultipartPrefix("old/"), fs.WithMultipartPrefix("reports/")}},
				{name: "no matches", prefix: "missing/", opts: []fs.Option{fs.WithMultipartPrefix("missing/")}, emptyResult: true},
				{name: "unicode and spaces", prefix: "报表 +/", opts: []fs.Option{fs.WithMultipartPrefix("报表 +/")}},
				{name: "reset prefix", sub: "tenant", opts: []fs.Option{fs.WithMultipartPrefix("old/"), fs.WithMultipartPrefix("")}},
				{name: "second page error", prefix: "reports/", opts: []fs.Option{fs.WithMultipartPrefix("reports/")}, failSecondPage: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					pages := 0
					transport := multipartTransport(func(req *http.Request) (*http.Response, error) {
						query := req.URL.Query()
						if query.Has("location") {
							return multipartResponse(req, 200, "<LocationConstraint>us-east-1</LocationConstraint>"), nil
						}
						if req.Method == http.MethodHead {
							return multipartResponse(req, 200, ""), nil
						}
						if req.Method != http.MethodGet || !query.Has("uploads") {
							t.Errorf("unexpected request: %s %s", req.Method, req.URL)
							return multipartResponse(req, 400, "<Error><Code>InvalidRequest</Code></Error>"), nil
						}
						pages++
						if got := query.Get("prefix"); got != tc.prefix {
							t.Errorf("prefix = %q, want %q", got, tc.prefix)
						}
						if req.Header.Get("Authorization") == "" {
							t.Error("request is missing authorization")
						}
						if tc.emptyResult {
							return multipartResponse(req, 200, "<ListMultipartUploadsResult><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>"), nil
						}
						if pages > 2 {
							return multipartResponse(req, 400, "<Error><Code>InvalidRequest</Code></Error>"), nil
						}
						if pages == 2 {
							if query.Get("key-marker") != tc.prefix+"a" || query.Get("upload-id-marker") != "upload-1" {
								t.Errorf("incorrect pagination markers: %s", req.URL)
							}
							if tc.failSecondPage {
								return multipartResponse(req, 403, "<Error><Code>AccessDenied</Code><Message>denied</Message></Error>"), nil
							}
						} else if query.Get("key-marker") != "" || query.Get("upload-id-marker") != "" {
							t.Error("first page has pagination markers")
						}
						responsePrefix, encoding := tc.prefix, ""
						if query.Get("encoding-type") == "url" {
							responsePrefix = url.QueryEscape(tc.prefix)
							encoding = "<EncodingType>url</EncodingType>"
						}
						body := fmt.Sprintf("<ListMultipartUploadsResult>%s<Bucket>test-bucket</Bucket><IsTruncated>%t</IsTruncated><NextKeyMarker>%sa</NextKeyMarker><NextUploadIdMarker>upload-1</NextUploadIdMarker><Upload><Key>%s%s</Key><UploadId>upload-%d</UploadId><Initiated>2026-11-21T00:00:00Z</Initiated></Upload></ListMultipartUploadsResult>", encoding, pages == 1, responsePrefix, responsePrefix, []string{"a", "b"}[pages-1], pages)
						return multipartResponse(req, 200, body), nil
					})
					backend, err := driver.new("http://127.0.0.1:9000", tc.sub, transport)
					if err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					uploads, err := backend.Uploader().ListMultipartUploads(ctx, tc.opts...)
					if tc.failSecondPage {
						if err == nil || uploads != nil || pages != 2 {
							t.Fatalf("second page error returned uploads=%v, err=%v", uploads, err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if tc.emptyResult {
						if pages != 1 || len(uploads) != 0 {
							t.Fatalf("pages=%d, uploads=%v, want one empty page", pages, uploads)
						}
						return
					}
					if pages != 2 || len(uploads) != 2 {
						t.Fatalf("pages=%d, uploads=%v, want two pages and uploads", pages, uploads)
					}
					for i, upload := range uploads {
						if upload.Path != tc.prefix+[]string{"a", "b"}[i] || upload.UploadID != fmt.Sprintf("upload-%d", i+1) || !upload.CreateTime.Equal(time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)) {
							t.Errorf("unexpected upload: %+v", upload)
						}
					}
				})
			}
		})
	}
}

func TestLocalMultipartUploadsPrefix(t *testing.T) {
	backend, err := local.New(local.Config{RootPath: t.TempDir(), SubPath: "/tenant/"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uploader := backend.Uploader()
	for _, path := range []string{"reports/a", "reports/b", "reports-old/c", "other/d"} {
		if _, err := uploader.InitMultipartUpload(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		opts []fs.Option
		want int
	}{
		{"legacy", nil, 4},
		{"empty", []fs.Option{fs.WithMultipartPrefix("")}, 4},
		{"prefix", []fs.Option{fs.WithMultipartPrefix("reports/")}, 2},
		{"no matches", []fs.Option{fs.WithMultipartPrefix("missing/")}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uploads, err := uploader.ListMultipartUploads(ctx, tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if len(uploads) != tc.want {
				t.Fatalf("uploads=%v, want %d", uploads, tc.want)
			}
			if tc.name == "prefix" {
				for _, upload := range uploads {
					if upload.Path != "reports/a" && upload.Path != "reports/b" {
						t.Errorf("unexpected path: %q", upload.Path)
					}
				}
			}
		})
	}
}

func TestCloudMultipartUploadsDefaultTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case req.URL.Query().Has("location"):
			_, _ = io.WriteString(w, "<LocationConstraint>us-east-1</LocationConstraint>")
		case req.Method == http.MethodHead:
		case req.Method == http.MethodGet && req.URL.Query().Has("uploads"):
			if req.URL.Query().Get("prefix") != "tenant/reports/" {
				t.Errorf("unexpected prefix: %s", req.URL)
			}
			_, _ = io.WriteString(w, "<ListMultipartUploadsResult><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>")
		default:
			t.Errorf("unexpected request: %s %s", req.Method, req.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	for _, driver := range cloudDrivers {
		t.Run(driver.name, func(t *testing.T) {
			backend, err := driver.new(server.URL, "tenant", nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			uploads, err := backend.Uploader().ListMultipartUploads(ctx, fs.WithMultipartPrefix("reports/"))
			if err != nil || len(uploads) != 0 {
				t.Fatalf("uploads=%v, err=%v, want empty results", uploads, err)
			}
		})
	}
}

func TestS3CustomTransportRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			backend, err := s3.New(s3.Config{
				Endpoint: "https://s3.example.com", Region: "us-east-1", BucketName: "test-bucket",
				AccessKeyID: "test", SecretAccessKey: "secret",
				Transport: multipartTransport(func(req *http.Request) (*http.Response, error) {
					requests++
					if req.Method != http.MethodPost {
						t.Errorf("redirect changed method to %s", req.Method)
					}
					if requests == 1 {
						resp := multipartResponse(req, status, "")
						resp.Header.Set("Location", "https://redirect.example.net/redirected")
						return resp, nil
					}
					return multipartResponse(req, 200, "<InitiateMultipartUploadResult><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>"), nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			id, err := backend.Uploader().InitMultipartUpload(context.Background(), "file")
			if status == 307 || status == 308 {
				if err != nil || id != "upload-1" || requests != 2 {
					t.Fatalf("id=%q, err=%v, requests=%d", id, err, requests)
				}
			} else if err == nil || id != "" || requests != 1 {
				t.Fatalf("unsafe redirect: id=%q, err=%v, requests=%d", id, err, requests)
			}
		})
	}
}
