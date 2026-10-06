package appstore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"time"

	apphttp "github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

var _ = Describe("Authentication redirects", func() {
	DescribeTable("preserves a signed POST, cookies, credentials and storefront after a credential retry",
		func(status int, suffix, appleID, storefront, authCode string) {
			ctrl := gomock.NewController(GinkgoT())
			jar, err := cookiejar.New(nil)
			Expect(err).NotTo(HaveOccurred())
			client := apphttp.NewClient[loginResult](apphttp.Args{CookieJar: loginSessionJar{jar}, Authentication: true})
			adapter := apphttp.NewMockClient[loginResult](ctrl)
			chain := keychain.NewMockKeychain(ctrl)
			chain.EXPECT().Set("account", gomock.Any()).Return(nil)
			sut := &appstore{loginClient: adapter, keychain: chain, authRetrySleep: func(time.Duration) { Fail("must not sleep") }}
			podURL := "https://p7-buy.itunes.apple.com" + PrivateAppStoreAPIPathAuth + suffix + "?routing=opaque"
			calls := 0
			var originalBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer GinkgoRecover()
				calls++
				Expect(r.Method).To(Equal(http.MethodPost))
				Expect(r.Header.Get("X-Apple-Store-Front")).To(Equal(storefront))
				body, err := io.ReadAll(r.Body)
				Expect(err).NotTo(HaveOccurred())
				signature, err := base64.StdEncoding.DecodeString(r.Header.Get(apphttp.HeaderAppleActionSignature))
				Expect(err).NotTo(HaveOccurred())
				Expect(signature).To(Equal(body))
				var payload map[string]string
				_, err = plist.Unmarshal(body, &payload)
				Expect(err).NotTo(HaveOccurred())
				Expect(payload["appleId"]).To(Equal(appleID))
				Expect(payload["password"]).To(Equal("password" + authCode))
				if calls > 1 {
					cookie, err := r.Cookie("session")
					Expect(err).NotTo(HaveOccurred())
					Expect(cookie.Value).To(Equal("retained"))
					Expect(payload["attempt"]).To(Equal("2"))
				}
				switch calls {
				case 1:
					Expect(payload["attempt"]).To(Equal("1"))
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "retained", Path: "/"})
					_, err = w.Write([]byte("<dict><key>failureType</key><string>-5000</string></dict>"))
					Expect(err).NotTo(HaveOccurred())
				case 2:
					originalBody = body
					w.Header().Set("Location", podURL)
					w.WriteHeader(status)
				case 3:
					Expect(body).To(Equal(originalBody))
					Expect(r.URL.Path).To(Equal(PrivateAppStoreAPIPathAuth + suffix))
					Expect(r.URL.RawQuery).To(Equal("routing=opaque"))
					w.Header().Set(HTTPHeaderStoreFront, "143441-1,29")
					_, err = w.Write([]byte("<dict><key>passwordToken</key><string>token</string><key>dsPersonId</key><string>123</string></dict>"))
					Expect(err).NotTo(HaveOccurred())
				default:
					Fail("unexpected request")
				}
			}))
			DeferCleanup(srv.Close)
			var destinations []string
			adapter.EXPECT().Send(gomock.Any()).DoAndReturn(func(request apphttp.Request) (apphttp.Result[loginResult], error) {
				destinations = append(destinations, request.URL)
				parsed, err := url.Parse(request.URL)
				Expect(err).NotTo(HaveOccurred())
				request.URL = srv.URL + parsed.RequestURI()

				return client.Send(request)
			}).Times(3)
			signer := &stubActionSigner{}
			account, err := sut.login(appleID, "password", authCode, "guid", testAuthEndpoint+suffix, signer)
			Expect(err).NotTo(HaveOccurred())
			Expect(account.PasswordToken).To(Equal("token"))
			Expect(account.StoreFront).To(Equal("143441-1,29"))
			Expect(destinations).To(Equal([]string{testAuthEndpoint + suffix, testAuthEndpoint + suffix, podURL}))
			Expect(signer.signCalls).To(Equal(3))
		},
		Entry("301, bare path", 301, "", "user@example.test", "", "123456"),
		Entry("301, trailing slash", 301, "/", "user@example.test", "", "123456"),
		Entry("302, bare path", 302, "", "user@example.test", "", "123456"),
		Entry("302, trailing slash", 302, "/", "user@example.test", "", "123456"),
		Entry("307, bare path", 307, "", "user@example.test", "", "123456"),
		Entry("307, trailing slash", 307, "/", "user@example.test", "", "123456"),
		Entry("308, bare path", 308, "", "user@example.test", "", "123456"),
		Entry("308, trailing slash", 308, "/", "user@example.test", "", "123456"),
		Entry("Chinese phone number before 2FA", 302, "/", "+86 139-1234-5678", "143465", ""),
		Entry("Indian phone number with 2FA", 307, "/", "9123456789", "143467", "012345"),
	)

	It("follows relative and pod redirects within the redirect budget", func() {
		ctrl := gomock.NewController(GinkgoT())
		client := apphttp.NewMockClient[loginResult](ctrl)
		chain := keychain.NewMockKeychain(ctrl)
		chain.EXPECT().Set("account", gomock.Any()).Return(nil)
		sut := &appstore{loginClient: client, keychain: chain}
		pod := "https://p7-buy.itunes.apple.com" + PrivateAppStoreAPIPathAuth
		urls := []string{testAuthEndpoint, testAuthEndpoint + "/", pod, pod + "/", pod + "/?routing=opaque"}
		locations := []string{PrivateAppStoreAPIPathAuth + "/", "//p7-buy.itunes.apple.com" + PrivateAppStoreAPIPathAuth, "authenticate/", "?routing=opaque"}
		statuses := []int{301, 302, 307, 308}
		calls := 0
		var original apphttp.Payload
		client.EXPECT().Send(gomock.Any()).DoAndReturn(func(request apphttp.Request) (apphttp.Result[loginResult], error) {
			Expect(request.URL).To(Equal(urls[calls]))
			if calls == 0 {
				original = request.Payload
			}
			Expect(request.Payload).To(BeIdenticalTo(original))
			calls++
			if calls <= len(locations) {
				return apphttp.Result[loginResult]{StatusCode: statuses[calls-1], Headers: map[string]string{"Location": locations[calls-1]}}, nil
			}

			return apphttp.Result[loginResult]{StatusCode: 200, Headers: map[string]string{HTTPHeaderStoreFront: "storefront"}, Data: loginResult{PasswordToken: "token", DirectoryServicesID: "123"}}, nil
		}).Times(5)
		_, err := sut.login("email", "password", "", "guid", testAuthEndpoint, &stubActionSigner{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("stops a redirect loop without retaining response secrets", func() {
		ctrl := gomock.NewController(GinkgoT())
		client := apphttp.NewMockClient[loginResult](ctrl)
		client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{
			StatusCode: 301,
			Headers:    map[string]string{"Location": testAuthEndpoint + "?token=secret-query", "Set-Cookie": "secret-cookie"},
			Data:       loginResult{PasswordToken: "secret-token"},
		}, nil).Times(maxAuthenticationRedirects + 1)
		sut := &appstore{loginClient: client}
		_, err := sut.login("email", "password", "", "guid", testAuthEndpoint, &stubActionSigner{})
		Expect(err).To(MatchError("too many authentication redirects (HTTP 301)"))
		var authErr *Error
		Expect(errors.As(err, &authErr)).To(BeTrue())
		Expect(authErr.Metadata).To(Equal(map[string]interface{}{"statusCode": 301, "destination": testAuthEndpoint}))
	})

	It("retries the first credential failure at the assigned pod", func() {
		ctrl := gomock.NewController(GinkgoT())
		client := apphttp.NewMockClient[loginResult](ctrl)
		sut := &appstore{loginClient: client}
		pod := "https://p7-buy.itunes.apple.com" + PrivateAppStoreAPIPathAuth + "/"
		calls := 0
		client.EXPECT().Send(gomock.Any()).DoAndReturn(func(request apphttp.Request) (apphttp.Result[loginResult], error) {
			calls++
			if calls == 1 {
				return apphttp.Result[loginResult]{StatusCode: 302, Headers: map[string]string{"Location": pod}}, nil
			}
			Expect(request.URL).To(Equal(pod))
			if calls == 2 {
				Expect(request.Payload.(*apphttp.XMLPayload).Content).To(HaveKeyWithValue("attempt", "1"))

				return apphttp.Result[loginResult]{StatusCode: 200, Data: loginResult{FailureType: FailureTypeInvalidCredentials}}, nil
			}
			Expect(request.Payload.(*apphttp.XMLPayload).Content).To(HaveKeyWithValue("attempt", "2"))

			return apphttp.Result[loginResult]{StatusCode: 200, Data: loginResult{FailureType: FailureTypeInvalidCredentials, CustomerMessage: "invalid credentials"}}, nil
		}).Times(3)
		_, err := sut.login("email", "password", "", "guid", testAuthEndpoint, &stubActionSigner{})
		Expect(err).To(MatchError("invalid credentials"))
	})

	DescribeTable("rejects unsafe or unsupported redirects before reposting credentials",
		func(status int, location string) {
			ctrl := gomock.NewController(GinkgoT())
			client := apphttp.NewMockClient[loginResult](ctrl)
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{
				StatusCode: status,
				Headers:    map[string]string{"Location": location, "Set-Cookie": "secret-cookie"},
				Data:       loginResult{PasswordToken: "secret-token"},
			}, nil).Times(1)
			sut := &appstore{loginClient: client}
			_, err := sut.login("email", "password", "", "guid", testAuthEndpoint, &stubActionSigner{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring("secret"))
			var authErr *Error
			Expect(errors.As(err, &authErr)).To(BeTrue())
			data, marshalErr := json.Marshal(authErr.Metadata)
			Expect(marshalErr).NotTo(HaveOccurred())
			Expect(string(data)).NotTo(ContainSubstring("secret"))
		},
		Entry("HTTP downgrade", 301, "http://buy.itunes.apple.com"+PrivateAppStoreAPIPathAuth),
		Entry("foreign host", 302, "https://example.com"+PrivateAppStoreAPIPathAuth+"?token=secret-query"),
		Entry("host suffix spoof", 307, "https://buy.itunes.apple.com.example.com"+PrivateAppStoreAPIPathAuth),
		Entry("foreign protocol-relative host", 308, "//example.com"+PrivateAppStoreAPIPathAuth),
		Entry("different path", 301, "https://buy.itunes.apple.com/other"),
		Entry("userinfo", 302, "https://user:secret-password@buy.itunes.apple.com"+PrivateAppStoreAPIPathAuth),
		Entry("different port", 307, "https://buy.itunes.apple.com:8443"+PrivateAppStoreAPIPathAuth),
		Entry("encoded path", 308, "https://buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/%61uthenticate"),
		Entry("fragment", 301, testAuthEndpoint+"#secret-fragment"),
		Entry("fragment after query", 302, testAuthEndpoint+"?routing=opaque#secret-fragment"),
		Entry("malformed URL", 302, "https://buy.itunes.apple.com/%secret"),
		Entry("empty Location", 307, ""),
		Entry("blank Location", 308, " \t"),
		Entry("303 GET redirect", 303, testAuthEndpoint+"?token=secret-query"),
		Entry("300 multiple choices", 300, testAuthEndpoint),
	)
})
