package http

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Payload", func() {
	var sut Payload

	Context("URL Payload", func() {
		It("returns encoded URL data", func() {
			sut = &URLPayload{
				Content: map[string]interface{}{
					"foo": "bar",
					"num": 3,
				},
			}

			data, err := sut.data()
			Expect(err).ToNot(HaveOccurred())
			Expect(data).To(Equal([]byte("foo=bar&num=3")))
		})

		It("returns error if URL data is invalid", func() {
			sut = &URLPayload{
				Content: map[string]interface{}{
					"foo": func() {},
				},
			}

			data, err := sut.data()
			Expect(err).To(HaveOccurred())
			Expect(data).To(BeNil())
		})
	})

	Context("XML Payload", func() {
		It("returns encoded XML data", func() {
			sut = &XMLPayload{
				Content: map[string]interface{}{
					"foo":   "bar",
					"lorem": "ipsum",
				},
			}

			data, err := sut.data()
			Expect(err).ToNot(HaveOccurred())
			Expect(data).To(ContainSubstring("<dict><key>foo</key><string>bar</string><key>lorem</key><string>ipsum</string></dict>"))
		})

		It("returns error if XML data is invalid", func() {
			sut = &XMLPayload{
				Content: map[string]interface{}{
					"foo": func() {},
				},
			}

			data, err := sut.data()
			Expect(err).To(HaveOccurred())
			Expect(data).To(BeNil())
		})
	})
})
