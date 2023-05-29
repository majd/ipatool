package operatingsystem

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path"
	"testing"
	"time"

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
		var file *os.File

		BeforeEach(func() {
			var err error

			file, err = os.CreateTemp("", "test_file")
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err := file.Close()
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns file info", func() {
			res, err := sut.Stat(file.Name())
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Name()).To(Equal(path.Base(file.Name())))
		})

		It("opens file", func() {
			res, err := sut.OpenFile(file.Name(), os.O_WRONLY, 0644)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Name()).To(Equal(file.Name()))
		})

		It("removes file", func() {
			err := sut.Remove(file.Name())
			Expect(err).ToNot(HaveOccurred())

			_, err = sut.Stat(file.Name())
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("renames file", func() {
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			newPath := fmt.Sprintf("%s/%d", os.TempDir(), r.Intn(100))

			err := sut.Rename(file.Name(), newPath)
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
