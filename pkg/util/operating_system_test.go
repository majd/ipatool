package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"io/fs"
	goos "os"
	"path"
)

var _ = Describe("Operating System", func() {
	var (
		os OperatingSystem
	)

	BeforeEach(func() {
		os = NewOperatingSystem()
	})

	When("env var is set", func() {
		BeforeEach(func() {
			err := goos.Setenv("TEST", "true")
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns env var", func() {
			res := os.Getenv("TEST")
			Expect(res).To(Equal("true"))
		})
	})

	When("file exists", func() {
		var file *goos.File

		BeforeEach(func() {
			var err error

			file, err = goos.CreateTemp("", "test_file")
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err := file.Close()
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns file info", func() {
			res, err := os.Stat(file.Name())
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Name()).To(Equal(path.Base(file.Name())))
		})

		It("opens file", func() {
			res, err := os.OpenFile(file.Name(), goos.O_WRONLY, 0644)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Name()).To(Equal(file.Name()))
		})

		It("removes file", func() {
			err := os.Remove(file.Name())
			Expect(err).ToNot(HaveOccurred())

			_, err = os.Stat(file.Name())
			Expect(goos.IsNotExist(err)).To(BeTrue())
		})
	})

	When("running", func() {
		It("returns current working directory", func() {
			res, err := os.Getwd()
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
		})
	})

	When("error is 'ErrNotExist'", func() {
		It("returns true", func() {
			res := os.IsNotExist(fs.ErrNotExist)
			Expect(res).To(BeTrue())
		})
	})

	When("directory does not exist", func() {
		It("creates directory", func() {
			err := os.MkdirAll(goos.TempDir(), 0664)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
