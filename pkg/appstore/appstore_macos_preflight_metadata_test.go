package appstore

import (
	"bytes"
	"crypto/sha1" // #nosec G505 -- XAR uses SHA-1 for archive checksums.
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/blacktop/go-macho/pkg/xar"
	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

func macPreflightXAR(distribution string) []byte {
	data := []byte(distribution)
	checksum := sha1.Sum(data) // #nosec G401 -- XAR uses SHA-1 for archive checksums.
	entry := fmt.Sprintf(`<file id="1"><type>file</type><name>Distribution</name><data><length>%d</length><offset>%d</offset><size>%d</size><encoding style="application/octet-stream"/><archived-checksum style="sha1">%x</archived-checksum><extracted-checksum style="sha1">%x</extracted-checksum></data></file>`, len(data), sha1.Size, len(data), checksum, checksum)

	// The live preflight packages were repacked in 2023. Their creation time
	// must not replace the original build timestamp in the installer version.
	return makeTestXARArchive(`<creation-time>2023-01-04T22:53:15</creation-time>`+entry, data...)
}

func macPreflightDistribution(version, packageVersion string) string {
	return fmt.Sprintf(`<installer-gui-script><product id="com.example.mac" version="%s"/>
<pkg-ref id="com.example.pkg"><relocate><bundle id="com.example.mac"/></relocate></pkg-ref>
<pkg-ref id="com.unrelated.pkg" version="9.9.9.0.1.1700000000"/>
<pkg-ref id="com.example.pkg" version="%s"/>
<pkg-ref id="com.example.pkg" version="%s"/></installer-gui-script>`, version, packageVersion, packageVersion)
}

var _ = Describe("Mac preflight version metadata", func() {
	DescribeTable("validates the selected product and its build timestamp", func(distribution, bundle, expectedError string) {
		data := macPreflightXAR(distribution)
		archive, err := xar.NewReader(bytes.NewReader(data), int64(len(data)))
		Expect(err).ToNot(HaveOccurred())
		metadata, err := macPreflightVersionMetadata(archive, bundle)
		if expectedError != "" {
			Expect(err).To(MatchError(ContainSubstring(expectedError)))
		} else {
			Expect(err).ToNot(HaveOccurred())
			Expect(metadata.DisplayVersion).To(Equal("4.2"))
			Expect(metadata.ReleaseDate).To(Equal(time.Unix(1341568473, 0).UTC()))
		}
	},
		Entry("unique product without a bundle filter", macPreflightDistribution("4.2", "4.2.0.0.1.1341568473"), "", ""),
		Entry("wrong bundle", macPreflightDistribution("4.2", "4.2.0.0.1.1341568473"), "com.other.mac", "requested bundle"),
		Entry("missing version", macPreflightDistribution("", "4.2.0.0.1.1341568473"), "", "display version"),
		Entry("missing timestamp", macPreflightDistribution("4.2", ""), "", "build timestamp"),
		Entry("ordinary dotted version", macPreflightDistribution("4.2", "4.2.0"), "", "supported build timestamp"),
		Entry("different package version", macPreflightDistribution("4.2", "4.3.0.0.1.1341568473"), "", "supported build timestamp"),
		Entry("invalid timestamp", macPreflightDistribution("4.2", "4.2.0.0.1.abcdefghij"), "", "invalid installer version"),
		Entry("zero timestamp", macPreflightDistribution("4.2", "4.2.0.0.1.0000000000"), "", "invalid build timestamp"),
		Entry("unrelated package", strings.ReplaceAll(macPreflightDistribution("4.2", "4.2.0.0.1.1341568473"), `<bundle id="com.example.mac"`, `<bundle id="com.other.mac"`), "", "requested bundle"),
		Entry("conflicting timestamps", strings.Replace(macPreflightDistribution("4.2", "4.2.0.0.1.1341568473"), "1341568473", "1341568474", 1), "", "conflicting build timestamps"),
		Entry("ambiguous products", strings.Replace(macPreflightDistribution("4.2", "4.2.0.0.1.1341568473"), "</installer-gui-script>", `<product id="com.other.mac" version="4.2"/></installer-gui-script>`, 1), "", "unique product"),
		Entry("malformed XML", "<", "", "failed to parse"),
	)

	It("rejects corrupted installer metadata", func() {
		data := macPreflightXAR(macPreflightDistribution("4.2", "4.2.0.0.1.1341568473"))
		data[len(data)-1] ^= 1
		archive, err := xar.NewReader(bytes.NewReader(data), int64(len(data)))
		Expect(err).ToNot(HaveOccurred())
		_, err = macPreflightVersionMetadata(archive, "com.example.mac")
		Expect(err).To(MatchError(ContainSubstring("checksum mismatch")))
	})

	DescribeTable("reads the selected legacy version without downloading the encrypted app", func(platform Platform, softwarePlatform, versionID, displayVersion, packageVersion string, timestamp int64) {
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()
		tempRoot := GinkgoT().TempDir()
		GinkgoT().Setenv("TMPDIR", tempRoot)
		GinkgoT().Setenv("TMP", tempRoot)
		GinkgoT().Setenv("TEMP", tempRoot)
		data := macPreflightXAR(macPreflightDistribution(displayVersion, packageVersion))
		var encryptedRequests atomic.Int64
		server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
			if r.URL.Path != "/preflight.pkg" {
				encryptedRequests.Add(1)
				w.WriteHeader(gohttp.StatusInternalServerError)

				return
			}
			_, _ = w.Write(data)
		}))
		defer server.Close()
		downloads := http.NewMockClient[downloadResult](ctrl)
		machines := machine.NewMockMachine(ctrl)
		store := &appstore{downloadClient: downloads, machine: machines, os: operatingsystem.New(), httpClient: http.NewClient[interface{}](http.Args{})}
		machines.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
			Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("externalVersionId", versionID))
		}).Return(http.Result[downloadResult]{StatusCode: 200, Data: downloadResult{Items: []downloadItemResult{{
			URL: server.URL + "/encrypted.pkg", PreflightPackageURL: server.URL + "/preflight.pkg",
			Metadata: map[string]interface{}{
				"software-platform": softwarePlatform, "product-type": "mac-os-app", "softwareVersionBundleId": "com.example.mac",
				"bundleShortVersionString": "4.3", "releaseDate": "2011-01-03T08:00:00Z",
			},
		}}}}, nil)

		out, err := store.GetVersionMetadata(GetVersionMetadataInput{App: App{ID: 409201541}, VersionID: versionID, Platform: platform})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.DisplayVersion).To(Equal(displayVersion))
		Expect(out.ReleaseDate).To(Equal(time.Unix(timestamp, 0).UTC()))
		Expect(encryptedRequests.Load()).To(BeZero())
		entries, err := os.ReadDir(tempRoot)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	},
		Entry("4.0.5 without a platform", Platform(""), "macos", "3235485", "4.0.5", "4.0.5.0.1.1292967101", int64(1292967101)),
		Entry("4.0.5 with macOS", PlatformMacOS, "macos", "3235485", "4.0.5", "4.0.5.0.1.1292967101", int64(1292967101)),
		Entry("4.2 without a platform", Platform(""), "macos", "12013122", "4.2", "4.2.0.0.1.1341568473", int64(1341568473)),
		Entry("4.2 with macOS", PlatformMacOS, "macos", "12013122", "4.2", "4.2.0.0.1.1341568473", int64(1341568473)),
		Entry("product type without platform metadata", Platform(""), "", "12013122", "4.2", "4.2.0.0.1.1341568473", int64(1341568473)),
	)
})
