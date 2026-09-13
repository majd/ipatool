package cmd

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/byteness/keyring"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("State directory", func() {
	var home, legacy, target string

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		legacy = filepath.Join(home, ConfigDirectoryName)
		target = filepath.Join(home, "state", "ipatool")
		GinkgoT().Setenv("XDG_STATE_HOME", filepath.Dir(target))
		GinkgoT().Setenv("XDG_DATA_HOME", "")
	})

	DescribeTable("selects the session directory", func(state, data, want string) {
		for variable, value := range map[string]string{"XDG_STATE_HOME": state, "XDG_DATA_HOME": data} {
			if value != "" && value != "relative" {
				value = filepath.Join(home, value)
			}

			GinkgoT().Setenv(variable, value)
		}

		got, err := prepareStateDirectory(operatingsystem.New(), home)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(filepath.Join(home, want)))
		Expect(got).To(BeADirectory())

		if got != legacy {
			Expect(legacy).ToNot(BeAnExistingFile())
		}
	},
		Entry("defaults to legacy storage", "", "", ConfigDirectoryName),
		Entry("prefers state storage", "state", "data", "state/ipatool"),
		Entry("falls back to data storage", "", "data", "data/ipatool"),
		Entry("ignores a relative state path", "relative", "data", "data/ipatool"),
		Entry("ignores relative paths", "relative", "relative", ConfigDirectoryName),
	)

	It("preserves authentication after migration and subsequent startup", func() {
		Expect(os.MkdirAll(legacy, 0700)).To(Succeed())

		openKeyring := func(directory string) keyring.Keyring {
			ring, err := keyring.Open(keyring.Config{
				AllowedBackends:  []keyring.BackendType{keyring.FileBackend},
				ServiceName:      KeychainServiceName,
				FileDir:          directory,
				FilePasswordFunc: keyring.FixedStringPrompt("test-passphrase"),
			})
			ExpectWithOffset(1, err).ToNot(HaveOccurred())

			return ring
		}
		item := keyring.Item{Key: "account", Data: []byte("existing-session"), Label: KeychainServiceName}
		Expect(openKeyring(legacy).Set(item)).To(Succeed())

		cookieURL, err := url.Parse("https://apps.apple.com/")
		Expect(err).ToNot(HaveOccurred())

		jar := newCookieJar(legacy)
		jar.SetCookies(cookieURL, []*http.Cookie{{Name: "session", Value: "existing-cookie", Path: "/", Expires: time.Now().Add(time.Hour)}})
		Expect(jar.Save()).To(Succeed())

		before, err := os.ReadFile(filepath.Join(legacy, item.Key))
		Expect(err).ToNot(HaveOccurred())

		for i := 0; i < 2; i++ {
			directory, err := prepareStateDirectory(operatingsystem.New(), home)
			Expect(err).ToNot(HaveOccurred())
			Expect(directory).To(Equal(target))

			got, err := openKeyring(directory).Get(item.Key)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Data).To(Equal(item.Data))

			after, err := os.ReadFile(filepath.Join(directory, item.Key))
			Expect(err).ToNot(HaveOccurred())
			Expect(after).To(Equal(before), "encrypted credentials must remain unchanged")

			cookies := newCookieJar(directory).Cookies(cookieURL)
			Expect(cookies).To(HaveLen(1))
			Expect(cookies[0].Name).To(Equal("session"))
			Expect(cookies[0].Value).To(Equal("existing-cookie"))
		}

		Expect(legacy).ToNot(BeAnExistingFile())
	})

	DescribeTable("preserves legacy storage when migration fails", func(mkdirErr, renameErr error) {
		Expect(os.MkdirAll(legacy, 0700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(legacy, CookieJarFileName), []byte("session"), 0600)).To(Succeed())

		got, err := prepareStateDirectory(migrationFailureOS{operatingsystem.New(), mkdirErr, renameErr}, home)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(legacy))

		data, err := os.ReadFile(filepath.Join(got, CookieJarFileName))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("session"))
	},
		Entry("parent creation denied", os.ErrPermission, nil),
		Entry("rename denied", nil, os.ErrPermission),
		Entry("cross filesystem", nil, errors.New("cross-device link")),
	)

	It("preserves conflicting sessions and uses legacy storage", func() {
		for _, directory := range []string{legacy, target} {
			Expect(os.MkdirAll(directory, 0700)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(directory, "account"), []byte(directory), 0600)).To(Succeed())
		}

		got, err := prepareStateDirectory(operatingsystem.New(), home)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(legacy))

		for _, directory := range []string{legacy, target} {
			data, err := os.ReadFile(filepath.Join(directory, "account"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(data)).To(Equal(directory))
		}
	})

	DescribeTable("rejects files at directory paths", func(useLegacy bool) {
		path := target
		if useLegacy {
			path = legacy
		}

		Expect(os.MkdirAll(filepath.Dir(path), 0700)).To(Succeed())
		Expect(os.WriteFile(path, []byte("existing file"), 0600)).To(Succeed())

		_, err := prepareStateDirectory(operatingsystem.New(), home)
		Expect(err).To(HaveOccurred())
	},
		Entry("legacy", true),
		Entry("destination", false),
	)

	It("preserves legacy storage for a nested destination", func() {
		GinkgoT().Setenv("XDG_STATE_HOME", filepath.Join(legacy, "state"))
		Expect(os.MkdirAll(legacy, 0700)).To(Succeed())

		got, err := prepareStateDirectory(operatingsystem.New(), home)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(legacy))

		entries, err := os.ReadDir(legacy)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty(), "migration must not create directories inside legacy storage")
	})
})

type migrationFailureOS struct {
	operatingsystem.OperatingSystem
	mkdirErr, renameErr error
}

// nolint:wrapcheck
func (o migrationFailureOS) MkdirAll(path string, mode os.FileMode) error {
	if o.mkdirErr != nil {
		return o.mkdirErr
	}

	return o.OperatingSystem.MkdirAll(path, mode)
}

func (o migrationFailureOS) Rename(from, to string) error {
	return o.renameErr
}
