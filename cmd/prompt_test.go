package cmd

import (
	"bytes"
	"io"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Masked input", func() {
	DescribeTable("echoes masks while preserving the entered password", func(input, password, echo string) {
		var output bytes.Buffer
		value, err := readMaskedInput(strings.NewReader(input), &output)

		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal(password))
		Expect(output.String()).To(Equal(echo))
	},
		Entry("enter", "secret\r", "secret", "******"),
		Entry("newline", "secret\n", "secret", "******"),
		Entry("unicode", "pä密🔑\r", "pä密🔑", "****"),
		Entry("delete", "ab\x7fc\r", "ac", "**\b \b*"),
		Entry("unicode backspace", "a密\bc\r", "ac", "**\b \b*"),
		Entry("backspace on empty input", "\b\x7fa\r", "a", "*"),
		Entry("clear line", "ab\x15c\r", "c", "**\b \b\b \b*"),
		Entry("arrow keys", "ab\x1b[D\x1bOCc\r", "abc", "***"),
		Entry("empty input", "\r", "", ""),
	)

	It("cancels without returning the password on Ctrl-C", func() {
		value, err := readMaskedInput(strings.NewReader("secret\x03"), io.Discard)

		Expect(err).To(MatchError("input interrupted"))
		Expect(value).To(BeEmpty())
	})

	DescribeTable("discards incomplete input", func(input string) {
		value, err := readMaskedInput(strings.NewReader(input), io.Discard)

		Expect(err).To(MatchError(ContainSubstring("EOF")))
		Expect(value).To(BeEmpty())
	},
		Entry("end of stream", "secret"),
		Entry("Ctrl-D", "secret\x04"),
	)
})
