package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (Login)", func() {
	const (
		testPassword  = "test-password"
		testEmail     = "test-email"
		testFirstName = "test-first-name"
		testLastName  = "test-last-name"
		testPod       = "42"
	)

	var (
		ctrl          *gomock.Controller
		as            *appstore
		mockKeychain  *keychain.MockKeychain
		mockClient    *http.MockClient[loginResult]
		mockBagClient *http.MockClient[bagResult]
		mockMachine   *machine.MockMachine
		signer        *stubActionSigner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockClient = http.NewMockClient[loginResult](ctrl)
		mockBagClient = http.NewMockClient[bagResult](ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		signer = &stubActionSigner{}
		as = &appstore{
			keychain:       mockKeychain,
			loginClient:    mockClient,
			bagClient:      mockBagClient,
			machine:        mockMachine,
			authRetrySleep: func(time.Duration) {},
			actionSignerFactory: func(config SAPConfig, machineID []byte) (ActionSigner, error) {
				Expect(config).To(Equal(validSAPConfig()))
				Expect(machineID).To(Equal([]byte{0, 0, 0, 0, 0, 0}))

				return signer, nil
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("transient authentication responses", func() {
		It("retries the same request until it succeeds", func() {
			request := as.loginRequest(testEmail, testPassword, "", "guid", testAuthEndpoint, 1, signer)
			responses := []struct {
				result http.Result[loginResult]
				err    error
			}{
				{err: &http.UnexpectedResponseError{StatusCode: 204}},
				{err: &http.UnexpectedResponseError{StatusCode: 404}},
				{result: http.Result[loginResult]{StatusCode: 200}},
			}
			call := 0
			mockClient.EXPECT().
				Send(gomock.Any()).
				DoAndReturn(func(actual http.Request) (http.Result[loginResult], error) {
					Expect(actual.URL).To(Equal(request.URL))
					Expect(actual.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("attempt", "1"))
					response := responses[call]
					call++

					return response.result, response.err
				}).
				Times(3)

			result, err := as.sendAuthenticationRequest(request)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.StatusCode).To(Equal(200))
		})

		DescribeTable("bounds retries to transient statuses",
			func(status, expectedCalls int) {
				request := as.loginRequest(testEmail, testPassword, "", "guid", testAuthEndpoint, 1, signer)
				responseErr := &http.UnexpectedResponseError{StatusCode: status}
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[loginResult]{}, responseErr).
					Times(expectedCalls)

				_, err := as.sendAuthenticationRequest(request)

				Expect(errors.Is(err, responseErr)).To(BeTrue())
				if expectedCalls == maxAuthenticationRequestAttempts {
					Expect(err.Error()).To(ContainSubstring("after 3 attempts"))
					Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("HTTP %d, %d, %d", status, status, status)))
				}
			},
			Entry("retries and then stops for HTTP 204", 204, maxAuthenticationRequestAttempts),
			Entry("retries and then stops for HTTP 503", 503, maxAuthenticationRequestAttempts),
			Entry("does not retry HTTP 403", 403, 1),
		)
	})

	When("fails to read Machine's MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", errors.New(""))
		})

		It("returns error", func() {
			_, err := as.Login(LoginInput{
				Password: testPassword,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("successfully reads machine's MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)
			mockBagClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					Expect(req.URL).To(Equal("https://init.itunes.apple.com/bag.xml?guid=000000000000"))
				}).
				Return(http.Result[bagResult]{
					StatusCode: 200,
					Data:       validBagResult(),
				}, nil)
		})

		When("client returns error", func() {
			var clientErr error

			BeforeEach(func() {
				clientErr = errors.New("client error")
				mockClient.EXPECT().
					Send(gomock.Any()).
					Do(func(req http.Request) {
						Expect(req.URL).To(Equal(testAuthEndpoint))
						Expect(req.ActionSigner).To(BeIdenticalTo(signer))
					}).
					Return(http.Result[loginResult]{}, clientErr)
			})

			It("returns wrapped error", func() {
				_, err := as.Login(LoginInput{
					Password: testPassword,
				})
				Expect(err).To(HaveOccurred())
				Expect(errors.Is(err, clientErr)).To(BeTrue())
				Expect(signer.closeCalls).To(Equal(1))
			})

			It("preserves both the request and cleanup errors", func() {
				cleanupErr := errors.New("cleanup failed")
				signer.closeErr = cleanupErr

				_, err := as.Login(LoginInput{Password: testPassword})
				Expect(errors.Is(err, clientErr)).To(BeTrue())
				Expect(errors.Is(err, cleanupErr)).To(BeTrue())
				Expect(err.Error()).To(ContainSubstring("failed to close SAP action signer"))
				Expect(signer.closeCalls).To(Equal(1))
			})
		})

		When("the SAP signer fails to initialize", func() {
			BeforeEach(func() {
				as.actionSignerFactory = func(SAPConfig, []byte) (ActionSigner, error) {
					return nil, errors.New("signer error")
				}
			})

			It("returns a wrapped error", func() {
				_, err := as.Login(LoginInput{Password: testPassword})
				Expect(err).To(MatchError("failed to initialize SAP action signer: signer error"))
			})
		})

		When("store API returns invalid credentials on first attempt", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Do(func(req http.Request) {
						Expect(req.ActionSigner).To(BeIdenticalTo(signer))
					}).
					Return(http.Result[loginResult]{
						Data: loginResult{
							FailureType: FailureTypeInvalidCredentials,
						},
					}, nil).
					Times(2)
			})

			It("retries once then returns an error", func() {
				_, err := as.Login(LoginInput{
					Password: testPassword,
				})
				Expect(err).To(HaveOccurred())
			})
		})

		When("store API returns error", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[loginResult]{
						Data: loginResult{
							FailureType: "random-error",
						},
					}, nil)
			})

			It("returns error", func() {
				_, err := as.Login(LoginInput{
					Password: testPassword,
				})
				Expect(err).To(HaveOccurred())
			})
		})

		When("store API indicates account is disabled", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[loginResult]{
						Data: loginResult{
							CustomerMessage: CustomerMessageAccountDisabled,
						},
					}, nil)
			})

			It("returns account disabled error", func() {
				_, err := as.Login(LoginInput{
					Password: testPassword,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("account is disabled"))
			})
		})

		When("store API requires 2FA code", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[loginResult]{
						Data: loginResult{
							FailureType:     "",
							CustomerMessage: CustomerMessageBadLogin,
						},
					}, nil)
			})

			It("returns ErrAuthCodeRequired error", func() {
				_, err := as.Login(LoginInput{
					Password: testPassword,
				})
				Expect(err).To(Equal(ErrAuthCodeRequired))
			})
		})

		When("store API redirects", func() {
			const (
				testRedirectLocation = "https://p42-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/authenticate"
			)

			BeforeEach(func() {
				firstCall := mockClient.EXPECT().
					Send(gomock.Any()).
					Do(func(req http.Request) {
						Expect(req.URL).To(Equal(testAuthEndpoint))
						Expect(req.ActionSigner).To(BeIdenticalTo(signer))
						Expect(req.Payload).To(BeAssignableToTypeOf(&http.XMLPayload{}))
						x := req.Payload.(*http.XMLPayload)
						Expect(x.Content).To(HaveKeyWithValue("attempt", "1"))
					}).
					Return(http.Result[loginResult]{
						StatusCode: 302,
						Headers:    map[string]string{"Location": testRedirectLocation},
					}, nil)
				secondCall := mockClient.EXPECT().
					Send(gomock.Any()).
					Do(func(req http.Request) {
						Expect(req.URL).To(Equal(testRedirectLocation))
						Expect(req.ActionSigner).To(BeIdenticalTo(signer))
						Expect(req.Payload).To(BeAssignableToTypeOf(&http.XMLPayload{}))
						x := req.Payload.(*http.XMLPayload)
						Expect(x.Content).To(HaveKeyWithValue("attempt", "1"))
					}).
					Return(http.Result[loginResult]{}, errors.New("test complete"))
				gomock.InOrder(firstCall, secondCall)
			})

			It("follows the redirect while preserving the original request body", func() {
				_, err := as.Login(LoginInput{
					Password: testPassword,
				})
				Expect(err).To(MatchError("request failed: test complete"))
			})
		})

		When("store API redirects outside Apple's authentication pods", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[loginResult]{
						StatusCode: 302,
						Headers:    map[string]string{"Location": "https://example.com/steal"},
					}, nil)
			})

			It("rejects the redirect before reposting credentials", func() {
				_, err := as.Login(LoginInput{Password: testPassword})
				Expect(err).To(MatchError(ContainSubstring("invalid authentication redirect")))
			})
		})

		When("store API returns valid response", func() {
			const (
				testPasswordToken       = "test-password-token"
				testDirectoryServicesID = "directory-services-id"
				testStoreFront          = "test-storefront"
			)

			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[loginResult]{
						StatusCode: 200,
						Headers: map[string]string{
							HTTPHeaderStoreFront: testStoreFront,
							HTTPHeaderPod:        testPod,
						},
						Data: loginResult{
							PasswordToken:       testPasswordToken,
							DirectoryServicesID: testDirectoryServicesID,
							Account: loginAccountResult{
								Email: testEmail,
								Address: loginAddressResult{
									FirstName: testFirstName,
									LastName:  testLastName,
								},
							},
						},
					}, nil)
			})

			When("successfully saves account in keychain", func() {
				BeforeEach(func() {
					mockKeychain.EXPECT().
						Set("account", gomock.Any()).
						Do(func(key string, data []byte) {
							want := Account{
								Name:                fmt.Sprintf("%s %s", testFirstName, testLastName),
								Email:               testEmail,
								PasswordToken:       testPasswordToken,
								Password:            testPassword,
								DirectoryServicesID: testDirectoryServicesID,
								StoreFront:          testStoreFront,
								Pod:                 testPod,
							}

							var got Account
							Expect(json.Unmarshal(data, &got)).To(Succeed())
							Expect(got).To(Equal(want))
						}).
						Return(nil)
				})

				It("returns nil", func() {
					out, err := as.Login(LoginInput{
						Password: testPassword,
					})
					Expect(err).ToNot(HaveOccurred())
					Expect(out.Account.Email).To(Equal(testEmail))
					Expect(out.Account.Name).To(Equal(strings.Join([]string{testFirstName, testLastName}, " ")))
				})

				It("returns the persisted account with a signer cleanup error", func() {
					cleanupErr := errors.New("cleanup failed")
					signer.closeErr = cleanupErr

					out, err := as.Login(LoginInput{Password: testPassword})
					Expect(errors.Is(err, cleanupErr)).To(BeTrue())
					Expect(err.Error()).To(ContainSubstring("failed to close SAP action signer"))
					Expect(out.Account.Email).To(Equal(testEmail))
					Expect(signer.closeCalls).To(Equal(1))
				})
			})
		})
	})
})

type stubActionSigner struct {
	closeCalls int
	closeErr   error
}

func (*stubActionSigner) Sign(data []byte) ([]byte, error) {
	return append([]byte(nil), data...), nil
}

func (s *stubActionSigner) Close() error {
	s.closeCalls++

	return s.closeErr
}
