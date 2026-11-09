package assets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

var _ = Describe("remote image HTTP requests", func() {
	It("reads image ranges from a server requiring a browser-compatible user agent", func() {
		const content = "restore image"

		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.UserAgent() != "Mozilla/5.0" {
				http.Error(response, "unsupported user agent", http.StatusForbidden)

				return
			}

			http.ServeContent(response, request, "restore.ipsw", time.Time{}, strings.NewReader(content))
		}))
		DeferCleanup(server.Close)

		transport := &http.Transport{}
		DeferCleanup(transport.CloseIdleConnections)

		client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			request = request.Clone(request.Context())
			request.URL.Scheme = "http"
			request.URL.Host = server.Listener.Addr().String()

			return transport.RoundTrip(request)
		})}

		remote, err := openRemoteFile(context.Background(), client)
		Expect(err).NotTo(HaveOccurred())
		Expect(remote.size).To(Equal(int64(len(content))))

		data := make([]byte, len(content)-1)
		count, err := remote.ReadAt(data, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(len(data)))
		Expect(string(data)).To(Equal(content[1:]))
	})
})
