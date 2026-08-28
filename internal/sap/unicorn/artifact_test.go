package unicorn

import (
	"strings"
	"testing"
)

func TestArtifactForSupportedPlatforms(t *testing.T) {
	tests := []struct {
		goos     string
		goarch   string
		filename string
	}{
		{goos: "darwin", goarch: "amd64", filename: "libunicorn.2.dylib"},
		{goos: "darwin", goarch: "arm64", filename: "libunicorn.2.dylib"},
		{goos: "linux", goarch: "amd64", filename: "libunicorn.so.2"},
		{goos: "linux", goarch: "arm64", filename: "libunicorn.so.2"},
		{goos: "linux-musl", goarch: "amd64", filename: "libunicorn.so.2"},
		{goos: "linux-musl", goarch: "arm64", filename: "libunicorn.so.2"},
		{goos: "windows", goarch: "amd64", filename: "libunicorn.dll"},
		{goos: "windows", goarch: "arm64", filename: "libunicorn.dll"},
	}

	for _, test := range tests {
		t.Run(test.goos+"/"+test.goarch, func(t *testing.T) {
			selected, err := artifactFor(test.goos, test.goarch)
			if err != nil {
				t.Fatal(err)
			}

			if selected.filename != test.filename {
				t.Fatalf("filename = %q, want %q", selected.filename, test.filename)
			}

			artifacts := append([]artifact{selected}, selected.dependencies...)
			for _, item := range artifacts {
				if !strings.HasPrefix(item.url, "https://") {
					t.Fatalf("artifact URL is not HTTPS: %q", item.url)
				}

				if len(item.archiveSHA256) != 64 || len(item.librarySHA256) != 64 {
					t.Fatal("artifact is missing a SHA-256 checksum")
				}
			}
		})
	}
}

func TestArtifactForWindowsARM64IncludesRuntimeDependency(t *testing.T) {
	selected, err := artifactFor("windows", "arm64")
	if err != nil {
		t.Fatal(err)
	}

	if selected.format != archiveTarZstd {
		t.Fatalf("archive format = %d, want %d", selected.format, archiveTarZstd)
	}

	if len(selected.dependencies) != 1 {
		t.Fatalf("dependencies = %d, want 1", len(selected.dependencies))
	}

	dependency := selected.dependencies[0]
	if dependency.filename != "libwinpthread-1.dll" || dependency.format != archiveTarZstd {
		t.Fatalf("unexpected dependency: %+v", dependency)
	}
}
