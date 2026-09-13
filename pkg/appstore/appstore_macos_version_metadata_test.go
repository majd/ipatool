package appstore

import (
	"bytes"
	"context"
	"crypto/sha1" // #nosec G505 -- XAR uses SHA-1 for archive checksums.
	"fmt"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/blacktop/go-macho/pkg/xar"
	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

func macMetadataXAR(info, modified string) []byte {
	payload := []byte("app payload")

	var entries string

	offset := sha1.Size

	for index, entry := range []struct {
		name string
		data []byte
	}{{"PackageInfo", []byte(info)}, {"Payload", payload}} {
		checksum := sha1.Sum(entry.data) // #nosec G401 -- XAR uses SHA-1 for archive checksums.
		entries += fmt.Sprintf(`<file id="%d"><type>file</type><name>%s</name><mtime>%s</mtime><data><length>%d</length><offset>%d</offset><size>%d</size><encoding style="application/octet-stream"/><archived-checksum style="sha1">%x</archived-checksum><extracted-checksum style="sha1">%x</extracted-checksum></data></file>`, index+1, entry.name, modified, len(entry.data), offset, len(entry.data), checksum, checksum)
		offset += len(entry.data)
	}

	return makeTestXARArchive(entries, append([]byte(info), payload...)...)
}

var _ = Describe("Mac version metadata", func() {
	const info = `<pkg-info><bundle id="com.example.mac" CFBundleShortVersionString="1.2.1"/></pkg-info>`
	const modified = "2025-07-24T08:38:42Z"

	DescribeTable("validates package metadata", func(contents, bundle, stamp string, success bool) {
		data := macMetadataXAR(contents, stamp)
		archive, err := xar.NewReader(bytes.NewReader(data), int64(len(data)))
		Expect(err).ToNot(HaveOccurred())
		metadata, err := macPackageVersionMetadata(archive, bundle)
		if success {
			Expect(err).ToNot(HaveOccurred())
			Expect(metadata.DisplayVersion).To(Equal("1.2.1"))
			Expect(metadata.ReleaseDate).To(Equal(time.Date(2025, 7, 24, 8, 38, 42, 0, time.UTC)))
		} else {
			Expect(err).To(HaveOccurred())
		}
	},
		Entry("matching bundle", info, "com.example.mac", modified, true),
		Entry("ID-only lookup with a unique bundle", info, "", modified, true),
		Entry("wrong bundle", info, "com.other.mac", modified, false),
		Entry("missing version", `<pkg-info><bundle id="com.example.mac"/></pkg-info>`, "com.example.mac", modified, false),
		Entry("missing timestamp", info, "com.example.mac", "", false),
		Entry("ambiguous bundles", `<pkg-info><bundle id="a" CFBundleShortVersionString="1"/><bundle id="b" CFBundleShortVersionString="2"/></pkg-info>`, "", modified, false),
		Entry("invalid XML", "<", "com.example.mac", modified, false),
	)

	It("reads Mac-compatible IPA metadata with the existing IPA reader", func() {
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()
		date := time.Date(2025, 7, 24, 8, 38, 42, 0, time.UTC)
		server, _, _ := testIPAServer(testIPA("2.0", date, date))
		defer server.Close()
		downloads := http.NewMockClient[downloadResult](ctrl)
		machines := machine.NewMockMachine(ctrl)
		store := &appstore{downloadClient: downloads, machine: machines, httpClient: http.NewClient[interface{}](http.Args{})}
		machines.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		downloads.EXPECT().Send(gomock.Any()).Return(http.Result[downloadResult]{StatusCode: 200, Data: downloadResult{Items: []downloadItemResult{{
			URL: server.URL, Sinfs: []Sinf{{Data: []byte("sinf")}}, Metadata: map[string]interface{}{"software-platform": "ios"},
		}}}}, nil)
		out, err := store.GetVersionMetadata(GetVersionMetadataInput{App: App{ID: 42}, Platform: PlatformMacOS, VersionID: "123"})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.DisplayVersion).To(Equal("2.0"))
		Expect(out.ReleaseDate).To(Equal(date))
	})

	It("decrypts the selected Mac version and ignores stale API metadata", func() {
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()
		tempRoot := GinkgoT().TempDir()
		GinkgoT().Setenv("TMPDIR", tempRoot)
		GinkgoT().Setenv("TMP", tempRoot)
		GinkgoT().Setenv("TEMP", tempRoot)
		server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) { _, _ = io.WriteString(w, "encrypted") }))
		defer server.Close()
		downloads := http.NewMockClient[downloadResult](ctrl)
		machines := machine.NewMockMachine(ctrl)
		decrypter := &fakeMacPackageDecrypter{output: macMetadataXAR(info, modified)}
		store := &appstore{downloadClient: downloads, machine: machines, os: operatingsystem.New(), httpClient: http.NewClient[interface{}](http.Args{}), macDecrypterFactory: func(ctx context.Context, hw, dp []byte) (macPackageDecrypter, error) {
			Expect(hw).To(Equal([]byte{0, 17, 34, 51, 68, 85}))
			Expect(dp).To(Equal([]byte("dpInfo")))

			return decrypter, nil
		}}
		machines.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
			Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("externalVersionId", "876660716"))
		}).Return(http.Result[downloadResult]{StatusCode: 200, Data: downloadResult{Items: []downloadItemResult{{URL: server.URL, Sinfs: []Sinf{{DPInfo: []byte("dpInfo")}}, Metadata: map[string]interface{}{"software-platform": "macos", "bundleShortVersionString": "9.9", "releaseDate": "2023-12-26T08:00:00Z"}}}}}, nil)
		out, err := store.GetVersionMetadata(GetVersionMetadataInput{App: App{ID: 42, BundleID: "com.example.mac"}, Platform: PlatformMacOS, VersionID: "876660716"})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.DisplayVersion).To(Equal("1.2.1"))
		Expect(out.ReleaseDate).To(Equal(time.Date(2025, 7, 24, 8, 38, 42, 0, time.UTC)))
		entries, err := os.ReadDir(tempRoot)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})
})
