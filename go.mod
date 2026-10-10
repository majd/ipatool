module github.com/majd/ipatool/v2

go 1.26.5

// go-apfs requires this package path; keep decoding pure Go for cross-platform builds.
replace github.com/blacktop/lzfse-cgo => ./internal/lzfsecompat

require (
	github.com/avast/retry-go v3.0.0+incompatible
	github.com/blacktop/go-apfs v1.0.28
	github.com/blacktop/go-macho v1.1.282
	github.com/bodgit/sevenzip v1.6.3
	github.com/byteness/go-keychain v0.0.0-20191008050251-8e49817e8af4
	github.com/byteness/keyring v1.9.0
	github.com/deploymenttheory/go-macos-pkg v0.3.0
	github.com/ebitengine/purego v0.10.2
	github.com/go-compressions/lzfse v0.3.0
	github.com/juju/persistent-cookiejar v1.0.0
	github.com/klauspost/compress v1.18.6
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/onsi/ginkgo/v2 v2.5.0
	github.com/onsi/gomega v1.24.0
	github.com/rs/zerolog v1.28.0
	github.com/schollz/progressbar/v3 v3.19.1
	github.com/spf13/cobra v1.10.2
	github.com/thediveo/enumflag/v2 v2.0.1
	go.uber.org/mock v0.4.0
	golang.org/x/sys v0.48.0
	golang.org/x/term v0.46.0
	howett.net/plist v1.0.1
)

require (
	github.com/1Password/connect-sdk-go v1.5.4-0.20250417152128-c154b387248b // indirect
	github.com/1password/onepassword-sdk-go v0.4.1-beta.1 // indirect
	github.com/VividCortex/ewma v1.2.0 // indirect
	github.com/acarl005/stripansi v0.0.0-20180116102854-5a71ef0e047d // indirect
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/apex/log v1.9.0 // indirect
	github.com/blacktop/go-dwarf v1.0.14 // indirect
	github.com/blacktop/go-plist v1.0.2 // indirect
	github.com/blacktop/lzfse-cgo v1.2.0 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/byteness/go-libsecret v0.0.0-20260108215642-107379d3dee0 // indirect
	github.com/byteness/percent v0.2.2 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/dvsekhvalnov/jose2go v1.8.0 // indirect
	github.com/dylibso/observe-sdk/go v0.0.0-20240828172851-9145d8ad07e1 // indirect
	github.com/extism/go-sdk v1.7.1 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/frankban/quicktest v1.14.6 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/ianlancetaylor/demangle v0.0.0-20251118225945-96ee0021ea0f // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/juju/go4 v0.0.0-20160222163258-40d72ab9641a // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/noamcohen97/touchid-go v0.3.0 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.29 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stangelandcl/ppmd v0.1.0 // indirect
	github.com/tetratelabs/wabin v0.0.0-20230304001439-f6f874872834 // indirect
	github.com/tetratelabs/wazero v1.11.0 // indirect
	github.com/uber/jaeger-client-go v2.30.0+incompatible // indirect
	github.com/uber/jaeger-lib v2.4.1+incompatible // indirect
	github.com/ulikunitz/xz v0.5.17 // indirect
	github.com/vbauerster/mpb/v7 v7.5.3 // indirect
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go4.org v0.0.0-20260112195520-a5071408f32f // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/exp v0.0.0-20220722155223-a9213eeb770e // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/errgo.v1 v1.0.1 // indirect
	gopkg.in/retry.v1 v1.0.3 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
