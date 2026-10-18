package appstore

import (
	"encoding/json"
	"errors"

	apphttp "github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Authentication redirects", func() {
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
