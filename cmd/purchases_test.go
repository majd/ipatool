package cmd

import (
	"github.com/majd/ipatool/v2/pkg/appstore"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("List Purchases command", func() {
	It("uses the pagination defaults", func() {
		cmd := listPurchasesCmd()

		page, err := cmd.Flags().GetInt("page")
		Expect(err).ToNot(HaveOccurred())
		Expect(page).To(Equal(1))

		maxResults, err := cmd.Flags().GetInt("max-results")
		Expect(err).ToNot(HaveOccurred())
		Expect(maxResults).To(Equal(appstore.DefaultOwnedAppsLimit))
	})

	DescribeTable("rejects invalid pagination",
		func(flag, value, errorText string) {
			cmd := listPurchasesCmd()
			Expect(cmd.Flags().Set(flag, value)).To(Succeed())

			err := cmd.PreRunE(cmd, nil)
			Expect(err).To(MatchError(ContainSubstring(errorText)))
		},
		Entry("page below one", "page", "0", "page"),
		Entry("max results below one", "max-results", "0", "max results"),
		Entry("max results over limit", "max-results", "101", "100"),
	)

	It("is registered on the root command", func() {
		cmd, _, err := rootCmd().Find([]string{"list-purchases"})

		Expect(err).ToNot(HaveOccurred())
		Expect(cmd.Name()).To(Equal("list-purchases"))
	})
})
