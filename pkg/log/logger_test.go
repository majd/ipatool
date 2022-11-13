package log

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/golang/mock/gomock"
	"github.com/rs/zerolog"
	"os"
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
		logger = NewLogger()
		logger.Update(NewLogger().Output(mockWriter))
	})

	It("returns a copy of the logger", func() {
		out := logger.Output(os.Stdout)
		Expect(out).ToNot(BeNil())
	})

	When("writing logs", func() {
		It("logs debug level", func() {
			mockWriter.EXPECT().
				WriteLevel(zerolog.DebugLevel, gomock.Any()).
				Do(func(level zerolog.Level, p []byte) {
					Expect(p).To(ContainSubstring("debug"))
				}).
				Return(0, nil)

			logger.Debug().Msg("debug")
		})

		It("logs info level", func() {
			mockWriter.EXPECT().
				WriteLevel(zerolog.InfoLevel, gomock.Any()).
				Do(func(level zerolog.Level, p []byte) {
					Expect(p).To(ContainSubstring("info"))
				}).
				Return(0, nil)

			logger.Info().Msg("info")
		})

		It("logs warn level", func() {
			mockWriter.EXPECT().
				WriteLevel(zerolog.WarnLevel, gomock.Any()).
				Do(func(level zerolog.Level, p []byte) {
					Expect(p).To(ContainSubstring("warn"))
				}).
				Return(0, nil)

			logger.Warn().Msg("warn")
		})

		It("logs error level", func() {
			mockWriter.EXPECT().
				WriteLevel(zerolog.ErrorLevel, gomock.Any()).
				Do(func(level zerolog.Level, p []byte) {
					Expect(p).To(ContainSubstring("error"))
				}).
				Return(0, nil)

			logger.Error().Msg("error")
		})
	})

	When("passing info log level", func() {
		It("returns correct mapping", func() {
			out, err := logger.LevelFromString(InfoLevel)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal(zerolog.InfoLevel))
		})
	})

	When("passing info debug level", func() {
		It("returns correct mapping", func() {
			out, err := logger.LevelFromString(DebugLevel)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal(zerolog.DebugLevel))
		})
	})

	When("passing warn log level", func() {
		It("returns correct mapping", func() {
			out, err := logger.LevelFromString(WarnLevel)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal(zerolog.WarnLevel))
		})
	})

	When("passing error log level", func() {
		It("returns correct mapping", func() {
			out, err := logger.LevelFromString(ErrorLevel)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal(zerolog.ErrorLevel))
		})
	})

	When("passing invalid log level", func() {
		It("returns correct mapping", func() {
			_, err := logger.LevelFromString("?")
			Expect(err).To(MatchError("invalid log level"))
		})
	})
})
