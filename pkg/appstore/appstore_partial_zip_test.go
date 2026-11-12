package appstore

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	gohttp "net/http"
	"strings"

	apphttp "github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

func testRangeResponseReader(size int64, respond func(*gohttp.Request) *gohttp.Response) *httpRangeReaderAt {
	client := apphttp.NewMockClient[interface{}](gomock.NewController(GinkgoT()))
	client.EXPECT().NewRequest(gohttp.MethodGet, "https://example.test/app.ipa", nil).
		DoAndReturn(gohttp.NewRequest).AnyTimes()
	client.EXPECT().Do(gomock.Any()).DoAndReturn(func(request *gohttp.Request) (*gohttp.Response, error) {
		return respond(request), nil
	}).AnyTimes()

	return &httpRangeReaderAt{client: client, url: "https://example.test/app.ipa", size: size}
}

var _ = Describe("HTTP range read-ahead", func() {
	var data []byte

	BeforeEach(func() {
		data = make([]byte, 2*rangeReaderReadAhead)
		for i := range data {
			data[i] = byte(i % 251)
		}
	})

	It("refills a read that crosses the cached window", func() {
		ranges := []string{}
		server, _, _ := testIPAServerWithRangeLog(data, &ranges)
		defer server.Close()
		reader, _, err := newHTTPRangeReaderAt(newDownloadTestClient(), server.URL)
		Expect(err).NotTo(HaveOccurred())

		buffer := make([]byte, 4096)
		for _, offset := range []int64{0, rangeReaderReadAhead - 2048, rangeReaderReadAhead} {
			count, err := reader.ReadAt(buffer, offset)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(len(buffer)))
			Expect(buffer).To(Equal(data[offset : offset+int64(len(buffer))]))
		}

		Expect(ranges).To(Equal([]string{
			"bytes=0-0",
			fmt.Sprintf("bytes=0-%d", rangeReaderReadAhead-1),
			fmt.Sprintf("bytes=%d-%d", rangeReaderReadAhead-2048, 2*rangeReaderReadAhead-2049),
		}))
	})

	It("refills backward reads and isolates cached data from caller mutation", func() {
		ranges := []string{}
		server, _, _ := testIPAServerWithRangeLog(data, &ranges)
		defer server.Close()
		reader, _, err := newHTTPRangeReaderAt(newDownloadTestClient(), server.URL)
		Expect(err).NotTo(HaveOccurred())

		buffer := make([]byte, 4096)
		for _, offset := range []int64{rangeReaderReadAhead, 0, 0} {
			clear(buffer)
			count, err := reader.ReadAt(buffer, offset)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(len(buffer)))
			Expect(buffer).To(Equal(data[offset : offset+int64(len(buffer))]))
		}

		Expect(ranges).To(HaveLen(3))
	})

	It("preserves EOF behavior when reads are served from the cache", func() {
		ranges := []string{}
		server, _, _ := testIPAServerWithRangeLog([]byte("abcdef"), &ranges)
		defer server.Close()
		reader, size, err := newHTTPRangeReaderAt(newDownloadTestClient(), server.URL)
		Expect(err).NotTo(HaveOccurred())

		_, err = reader.ReadAt(make([]byte, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		buffer := make([]byte, 4)
		count, err := reader.ReadAt(buffer, 4)
		Expect(err).To(Equal(io.EOF))
		Expect(count).To(Equal(2))
		Expect(string(buffer[:count])).To(Equal("ef"))

		count, err = reader.ReadAt(buffer[:2], 4)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(2))

		count, err = reader.ReadAt(buffer, size)
		Expect(err).To(Equal(io.EOF))
		Expect(count).To(BeZero())
		Expect(ranges).To(Equal([]string{"bytes=0-0", "bytes=0-5"}))
	})

	It("supports independent concurrent reads", func() {
		server, _, _ := testIPAServer(data)
		defer server.Close()
		reader, _, err := newHTTPRangeReaderAt(newDownloadTestClient(), server.URL)
		Expect(err).NotTo(HaveOccurred())

		finished := make(chan error, 8)
		for i := range 8 {
			go func() {
				offset := int64(i * 200000)
				buffer := make([]byte, 4096)
				_, err := reader.ReadAt(buffer, offset)
				if err == nil && !bytes.Equal(buffer, data[offset:offset+int64(len(buffer))]) {
					err = errors.New("incorrect concurrent range")
				}

				finished <- err
			}()
		}

		for range 8 {
			Eventually(finished).Should(Receive(BeNil()))
		}
	})

	DescribeTable("preserves received bytes when read-ahead is interrupted", func(body string, expectedErr error) {
		calls := 0
		reader := testRangeResponseReader(rangeReaderReadAhead, func(*gohttp.Request) *gohttp.Response {
			calls++

			return &gohttp.Response{
				StatusCode: gohttp.StatusPartialContent,
				Header:     gohttp.Header{"Content-Range": []string{fmt.Sprintf("bytes 0-%d/%d", rangeReaderReadAhead-1, rangeReaderReadAhead)}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}
		})

		for range 2 {
			buffer := make([]byte, 4)
			count, err := reader.ReadAt(buffer, 0)
			if expectedErr == nil {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(errors.Is(err, expectedErr)).To(BeTrue())
			}

			Expect(count).To(Equal(min(len(body), len(buffer))))
			Expect(string(buffer[:count])).To(Equal(body[:count]))
		}

		Expect(calls).To(Equal(2))
	},
		Entry("a complete requested prefix", "abcd", nil),
		Entry("some speculative bytes", "abcdef", nil),
		Entry("an incomplete requested prefix", "abc", io.ErrUnexpectedEOF),
		Entry("an empty response body", "", io.EOF),
	)

	It("retains the previous cache after an interrupted read-ahead request", func() {
		calls := 0
		reader := testRangeResponseReader(2*rangeReaderReadAhead, func(request *gohttp.Request) *gohttp.Response {
			calls++
			start, stop, err := testRangeBounds(request.Header.Get("Range"), int(2*rangeReaderReadAhead))
			Expect(err).NotTo(HaveOccurred())
			body := []byte("bcde")
			if start == 0 {
				body = bytes.Repeat([]byte("a"), int(rangeReaderReadAhead))
			}

			return &gohttp.Response{
				StatusCode: gohttp.StatusPartialContent,
				Header:     gohttp.Header{"Content-Range": []string{fmt.Sprintf("bytes %d-%d/%d", start, stop, 2*rangeReaderReadAhead)}},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}
		})

		buffer := make([]byte, 4)
		for _, offset := range []int64{0, rangeReaderReadAhead, 0} {
			_, err := reader.ReadAt(buffer, offset)
			Expect(err).NotTo(HaveOccurred())
			if offset == 0 {
				Expect(string(buffer)).To(Equal("aaaa"))
			} else {
				Expect(string(buffer)).To(Equal("bcde"))
			}
		}

		Expect(calls).To(Equal(2))
	})

	DescribeTable("rejects invalid response ranges before caching", func(header string) {
		calls := 0
		reader := testRangeResponseReader(32, func(*gohttp.Request) *gohttp.Response {
			calls++

			return &gohttp.Response{
				StatusCode: gohttp.StatusPartialContent,
				Header:     gohttp.Header{"Content-Range": []string{header}},
				Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", 28))),
			}
		})

		buffer := make([]byte, 4)
		count, err := reader.ReadAt(buffer, 4)
		Expect(err).To(MatchError(ContainSubstring("content range")))
		Expect(count).To(BeZero())

		header = "bytes 4-31/32"
		count, err = reader.ReadAt(buffer, 4)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(4))
		Expect(string(buffer)).To(Equal("xxxx"))
		Expect(calls).To(Equal(2))
	},
		Entry("missing header", ""),
		Entry("wrong start", "bytes 5-31/32"),
		Entry("wrong end", "bytes 4-30/32"),
		Entry("wrong file size", "bytes 4-31/33"),
		Entry("unknown file size", "bytes 4-31/*"),
		Entry("wrong unit", "items 4-31/32"),
		Entry("negative start", "bytes -4-31/32"),
		Entry("reversed bounds", "bytes 31-4/32"),
		Entry("end beyond EOF", "bytes 4-32/32"),
	)

	It("clamps EOF reads without overflowing large offsets", func() {
		reader := testRangeResponseReader(math.MaxInt64, func(request *gohttp.Request) *gohttp.Response {
			Expect(request.Header.Get("Range")).To(Equal(fmt.Sprintf("bytes=%d-%d", int64(math.MaxInt64-4), int64(math.MaxInt64-1))))

			return &gohttp.Response{
				StatusCode: gohttp.StatusPartialContent,
				Header:     gohttp.Header{"Content-Range": []string{fmt.Sprintf("bytes %d-%d/%d", int64(math.MaxInt64-4), int64(math.MaxInt64-1), int64(math.MaxInt64))}},
				Body:       io.NopCloser(strings.NewReader("tail")),
			}
		})

		buffer := make([]byte, 8)
		count, err := reader.ReadAt(buffer, math.MaxInt64-4)
		Expect(err).To(Equal(io.EOF))
		Expect(count).To(Equal(4))
		Expect(string(buffer[:count])).To(Equal("tail"))
	})
})
