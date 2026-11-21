package minio

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/goairix/fs"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestListMultipartUploadsPrefix(t *testing.T) {
	for _, tc := range []struct {
		name, subPath, want string
		opts                []fs.Option
	}{
		{"legacy", "tenant", "", nil},
		{"empty", "tenant", "", []fs.Option{fs.WithMultipartPrefix("")}},
		{"prefix", "", "reports/", []fs.Option{fs.WithMultipartPrefix("reports/")}},
		{"scoped", "/tenant/", "tenant/reports/", []fs.Option{fs.WithMultipartPrefix("reports/")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listed := false
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				query := req.URL.Query()
				var body string
				switch {
				case query.Has("location"):
					body = "<LocationConstraint>us-east-1</LocationConstraint>"
				case req.Method == http.MethodHead:
				case req.Method == http.MethodGet && query.Has("uploads"):
					listed = true
					if got := query.Get("prefix"); got != tc.want {
						t.Errorf("prefix = %q, want %q", got, tc.want)
					}
					body = "<ListMultipartUploadsResult><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>"
				default:
					t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			backend, err := New(Config{Endpoint: "127.0.0.1:9000", BucketName: "test-bucket", SubPath: tc.subPath, Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Uploader().ListMultipartUploads(context.Background(), tc.opts...); err != nil {
				t.Fatal(err)
			}
			if !listed {
				t.Fatal("multipart list did not reach the configured transport")
			}
		})
	}
}
