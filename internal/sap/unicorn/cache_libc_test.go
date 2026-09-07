//go:build darwin || linux

package unicorn

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInterpreterUsesMusl(t *testing.T) {
	cases := map[string]bool{
		"/lib/ld-musl-x86_64.so.1":      true,
		"/lib/ld-musl-aarch64.so.1":     true,
		"/lib64/ld-linux-x86-64.so.2":   false,
		"/lib/ld-linux-aarch64.so.1":    false,
		"/usr/lib/ld-linux-x86-64.so.2": false,
		"":                              false,
	}

	for interpreter, expected := range cases {
		if actual := interpreterUsesMusl(interpreter); actual != expected {
			t.Errorf("interpreterUsesMusl(%q) = %t, want %t", interpreter, actual, expected)
		}
	}
}

// A glibc host may carry a musl loader for cross compilation, in which case the host probe and the
// process interpreter disagree. The interpreter is the one that describes this process.
func TestLinuxUsesMuslPrefersProcessInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PT_INTERP of /proc/self/exe is only available on Linux")
	}

	interpreter, ok := selfInterpreter()
	if !ok {
		t.Skip("statically linked test binary has no PT_INTERP")
	}

	if !filepath.IsAbs(interpreter) {
		t.Fatalf("interpreter = %q, want an absolute path", interpreter)
	}

	if _, err := os.Stat(interpreter); err != nil {
		t.Fatalf("interpreter %q is not present on the host: %v", interpreter, err)
	}

	if actual, expected := linuxUsesMusl(), interpreterUsesMusl(interpreter); actual != expected {
		t.Fatalf("linuxUsesMusl() = %t, want %t for interpreter %q", actual, expected, interpreter)
	}
}
