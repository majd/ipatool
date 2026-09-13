package appstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	apphttp "github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	"github.com/schollz/progressbar/v3"
)

func TestDownloadFileResumesWithAndWithoutProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=7-" {
			t.Errorf("range = %q", got)
		}

		w.Header().Set("Content-Range", "bytes 7-14/15")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "download")
	}))
	defer server.Close()

	for _, interactive := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-interactive", true: "interactive"}[interactive], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.tmp")
			if err := os.WriteFile(path, []byte("partial"), 0600); err != nil {
				t.Fatal(err)
			}

			store := &appstore{httpClient: apphttp.NewClient[interface{}](apphttp.Args{}), os: operatingsystem.New()}

			var progress *progressbar.ProgressBar
			if interactive {
				progress = progressbar.NewOptions64(15, progressbar.OptionSetWriter(io.Discard))
			}

			if err := store.downloadFile(context.Background(), server.URL, path, progress); err != nil {
				t.Fatal(err)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if string(data) != "partialdownload" {
				t.Fatalf("download = %q", data)
			}
		})
	}
}

func TestDownloadFileValidatesResumeResponses(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		rangeHeader string
		body        string
		want        string
		wantError   bool
		chunked     bool
	}{
		{name: "range ignored", status: http.StatusOK, body: "new", want: "new"},
		{name: "already complete", status: http.StatusRequestedRangeNotSatisfiable, rangeHeader: "bytes */7", body: "error page", want: "partial"},
		{name: "stale oversized partial", status: http.StatusRequestedRangeNotSatisfiable, rangeHeader: "bytes */3", body: "error page", want: "partial", wantError: true},
		{name: "incomplete rejected partial", status: http.StatusRequestedRangeNotSatisfiable, rangeHeader: "bytes */15", want: "partial", wantError: true},
		{name: "malformed unsatisfied range", status: http.StatusRequestedRangeNotSatisfiable, rangeHeader: "7", want: "partial", wantError: true},
		{name: "http error", status: http.StatusForbidden, body: "error page", want: "partial", wantError: true},
		{name: "wrong offset", status: http.StatusPartialContent, rangeHeader: "bytes 0-7/15", body: "download", want: "partial", wantError: true},
		{name: "missing range", status: http.StatusPartialContent, body: "download", want: "partial", wantError: true},
		{name: "malformed range", status: http.StatusPartialContent, rangeHeader: "bytes 7-x/15", body: "download", want: "partial", wantError: true},
		{name: "invalid total", status: http.StatusPartialContent, rangeHeader: "bytes 7-14/14", body: "download", want: "partial", wantError: true},
		{name: "length mismatch", status: http.StatusPartialContent, rangeHeader: "bytes 7-14/15", body: "bad", want: "partial", wantError: true},
		{name: "truncated chunked response", status: http.StatusPartialContent, rangeHeader: "bytes 7-14/15", body: "dow", want: "partialdow", wantError: true, chunked: true},
	} {
		for _, interactive := range []bool{false, true} {
			t.Run(test.name+map[bool]string{false: "/non-interactive", true: "/interactive"}[interactive], func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "app.tmp")
				if err := os.WriteFile(path, []byte("partial"), 0600); err != nil {
					t.Fatal(err)
				}

				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Range") != "bytes=7-" {
						t.Errorf("range = %q", r.Header.Get("Range"))
					}

					if test.rangeHeader != "" {
						w.Header().Set("Content-Range", test.rangeHeader)
					}

					w.WriteHeader(test.status)

					if test.chunked {
						w.(http.Flusher).Flush()
					}

					_, _ = io.WriteString(w, test.body)
				}))
				defer server.Close()

				store := &appstore{httpClient: apphttp.NewClient[interface{}](apphttp.Args{}), os: operatingsystem.New()}

				var progress *progressbar.ProgressBar
				if interactive {
					progress = progressbar.NewOptions64(1, progressbar.OptionSetWriter(io.Discard))
				}

				err := store.downloadFile(context.Background(), server.URL, path, progress)
				if (err != nil) != test.wantError {
					t.Fatalf("download error = %v", err)
				}

				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}

				if string(data) != test.want {
					t.Fatalf("file = %q, want %q", data, test.want)
				}
			})
		}
	}
}
