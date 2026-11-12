package unicorn

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Unicorn clock initialization", func() {
	It("waits for every active emulation and permits nested calls while waiting", func() {
		var clock clockState

		firstDone := clock.beginEmulation()
		secondDone := clock.beginEmulation()
		initialized := make(chan struct{})
		result := make(chan error, 1)

		go func() {
			result <- clock.initialize(context.Background(), func() error {
				close(initialized)

				return nil
			})
		}()

		Consistently(initialized, 20*time.Millisecond).ShouldNot(BeClosed())
		firstDone()
		Consistently(initialized, 20*time.Millisecond).ShouldNot(BeClosed())

		nestedDone := clock.beginEmulation()
		secondDone()
		Consistently(initialized, 20*time.Millisecond).ShouldNot(BeClosed())
		nestedDone()
		Eventually(initialized).Should(BeClosed())

		var err error

		Eventually(result).Should(Receive(&err))
		Expect(err).NotTo(HaveOccurred())
	})

	It("waits for initialization before starting emulation", func() {
		var clock clockState

		initializing := make(chan struct{})
		release := make(chan struct{})
		result := make(chan error, 1)
		started := make(chan func(), 1)

		go func() {
			result <- clock.initialize(context.Background(), func() error {
				close(initializing)
				<-release

				return nil
			})
		}()

		Eventually(initializing).Should(BeClosed())

		go func() { started <- clock.beginEmulation() }()

		Consistently(started, 20*time.Millisecond).ShouldNot(Receive())
		close(release)

		var done func()

		Eventually(started).Should(Receive(&done))
		done()

		var err error

		Eventually(result).Should(Receive(&err))
		Expect(err).NotTo(HaveOccurred())
	})

	It("cancels initialization while another engine is executing", func() {
		var clock clockState

		done := clock.beginEmulation()
		defer done()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		initialized := make(chan struct{})
		result := make(chan error, 1)

		go func() {
			result <- clock.initialize(ctx, func() error {
				close(initialized)

				return nil
			})
		}()

		Consistently(result, 20*time.Millisecond).ShouldNot(Receive())
		cancel()

		var err error

		Eventually(result).Should(Receive(&err))
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(initialized).NotTo(BeClosed())
	})

	It("releases the clock after initialization fails", func() {
		var clock clockState

		failure := errors.New("initialization failed")
		Expect(clock.initialize(context.Background(), func() error { return failure })).To(MatchError(failure))
		Expect(clock.initialize(context.Background(), func() error { return nil })).To(Succeed())
		done := clock.beginEmulation()
		done()
	})
})
