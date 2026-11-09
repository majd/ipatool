package appstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/schollz/progressbar/v3"
)

type downloadFixtureData byte

func (seed downloadFixtureData) ReadAt(buffer []byte, offset int64) (int, error) {
	for index := range buffer {
		buffer[index] = byte((offset+int64(index))%251) + byte(seed)
	}

	return len(buffer), nil
}

type downloadFixtureRequest struct {
	rangeHeader string
	validator   string
}

type parallelDownloadFixture struct {
	size     int64
	path     string
	store    *appstore
	server   *httptest.Server
	mu       sync.Mutex
	requests []downloadFixtureRequest
	active   atomic.Int64
	maximum  atomic.Int64
	handler  http.HandlerFunc
}

func newParallelDownloadFixture(size int64) *parallelDownloadFixture {
	fixture := &parallelDownloadFixture{
		size:  size,
		path:  filepath.Join(GinkgoT().TempDir(), "app.ipa.tmp"),
		store: &appstore{httpClient: newDownloadTestClient(), os: operatingsystem.New()},
	}

	fixture.handler = func(writer http.ResponseWriter, request *http.Request) {
		fixture.serve(writer, request, 1, `"fixture"`)
	}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer GinkgoRecover()

		active := fixture.active.Add(1)
		defer fixture.active.Add(-1)

		for maximum := fixture.maximum.Load(); active > maximum; maximum = fixture.maximum.Load() {
			if fixture.maximum.CompareAndSwap(maximum, active) {
				break
			}
		}

		fixture.mu.Lock()
		fixture.requests = append(fixture.requests, downloadFixtureRequest{
			rangeHeader: request.Header.Get("Range"),
			validator:   request.Header.Get("If-Range"),
		})
		fixture.mu.Unlock()

		fixture.handler(writer, request)
	}))
	DeferCleanup(fixture.server.Close)

	return fixture
}

func (fixture *parallelDownloadFixture) serve(writer http.ResponseWriter, request *http.Request, seed downloadFixtureData, validator string) {
	if validator != "" {
		writer.Header().Set("ETag", validator)
	}

	http.ServeContent(writer, request, "app.ipa", time.Time{}, io.NewSectionReader(seed, 0, fixture.size))
}

func (fixture *parallelDownloadFixture) recordedRequests() []downloadFixtureRequest {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()

	return append([]downloadFixtureRequest(nil), fixture.requests...)
}

func (fixture *parallelDownloadFixture) writePrefix(size int64) {
	file, err := os.Create(fixture.path)
	Expect(err).NotTo(HaveOccurred())

	_, err = io.Copy(file, io.NewSectionReader(downloadFixtureData(1), 0, size))
	Expect(err).NotTo(HaveOccurred())
	Expect(file.Close()).To(Succeed())
}

func (fixture *parallelDownloadFixture) expectContents(seed downloadFixtureData) {
	file, err := os.Open(fixture.path)
	Expect(err).NotTo(HaveOccurred())
	defer file.Close()

	info, err := file.Stat()
	Expect(err).NotTo(HaveOccurred())
	Expect(info.Size()).To(Equal(fixture.size))

	actual, expected := sha256.New(), sha256.New()
	_, err = io.Copy(actual, file)
	Expect(err).NotTo(HaveOccurred())
	_, err = io.Copy(expected, io.NewSectionReader(seed, 0, fixture.size))
	Expect(err).NotTo(HaveOccurred())
	Expect(actual.Sum(nil)).To(Equal(expected.Sum(nil)))
	Expect(downloadPartsPath(fixture.path)).NotTo(BeAnExistingFile())
	Expect(downloadPartsPath(fixture.path)).NotTo(BeADirectory())
}

