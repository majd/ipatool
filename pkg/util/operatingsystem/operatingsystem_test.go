package operatingsystem

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OperatingSystem Suite")
}

var _ = Describe("OperatingSystem", func() {
	var sut OperatingSystem

	BeforeEach(func() {
		sut = New()
	})

	When("env var is set", func() {
		BeforeEach(func() {
			err := os.Setenv("TEST", "true")
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns env var", func() {
			res := sut.Getenv("TEST")
			Expect(res).To(Equal("true"))
		})
	})

	When("file exists", func() {
		var filePath string

		BeforeEach(func() {
			file, err := os.CreateTemp("", "test_file")
			Expect(err).ToNot(HaveOccurred())

			filePath = file.Name()
			Expect(file.Close()).To(Succeed())
		})

		AfterEach(func() {
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("returns file info", func() {
			res, err := sut.Stat(filePath)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Name()).To(Equal(filepath.Base(filePath)))
		})

		It("opens file", func() {
			res, err := sut.OpenFile(filePath, os.O_WRONLY, 0644)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Name()).To(Equal(filePath))
			Expect(res.Close()).To(Succeed())
		})

		It("removes file", func() {
			err := sut.Remove(filePath)
			Expect(err).ToNot(HaveOccurred())

			_, err = sut.Stat(filePath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("renames file", func() {
			newPath := filePath + ".renamed"

			err := sut.Rename(filePath, newPath)
			defer func() {
				_ = sut.Remove(newPath)
			}()

			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("running", func() {
		It("returns current working directory", func() {
			res, err := sut.Getwd()
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
		})
	})

	When("error is 'ErrNotExist'", func() {
		It("returns true", func() {
			res := sut.IsNotExist(fs.ErrNotExist)
			Expect(res).To(BeTrue())
		})
	})

	When("directory does not exist", func() {
		It("creates directory", func() {
			err := sut.MkdirAll(os.TempDir(), 0664)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
