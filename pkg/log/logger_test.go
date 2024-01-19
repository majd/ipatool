package log

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/rs/zerolog"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Logger", func() {
	var (
		ctrl       *gomock.Controller
		mockWriter *MockWriter
		logger     Logger
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockWriter = NewMockWriter(ctrl)
	})

	Context("Verbose logger", func() {
		BeforeEach(func() {
			logger = NewLogger(Args{
				Verbose: true,
				Writer:  mockWriter,
			})
		})

		When("logging with verbose level", func() {
			It("writes output", func() {
				mockWriter.EXPECT().
					WriteLevel(zerolog.DebugLevel, gomock.Any()).
					Do(func(level zerolog.Level, p []byte) {
						Expect(p).To(ContainSubstring("\"message\":\"verbose\""))
					}).
					Return(0, nil)

				logger.Verbose().Msg("verbose")
			})
		})
	})

	Context("Non-verbose logger", func() {
		BeforeEach(func() {
			logger = NewLogger(Args{
				Verbose: false,
				Writer:  mockWriter,
			})
		})

		When("logging messsage", func() {
			It("writes output", func() {
				mockWriter.EXPECT().
					WriteLevel(zerolog.InfoLevel, gomock.Any()).
					Do(func(level zerolog.Level, p []byte) {
						Expect(p).To(ContainSubstring("\"message\":\"info\""))
					}).
					Return(0, nil)

				logger.Log().Msg("info")
			})
		})

		When("logging error", func() {
			It("writes output", func() {
				mockWriter.EXPECT().
					WriteLevel(zerolog.ErrorLevel, gomock.Any()).
					Do(func(level zerolog.Level, p []byte) {
						Expect(p).To(ContainSubstring("\"message\":\"error\""))
					}).
					Return(0, nil)

				logger.Error().Msg("error")
			})
		})

		When("logging with verbose level", func() {
			It("returns nil", func() {
				res := logger.Verbose()
				Expect(res).To(BeNil())
			})
		})
	})
})
