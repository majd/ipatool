package appstore

import (
	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Phone account login routing", func() {
	DescribeTable("sets a storefront hint without rewriting the Apple ID", func(appleID, storefront string) {
		request := (&appstore{}).loginRequest(appleID, "password", "", "guid", testAuthEndpoint, 1, &stubActionSigner{})

		Expect(request.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("appleId", appleID))
		if storefront == "" {
			Expect(request.Headers).NotTo(HaveKey("X-Apple-Store-Front"))
		} else {
			Expect(request.Headers).To(HaveKeyWithValue("X-Apple-Store-Front", storefront))
		}
	},
		Entry("Chinese national number", "13912345678", "143465"),
		Entry("Chinese number with a plus prefix", "+8613912345678", "143465"),
		Entry("Chinese number with a 00 prefix", "008613912345678", "143465"),
		Entry("Chinese number with a bare country code", "8613912345678", "143465"),
		Entry("Chinese number with spaces and hyphens", " +86 139-1234-5678 ", "143465"),
		Entry("Indian national number beginning with 91", "9123456789", "143467"),
		Entry("Indian national number beginning with 86", "8612345678", "143467"),
		Entry("Indian national number beginning with 6", "6123456789", "143467"),
		Entry("Indian number with a trunk prefix", "09123456789", "143467"),
		Entry("Indian number with a plus prefix", "+919123456789", "143467"),
		Entry("Indian number with a 00 prefix", "00919123456789", "143467"),
		Entry("Indian number with a bare country code", "919123456789", "143467"),
		Entry("Indian number with spaces and hyphens", "+91 91234-56789", "143467"),
		Entry("email", "person@example.test", ""),
		Entry("numeric email", "13912345678@example.test", ""),
		Entry("legacy username", "some-account", ""),
		Entry("empty input", "", ""),
		Entry("formatting only", " -- ", ""),
		Entry("plus only", "+", ""),
		Entry("international access code only", "00", ""),
		Entry("letters within a number", "139x12345678", ""),
		Entry("unsupported punctuation", "139/1234/5678", ""),
		Entry("non-ASCII digits", "+８６１３９１２３４５６７８", ""),
		Entry("non-ASCII plus", "＋8613912345678", ""),
		Entry("embedded control character", "139\n12345678", ""),
		Entry("misplaced plus", "139+12345678", ""),
		Entry("repeated plus", "++8613912345678", ""),
		Entry("mixed international prefixes", "+008613912345678", ""),
		Entry("unsupported country with a Chinese-looking local format", "+13912345678", ""),
		Entry("unsupported country with a 00 prefix", "0013912345678", ""),
		Entry("unsupported country with an Indian-looking local format", "+8612345678", ""),
		Entry("explicit country code missing national digits", "+9123456789", ""),
		Entry("short Chinese number", "+861391234567", ""),
		Entry("long Chinese number", "+86139123456789", ""),
		Entry("Chinese country code with an Indian trunk number", "8609123456789", ""),
		Entry("invalid Chinese national prefix", "+8623912345678", ""),
		Entry("short Indian number", "+91912345678", ""),
		Entry("long Indian number", "+9191234567890", ""),
		Entry("invalid Indian national prefix", "+915123456789", ""),
		Entry("trunk prefix after an international code", "+9109123456789", ""),
	)
})
