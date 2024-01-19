package appstore

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"

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
		testZip.Close()
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
