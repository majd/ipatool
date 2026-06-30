//go:build integration

package appstore

import (
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	pkghttp "github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
)

type integrationCookieJar struct {
	jar *cookiejar.Jar
}

func (c integrationCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	c.jar.SetCookies(u, cookies)
}

func (c integrationCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return c.jar.Cookies(u)
}

func (integrationCookieJar) Save() error { return nil }

func newIntegrationAppStore(t *testing.T) AppStore {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	return NewAppStore(Args{
		CookieJar:       integrationCookieJar{jar: jar},
		OperatingSystem: operatingsystem.New(),
		Machine:         machine.New(machine.Args{OS: operatingsystem.New()}),
		Keychain:        keychain.New(keychain.Args{}),
	})
}

func TestLiveBagReturnsNativeAuthEndpoint(t *testing.T) {
	as := newIntegrationAppStore(t)

	out, err := as.Bag(BagInput{})
	if err != nil {
		t.Fatalf("Bag: %v", err)
	}

	if out.AuthEndpoint != defaultNativeAuthEndpoint {
		t.Fatalf("AuthEndpoint = %q, want %q", out.AuthEndpoint, defaultNativeAuthEndpoint)
	}
}

func TestLiveLoginUsesNativeAuthEndpoint(t *testing.T) {
	as := newIntegrationAppStore(t)

	bag, err := as.Bag(BagInput{})
	if err != nil {
		t.Fatalf("Bag: %v", err)
	}

	_, err = as.Login(LoginInput{
		Email:    "ipatool-integration-test@example.com",
		Password: "not-a-real-password",
		Endpoint: bag.AuthEndpoint,
	})
	if err == nil {
		t.Fatal("expected login to fail with invalid credentials")
	}

	var decodeErr *pkghttp.ResponseDecodeError
	if errors.As(err, &decodeErr) {
		t.Fatalf("login returned plist decode error (wrong endpoint or non-plist body): %v", err)
	}

	if strings.Contains(err.Error(), "failed to unmarshal xml") {
		t.Fatalf("login hit legacy plist parse failure: %v", err)
	}

	t.Logf("login failed as expected with invalid credentials: %v", err)
}
