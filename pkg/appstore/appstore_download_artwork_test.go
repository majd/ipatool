package appstore

import (
	"archive/zip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	apphttp "github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
)

func TestDownloadArtworkAndIncludeInIPA(t *testing.T) {
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = io.WriteString(w, "artwork bytes")
	}))
	defer server.Close()

	store := &appstore{httpClient: apphttp.NewClient[interface{}](apphttp.Args{}), os: operatingsystem.New()}
	for _, url := range []string{"", server.URL} {
		data, err := store.downloadArtwork(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}

		if url == "" && (len(data) != 0 || requests != 0) {
			t.Fatal("artwork was downloaded without a URL")
		}

		if url != "" && string(data) != "artwork bytes" {
			t.Fatal("artwork missing")
		}

		path := filepath.Join(t.TempDir(), "source.zip")

		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}

		writer := zip.NewWriter(file)
		if _, err := writer.Create("Payload/App.app/"); err != nil {
			t.Fatal(err)
		}

		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		if err := file.Close(); err != nil {
			t.Fatal(err)
		}

		if err := store.applyPatches(downloadItemResult{Metadata: map[string]interface{}{}}, Account{}, path, path+".ipa", data); err != nil {
			t.Fatal(err)
		}

		reader, err := zip.OpenReader(path + ".ipa")
		if err != nil {
			t.Fatal(err)
		}

		found := false

		for _, entry := range reader.File {
			if entry.Name != "iTunesArtwork" {
				continue
			}

			found = true

			r, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}

			actual, err := io.ReadAll(r)
			r.Close()

			if err != nil || string(actual) != "artwork bytes" {
				t.Fatalf("artwork = %q; %v", actual, err)
			}
		}

		reader.Close()

		if found != (url != "") {
			t.Fatal("incorrect artwork presence")
		}
	}
}

func TestDownloadArtworkRejectsFailedResponse(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	store := &appstore{httpClient: apphttp.NewClient[interface{}](apphttp.Args{})}
	if _, err := store.downloadArtwork(context.Background(), server.URL); err == nil {
		t.Fatal("accepted error page as artwork")
	}

	if data, err := store.downloadArtwork(context.Background(), ""); err != nil || len(data) != 0 {
		t.Fatal("missing artwork URL should be skipped")
	}
}