var _ = Describe("automatic parallel downloads", func() {
	It("uses four requests when exactly 128 MiB remains and counts progress once", func() {
		const prefix = int64(4096)

		fixture := newParallelDownloadFixture(parallelDownloadThreshold + prefix)
		fixture.writePrefix(prefix)
		progress := progressbar.NewOptions64(1, progressbar.OptionSetWriter(io.Discard))

		err := fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, progress)
		Expect(err).NotTo(HaveOccurred())
		fixture.expectContents(1)

		requests := fixture.recordedRequests()
		Expect(requests).To(HaveLen(parallelDownloadCount))
		Expect(requests[0].rangeHeader).To(Equal("bytes=4096-"))
		for _, request := range requests[1:] {
			Expect(request.validator).To(Equal(`"fixture"`))
			Expect(request.rangeHeader).NotTo(HaveSuffix("-"))
		}

		Expect(fixture.maximum.Load()).To(Equal(int64(parallelDownloadCount)))
		Expect(progress.State().CurrentNum).To(Equal(fixture.size))
	})

	It("uses one request when one byte less than 128 MiB remains", func() {
		const prefix = int64(4097)

		fixture := newParallelDownloadFixture(parallelDownloadThreshold + prefix - 1)
		fixture.writePrefix(prefix)

		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(1)
		Expect(fixture.recordedRequests()).To(HaveLen(1))
	})

	It("uses one request for a small file", func() {
		fixture := newParallelDownloadFixture(64 << 10)

		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(1)
		Expect(fixture.recordedRequests()).To(HaveLen(1))
	})

	It("keeps one stream when the server ignores the initial range", func() {
		fixture := newParallelDownloadFixture(parallelDownloadThreshold)
		fixture.writePrefix(4096)
		fixture.handler = func(writer http.ResponseWriter, request *http.Request) {
			request.Header.Del("Range")
			fixture.serve(writer, request, 1, `"fixture"`)
		}

		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(1)
		Expect(fixture.recordedRequests()).To(HaveLen(1))
	})

	DescribeTable("falls back to one stream without appending invalid staged bytes", func(status int, contentRange string, body string) {
		fixture := newParallelDownloadFixture(parallelDownloadThreshold)
		fixture.handler = func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.Header.Get("Range"), "-") {
				fixture.serve(writer, request, 1, `"fixture"`)

				return
			}

			writer.Header().Set("Content-Range", contentRange)
			writer.WriteHeader(status)
			_, _ = io.WriteString(writer, body)
		}

		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(1)

		requests := fixture.recordedRequests()
		Expect(len(requests)).To(BeNumerically(">", 1))
		Expect(requests[len(requests)-1].rangeHeader).To(HaveSuffix("-"))
		Expect(requests[len(requests)-1].validator).To(Equal(`"fixture"`))
	},
		Entry("ignored bounded ranges", http.StatusOK, "", "a whole response"),
		Entry("wrong offsets", http.StatusPartialContent, "bytes 1-3/134217728", "bad"),
		Entry("content-length mismatch", http.StatusPartialContent, "bytes 33554432-67108863/134217728", "bad"),
		Entry("server errors", http.StatusServiceUnavailable, "", "unavailable"),
	)

	It("restarts the fallback when If-Range detects a changed representation", func() {
		fixture := newParallelDownloadFixture(parallelDownloadThreshold)
		var requests atomic.Int64
		fixture.handler = func(writer http.ResponseWriter, request *http.Request) {
			if requests.Add(1) == 1 {
				fixture.serve(writer, request, 1, `"old"`)

				return
			}

			fixture.serve(writer, request, 2, `"new"`)
		}

		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(2)
	})

	DescribeTable("keeps a single stream without a strong validator", func(validator string) {
		fixture := newParallelDownloadFixture(parallelDownloadThreshold)
		fixture.handler = func(writer http.ResponseWriter, request *http.Request) {
			fixture.serve(writer, request, 1, validator)
		}

		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(1)
		Expect(fixture.recordedRequests()).To(HaveLen(1))
	},
		Entry("missing ETag", ""),
		Entry("weak ETag", `W/"fixture"`),
	)

	It("falls back when a valid range response is truncated mid-body", func() {
		fixture := newParallelDownloadFixture(parallelDownloadThreshold)
		fixture.handler = func(writer http.ResponseWriter, request *http.Request) {
			value := request.Header.Get("Range")
			if strings.HasSuffix(value, "-") {
				fixture.serve(writer, request, 1, `"fixture"`)

				return
			}

			writer.Header().Set("Content-Range", strings.Replace(value, "=", " ", 1)+fmt.Sprintf("/%d", fixture.size))
			writer.Header().Set("ETag", `"fixture"`)
			writer.WriteHeader(http.StatusPartialContent)
			writer.(http.Flusher).Flush()
			_, _ = io.WriteString(writer, "truncated")
		}

		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(1)
		requests := fixture.recordedRequests()
		Expect(len(requests)).To(BeNumerically(">", 1))
		Expect(requests[len(requests)-1].rangeHeader).To(HaveSuffix("-"))
	})

	It("resumes the first stream after a disconnect and resets staged progress", func() {
		fixture := newParallelDownloadFixture(parallelDownloadThreshold)
		var first atomic.Bool
		fixture.handler = func(writer http.ResponseWriter, request *http.Request) {
			if first.CompareAndSwap(false, true) {
				writer.Header().Set("ETag", `"fixture"`)
				writer.Header().Set("Content-Length", fmt.Sprint(fixture.size))
				writer.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", fixture.size-1, fixture.size))
				writer.WriteHeader(http.StatusPartialContent)
				_, _ = io.Copy(writer, io.NewSectionReader(downloadFixtureData(1), 0, 4096))

				return
			}

			fixture.serve(writer, request, 1, `"fixture"`)
		}

		progress := progressbar.NewOptions64(1, progressbar.OptionSetWriter(io.Discard))
		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, progress)).To(Succeed())
		fixture.expectContents(1)
		requests := fixture.recordedRequests()
		Expect(len(requests)).To(BeNumerically(">", 1))
		Expect(requests[len(requests)-1].rangeHeader).To(Equal("bytes=4096-"))
		Expect(progress.State().CurrentNum).To(Equal(fixture.size))
	})

	It("preserves a contiguous prefix on interruption and resumes with one request below the threshold", func() {
		fixture := newParallelDownloadFixture(parallelDownloadThreshold)
		var interrupted atomic.Bool
		interrupted.Store(true)
		fixture.handler = func(writer http.ResponseWriter, request *http.Request) {
			if interrupted.Load() && strings.HasSuffix(request.Header.Get("Range"), "-") {
				writer.Header().Set("ETag", `"fixture"`)
				writer.Header().Set("Content-Length", fmt.Sprint(fixture.size))
				writer.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", fixture.size-1, fixture.size))
				writer.WriteHeader(http.StatusPartialContent)
				_, _ = io.Copy(writer, io.NewSectionReader(downloadFixtureData(1), 0, 4096))
				writer.(http.Flusher).Flush()
				<-request.Context().Done()

				return
			}

			fixture.serve(writer, request, 1, `"fixture"`)
		}

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)
		finished := make(chan error, 1)
		go func() {
			finished <- fixture.store.downloadFile(ctx, fixture.server.URL, fixture.path, nil)
		}()

		Eventually(func() int64 {
			var staged int64
			for index := range parallelDownloadCount - 1 {
				info, err := os.Stat(filepath.Join(downloadPartsPath(fixture.path), fmt.Sprint(index)))
				if err == nil {
					staged += info.Size()
				}
			}

			return staged
		}, 10*time.Second).Should(Equal(int64(parallelDownloadThreshold / 4 * 3)))
		Eventually(func() int64 {
			info, err := os.Stat(fixture.path)
			if err != nil {
				return 0
			}

			return info.Size()
		}).Should(Equal(int64(4096)))
		cancel()

		var downloadErr error
		Eventually(finished).Should(Receive(&downloadErr))
		Expect(errors.Is(downloadErr, context.Canceled)).To(BeTrue())
		info, err := os.Stat(fixture.path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Size()).To(Equal(int64(4096)))
		Expect(downloadPartsPath(fixture.path)).NotTo(BeADirectory())

		interrupted.Store(false)
		before := len(fixture.recordedRequests())
		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(1)
		requests := fixture.recordedRequests()
		Expect(requests).To(HaveLen(before + 1))
		Expect(requests[before].rangeHeader).To(Equal("bytes=4096-"))
	})

	It("discards orphaned parts after an unclean exit and resumes only the main file", func() {
		fixture := newParallelDownloadFixture(64 << 10)
		fixture.writePrefix(4096)
		directory := downloadPartsPath(fixture.path)
		Expect(os.Mkdir(directory, 0700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(directory, "0"), []byte("out-of-order bytes"), 0600)).To(Succeed())

		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(1)
		requests := fixture.recordedRequests()
		Expect(requests).To(HaveLen(1))
		Expect(requests[0].rangeHeader).To(Equal("bytes=4096-"))
	})

	It("resumes safely after the downloading process is killed", func() {
		fixture := newParallelDownloadFixture(parallelDownloadThreshold)
		var interrupted atomic.Bool
		interrupted.Store(true)
		fixture.handler = func(writer http.ResponseWriter, request *http.Request) {
			if interrupted.Load() && strings.HasSuffix(request.Header.Get("Range"), "-") {
				writer.Header().Set("ETag", `"fixture"`)
				writer.Header().Set("Content-Length", fmt.Sprint(fixture.size))
				writer.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", fixture.size-1, fixture.size))
				writer.WriteHeader(http.StatusPartialContent)
				_, _ = io.Copy(writer, io.NewSectionReader(downloadFixtureData(1), 0, 4096))
				writer.(http.Flusher).Flush()
				<-request.Context().Done()

				return
			}

			fixture.serve(writer, request, 1, `"fixture"`)
		}

		executable, err := os.Executable()
		Expect(err).NotTo(HaveOccurred())
		child := exec.Command(executable, "-test.run=^TestAppStore$", "-ginkgo.focus=download subprocess helper")
		child.Env = append(os.Environ(), "IPATOOL_DOWNLOAD_TEST_URL="+fixture.server.URL, "IPATOOL_DOWNLOAD_TEST_PATH="+fixture.path)
		Expect(child.Start()).To(Succeed())
		DeferCleanup(func() {
			_ = child.Process.Kill()
			_ = child.Wait()
		})

		Eventually(func() int64 {
			var size int64
			for index := range parallelDownloadCount - 1 {
				info, err := os.Stat(filepath.Join(downloadPartsPath(fixture.path), fmt.Sprint(index)))
				if err == nil {
					size += info.Size()
				}
			}

			return size
		}, 15*time.Second).Should(Equal(int64(parallelDownloadThreshold / 4 * 3)))
		Eventually(func() int64 {
			info, err := os.Stat(fixture.path)
			if err != nil {
				return 0
			}

			return info.Size()
		}).Should(Equal(int64(4096)))
		Expect(child.Process.Kill()).To(Succeed())
		Expect(child.Wait()).To(HaveOccurred())
		Expect(downloadPartsPath(fixture.path)).To(BeADirectory())

		interrupted.Store(false)
		before := len(fixture.recordedRequests())
		Expect(fixture.store.downloadFile(context.Background(), fixture.server.URL, fixture.path, nil)).To(Succeed())
		fixture.expectContents(1)
		requests := fixture.recordedRequests()
		Expect(requests).To(HaveLen(before + 1))
		Expect(requests[before].rangeHeader).To(Equal("bytes=4096-"))
	})
})

var _ = Describe("download subprocess helper", func() {
	It("downloads the subprocess fixture", func() {
		url := os.Getenv("IPATOOL_DOWNLOAD_TEST_URL")
		if url == "" {
			Skip("only executed by the process-interruption test")
		}

		store := &appstore{httpClient: newDownloadTestClient(), os: operatingsystem.New()}
		Expect(store.downloadFile(context.Background(), url, os.Getenv("IPATOOL_DOWNLOAD_TEST_PATH"), nil)).To(Succeed())
	})
})
