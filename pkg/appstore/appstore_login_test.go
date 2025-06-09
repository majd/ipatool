package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	)

	var (
		ctrl         *gomock.Controller
		as           AppStore
		mockKeychain *keychain.MockKeychain
		mockClient   *http.MockClient[loginResult]
		mockMachine  *machine.MockMachine
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockClient = http.NewMockClient[loginResult](ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		as = &appstore{
			keychain:    mockKeychain,
			loginClient: mockClient,
			machine:     mockMachine,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
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
		})

		When("client returns error", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[loginResult]{}, errors.New(""))
			})

			It("returns wrapped error", func() {
				_, err := as.Login(LoginInput{
					Password: testPassword,
				})
				Expect(err).To(HaveOccurred())
			})
		})

		When("store API returns invalid first response", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[loginResult]{
						Data: loginResult{
							FailureType:     FailureTypeInvalidCredentials,
							CustomerMessage: "test",
						},
					}, nil).
					Times(2)
			})

			It("retries one more time", func() {
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
				testRedirectLocation = "https://" + PrivateAppStoreAPIDomain + PrivateAppStoreAPIPathAuthenticate + "?PRH=31&Pod=31"
			)

			BeforeEach(func() {
				firstCall := mockClient.EXPECT().
					Send(gomock.Any()).
					Do(func(req http.Request) {
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
						Expect(req.Payload).To(BeAssignableToTypeOf(&http.XMLPayload{}))
						x := req.Payload.(*http.XMLPayload)
						Expect(x.Content).To(HaveKeyWithValue("attempt", "2"))
					}).
					Return(http.Result[loginResult]{}, errors.New("test complete"))
				gomock.InOrder(firstCall, secondCall)
			})

			It("follows the redirect and increments attempt", func() {
				_, err := as.Login(LoginInput{
					Password: testPassword,
				})
				Expect(err).To(MatchError("request failed: test complete"))
			})
		})

		When("store API redirects too much", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[loginResult]{
						StatusCode: 302,
						Headers:    map[string]string{"Location": "hello"},
					}, nil).
					Times(4)
			})
			It("bails out", func() {
				_, err := as.Login(LoginInput{
					Password: testPassword,
				})
				Expect(err).To(MatchError("too many attempts"))
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
						Headers:    map[string]string{HTTPHeaderStoreFront: testStoreFront},
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

			When("fails to save account in keychain", func() {
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
							}

							var got Account
							err := json.Unmarshal(data, &got)
							Expect(err).ToNot(HaveOccurred())
							Expect(got).To(Equal(want))
						}).
						Return(errors.New(""))
				})

				It("returns error", func() {
					_, err := as.Login(LoginInput{
						Password: testPassword,
					})
					Expect(err).To(HaveOccurred())
				})
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
							}

							var got Account
							err := json.Unmarshal(data, &got)
							Expect(err).ToNot(HaveOccurred())
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
			})
		})
	})
})
