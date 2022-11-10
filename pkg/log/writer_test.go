package log

import (
	"github.com/golang/mock/gomock"
	"github.com/majd/ipatool/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
)

var _ = Describe("Writer", func() {
	var (
		ctrl             *gomock.Controller
		mockStdoutWriter *mocks.MockWriter
		mockStderrWriter *mocks.MockWriter
		sut              *writer
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockStdoutWriter = mocks.NewMockWriter(ctrl)
		mockStderrWriter = mocks.NewMockWriter(ctrl)
		sut = &writer{
			stdOutWriter: mockStdoutWriter,
			stdErrWriter: mockStderrWriter,
		}
	})

	It("returns valid writer", func() {
		out := NewWriter()
		Expect(out).ToNot(BeNil())
	})

	When("writing logs", func() {
		It("writes debug logs to stdout", func() {
			mockStdoutWriter.EXPECT().Write([]byte("debug")).Return(0, nil)

			_, err := sut.WriteLevel(zerolog.DebugLevel, []byte("debug"))
			Expect(err).ToNot(HaveOccurred())
		})

		It("writes info logs to stdout", func() {
			mockStdoutWriter.EXPECT().Write([]byte("info")).Return(0, nil).Times(2)

			_, err := sut.Write([]byte("info"))
			Expect(err).ToNot(HaveOccurred())

			_, err = sut.WriteLevel(zerolog.InfoLevel, []byte("info"))
			Expect(err).ToNot(HaveOccurred())
		})

		It("writes warn logs to stdout", func() {
			mockStdoutWriter.EXPECT().Write([]byte("warning")).Return(0, nil)

			_, err := sut.WriteLevel(zerolog.WarnLevel, []byte("warning"))
			Expect(err).ToNot(HaveOccurred())
		})

		It("writes error logs to stderr", func() {
			mockStderrWriter.EXPECT().Write([]byte("error")).Return(0, nil)

			_, err := sut.WriteLevel(zerolog.ErrorLevel, []byte("error"))
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("log level is not supported", func() {
		It("returns the length of the passed log", func() {
			length, err := sut.WriteLevel(zerolog.PanicLevel, []byte("panic"))
			Expect(err).ToNot(HaveOccurred())
			Expect(length).To(Equal(5))
		})
	})
})
