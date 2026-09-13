package appstore

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

var _ = Describe("AppStore (ReplicateSinf)", func() {
	var (
		ctrl               *gomock.Controller
		mockKeychain       *keychain.MockKeychain
		mockDownloadClient *http.MockClient[downloadResult]
		mockPurchaseClient *http.MockClient[purchaseResult]
		mockLoginClient    *http.MockClient[loginResult]
		mockHTTPClient     *http.MockClient[interface{}]
		mockOS             *operatingsystem.MockOperatingSystem
		mockMachine        *machine.MockMachine
		as                 AppStore
		testFile           *os.File
		testZip            *zip.Writer
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		mockLoginClient = http.NewMockClient[loginResult](ctrl)
		mockPurchaseClient = http.NewMockClient[purchaseResult](ctrl)
		mockHTTPClient = http.NewMockClient[interface{}](ctrl)
		mockOS = operatingsystem.NewMockOperatingSystem(ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		as = &appstore{
			keychain:       mockKeychain,
			loginClient:    mockLoginClient,
			purchaseClient: mockPurchaseClient,
			downloadClient: mockDownloadClient,
			httpClient:     mockHTTPClient,
			machine:        mockMachine,
			os:             mockOS,
		}

		var err error
		testFile, err = os.CreateTemp("", "test_file")
		Expect(err).ToNot(HaveOccurred())

		testZip = zip.NewWriter(testFile)
	})

	JustBeforeEach(func() {
		Expect(testZip.Close()).To(Succeed())
		Expect(testFile.Close()).To(Succeed())
	})

	AfterEach(func() {
		err := os.Remove(testFile.Name())
		Expect(err).ToNot(HaveOccurred())

		ctrl.Finish()
	})

	When("app includes codesign manifest", func() {
		BeforeEach(func() {
			mockOS.EXPECT().
				OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(os.OpenFile)

			mockOS.EXPECT().
				Remove(testFile.Name()).
				Return(nil)

			mockOS.EXPECT().
				Rename(fmt.Sprintf("%s.tmp", testFile.Name()), testFile.Name()).
				Return(nil)

			manifest, err := plist.Marshal(packageManifest{
				SinfPaths: []string{
					"SC_Info/TestApp.sinf",
				},
			}, plist.BinaryFormat)
			Expect(err).ToNot(HaveOccurred())

			w, err := testZip.Create("Payload/Test.app/SC_Info/Manifest.plist")
			Expect(err).ToNot(HaveOccurred())

			_, err = w.Write(manifest)
			Expect(err).ToNot(HaveOccurred())

			w, err = testZip.Create("Payload/Test.app/Info.plist")
			Expect(err).ToNot(HaveOccurred())

			info, err := plist.Marshal(map[string]interface{}{
				"CFBundleExecutable": "Test",
			}, plist.BinaryFormat)
			Expect(err).ToNot(HaveOccurred())

			_, err = w.Write(info)
			Expect(err).ToNot(HaveOccurred())

			w, err = testZip.Create("Payload/Test.app/Watch/Test.app/Info.plist")
			Expect(err).ToNot(HaveOccurred())

			watchInfo, err := plist.Marshal(map[string]interface{}{
				"WKWatchKitApp": true,
			}, plist.BinaryFormat)
			Expect(err).ToNot(HaveOccurred())

			_, err = w.Write(watchInfo)
			Expect(err).ToNot(HaveOccurred())
		})

		It("replicates sinf from manifest plist", func() {
			err := as.ReplicateSinf(ReplicateSinfInput{
				PackagePath: testFile.Name(),
				Sinfs: []Sinf{
					{
						ID:   0,
						Data: []byte(""),
					},
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("app does not include codesign manifest", func() {
		BeforeEach(func() {
			mockOS.EXPECT().
				OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(os.OpenFile)

			mockOS.EXPECT().
				Remove(testFile.Name()).
				Return(nil)

			mockOS.EXPECT().
				Rename(fmt.Sprintf("%s.tmp", testFile.Name()), testFile.Name()).
				Return(nil)

			w, err := testZip.Create("Payload/Test.app/Info.plist")
			Expect(err).ToNot(HaveOccurred())

			info, err := plist.Marshal(map[string]interface{}{
				"CFBundleExecutable": "Test",
			}, plist.BinaryFormat)
			Expect(err).ToNot(HaveOccurred())

			_, err = w.Write(info)
			Expect(err).ToNot(HaveOccurred())

			w, err = testZip.Create("Payload/Test.app/Watch/Test.app/Info.plist")
			Expect(err).ToNot(HaveOccurred())

			watchInfo, err := plist.Marshal(map[string]interface{}{
				"WKWatchKitApp": true,
			}, plist.BinaryFormat)
			Expect(err).ToNot(HaveOccurred())

			_, err = w.Write(watchInfo)
			Expect(err).ToNot(HaveOccurred())
		})

		It("replicates sinf", func() {
			err := as.ReplicateSinf(ReplicateSinfInput{
				PackagePath: testFile.Name(),
				Sinfs: []Sinf{
					{
						ID:   0,
						Data: []byte(""),
					},
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("fails to open file", func() {
		BeforeEach(func() {
			mockOS.EXPECT().
				OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, errors.New(""))
		})

		It("returns error", func() {
			err := as.ReplicateSinf(ReplicateSinfInput{
				PackagePath: testFile.Name(),
			})
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("SINF replication with optional license data", func() {
	DescribeTable("preserves package contents when Apple supplies no sinfs", func(withManifest bool, sinfs []Sinf) {
		packagePath := filepath.Join(GinkgoT().TempDir(), "app.ipa")
		file, err := os.Create(packagePath)
		Expect(err).ToNot(HaveOccurred())
		writer := zip.NewWriter(file)
		info, err := plist.Marshal(packageInfo{BundleExecutable: "Test"}, plist.BinaryFormat)
		Expect(err).ToNot(HaveOccurred())
		contents := map[string][]byte{
			"Payload/Test.app/Info.plist":        info,
			"Payload/Test.app/Test":              []byte("executable"),
			"Payload/Test.app/SC_Info/Test.supf": []byte("existing protection data"),
		}
		if withManifest {
			manifest, err := plist.Marshal(packageManifest{SinfPaths: []string{"SC_Info/Test.sinf"}}, plist.BinaryFormat)
			Expect(err).ToNot(HaveOccurred())
			contents["Payload/Test.app/SC_Info/Manifest.plist"] = manifest
		}
		for name, data := range contents {
			entry, err := writer.Create(name)
			Expect(err).ToNot(HaveOccurred())
			_, err = entry.Write(data)
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(writer.Close()).To(Succeed())
		Expect(file.Close()).To(Succeed())

		store := &appstore{os: operatingsystem.New()}
		Expect(store.ReplicateSinf(ReplicateSinfInput{PackagePath: packagePath, Sinfs: sinfs})).To(Succeed())

		reader, err := zip.OpenReader(packagePath)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(reader.Close)
		Expect(reader.File).To(HaveLen(len(contents)))
		for _, entry := range reader.File {
			Expect(contents).To(HaveKey(entry.Name))
			src, err := entry.Open()
			Expect(err).ToNot(HaveOccurred())
			data, err := io.ReadAll(src)
			Expect(err).ToNot(HaveOccurred())
			Expect(src.Close()).To(Succeed())
			Expect(data).To(Equal(contents[entry.Name]))
		}
		_, err = os.Stat(packagePath + ".tmp")
		Expect(os.IsNotExist(err)).To(BeTrue())
	},
		Entry("with a manifest and omitted sinfs", true, []Sinf(nil)),
		Entry("with a manifest and empty sinfs", true, []Sinf{}),
		Entry("without a manifest and omitted sinfs", false, []Sinf(nil)),
		Entry("without a manifest and empty sinfs", false, []Sinf{}),
	)
})
