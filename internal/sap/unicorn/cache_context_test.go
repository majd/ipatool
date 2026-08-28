package unicorn

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestEnsureLibraryHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	selected := artifact{
		url:           "https://example.invalid/unicorn",
		archiveSHA256: "unused",
		librarySHA256: "unused",
		member:        "unused",
		filename:      "libunicorn.test",
	}

	_, err := ensureLibrary(ctx, t.TempDir(), selected, http.DefaultClient)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ensureLibrary error = %v, want %v", err, context.Canceled)
	}
}
