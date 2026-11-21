package s3

import (
	"net/http"
	"testing"
)

func TestCustomTransportRedirectCredentials(t *testing.T) {
	backend, err := New(Config{Region: "us-east-1", Transport: http.DefaultTransport})
	if err != nil {
		t.Fatal(err)
	}
	client := backend.(*s3Fs).client.Options().HTTPClient.(*http.Client)
	for _, tc := range []struct {
		name, target, want string
	}{
		{"same host", "https://s3.example.com/redirected", "secret"},
		{"different host", "https://other.example.com/redirected", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous, _ := http.NewRequest(http.MethodPost, "https://s3.example.com/file", nil)
			redirected, _ := http.NewRequest(http.MethodPost, tc.target, nil)
			redirected.Header.Set("X-Amz-Security-Token", "secret")
			redirected.Response = &http.Response{StatusCode: http.StatusTemporaryRedirect}
			if err := client.CheckRedirect(redirected, []*http.Request{previous}); err != nil {
				t.Fatal(err)
			}
			if got := redirected.Header.Get("X-Amz-Security-Token"); got != tc.want {
				t.Fatalf("security token = %q, want %q", got, tc.want)
			}
		})
	}
}
