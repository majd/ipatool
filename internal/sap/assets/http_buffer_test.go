package assets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Buffered remote ranges", func() {
	var source []byte
	var remote *remoteFile
	var requests []string
	var mu sync.Mutex
	BeforeEach(func() {
		source = make([]byte, 1<<20)
		for n := range source {
			source[n] = byte(n % 251)
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requests = append(requests, r.Header.Get("Range"))
			mu.Unlock()
			http.ServeContent(w, r, "image", time.Time{}, bytes.NewReader(source))
		}))
		DeferCleanup(server.Close)
		requests = nil
		remote = &remoteFile{ctx: context.Background(), client: server.Client(), url: server.URL, size: int64(len(source))}
	})
	It("serves a small sequential stream in bounded larger transfers", func() {
		output := make([]byte, len(source))
		for offset := 0; offset < len(output); offset += 4096 {
			count, err := remote.ReadAt(output[offset:offset+4096], int64(offset))
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(4096))
		}
		Expect(output).To(Equal(source))
		mu.Lock()
		count := len(requests)
		mu.Unlock()
		Expect(count).To(Equal(5))
		Expect(len(remote.cache)).To(BeNumerically("<=", remoteReadAheadSize))
	})
	It("keeps sparse metadata requests exact", func() {
		for _, offset := range []int64{0, 500000, 900000} {
			output := make([]byte, 8)
			_, err := remote.ReadAt(output, offset)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal(source[offset : offset+8]))
		}
		mu.Lock()
		defer mu.Unlock()
		Expect(requests).To(Equal([]string{"bytes=0-7", "bytes=500000-500007", "bytes=900000-900007"}))
	})
	It("isolates cached bytes from caller mutation and reports EOF correctly", func() {
		data := make([]byte, 4096)
		_, err := remote.ReadAt(data, 0)
		Expect(err).NotTo(HaveOccurred())
		clear(data)
		_, err = remote.ReadAt(data, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(source[:4096]))
		tail := make([]byte, 32)
		count, err := remote.ReadAt(tail, int64(len(source)-16))
		Expect(err).To(MatchError(io.EOF))
		Expect(count).To(Equal(16))
		Expect(tail[:count]).To(Equal(source[len(source)-16:]))
	})
	It("honors cancellation even when data is buffered", func() {
		ctx, cancel := context.WithCancel(context.Background())
		remote.ctx = ctx
		data := make([]byte, 32)
		_, err := remote.ReadAt(data, 0)
		Expect(err).NotTo(HaveOccurred())
		cancel()
		_, err = remote.ReadAt(data, 0)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	})
	It("supports concurrent independent reads", func() {
		finished := make(chan error, 8)
		for n := range 8 {
			go func() {
				offset := int64(n * 40000)
				output := make([]byte, 4096)
				_, err := remote.ReadAt(output, offset)
				if err == nil && !bytes.Equal(output, source[offset:offset+4096]) {
					err = errors.New("incorrect concurrent range")
				}
				finished <- err
			}()
		}
		for range 8 {
			Eventually(finished).Should(Receive(BeNil()))
		}
	})
})

var _ = Describe("Interrupted remote responses", func() {
	It("resumes a short response at the first unread byte", func() {
		var ranges []string
		remote := &remoteFile{ctx: context.Background(), url: "https://example.test/image", size: 8, client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			ranges = append(ranges, r.Header.Get("Range"))
			if len(ranges) == 1 {
				return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Range": []string{"bytes 0-7/8"}}, Body: io.NopCloser(strings.NewReader("abcd"))}, nil
			}

			return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Range": []string{"bytes 4-7/8"}}, Body: io.NopCloser(strings.NewReader("efgh"))}, nil
		})}}
		data := make([]byte, 8)
		count, err := remote.ReadAt(data, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(8))
		Expect(string(data)).To(Equal("abcdefgh"))
		Expect(ranges).To(Equal([]string{"bytes=0-7", "bytes=4-7"}))
	})
	It("rejects incorrect response ranges without caching them", func() {
		remote := &remoteFile{ctx: context.Background(), url: "https://example.test/image", size: 8, client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Range": []string{"bytes 1-7/8"}}, Body: io.NopCloser(strings.NewReader("1234567"))}, nil
		})}}
		_, err := remote.ReadAt(make([]byte, 8), 0)
		Expect(err).To(MatchError(ContainSubstring("server returned bytes")))
		Expect(remote.cache).To(BeEmpty())
	})
	It("does not fail a completed requested prefix when speculative tail reads fail", func() {
		remote := &remoteFile{ctx: context.Background(), url: "https://example.test/image", size: 1 << 20}
		calls := 0
		remote.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			switch calls {
			case 1:
				return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Range": []string{"bytes 0-3/1048576"}}, Body: io.NopCloser(strings.NewReader("abcd"))}, nil
			case 2:
				return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Range": []string{fmt.Sprintf("bytes 4-%d/1048576", 4+remoteReadAheadSize-1)}}, Body: io.NopCloser(strings.NewReader("efgh"))}, nil
			default:
				return nil, errors.New("connection interrupted")
			}
		})}
		data := make([]byte, 4)
		_, err := remote.ReadAt(data, 0)
		Expect(err).NotTo(HaveOccurred())
		count, err := remote.ReadAt(data, 4)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(4))
		Expect(string(data)).To(Equal("efgh"))
		Expect(remote.cacheOffset).To(BeZero())
		Expect(remote.cache).To(HaveLen(4))
	})
})

var _ = Describe("Remote image size probes", func() {
	DescribeTable("requires exactly the requested byte", func(body string, valid bool) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Range": []string{"bytes 0-0/123"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		remote, err := openRemoteFile(context.Background(), client)
		if valid {
			Expect(err).NotTo(HaveOccurred())
			Expect(remote.size).To(Equal(int64(123)))
		} else {
			Expect(err).To(MatchError(ContainSubstring("probe has an invalid length")))
		}
	}, Entry("one byte", "x", true), Entry("empty body", "", false), Entry("too much data", "xx", false))
})
