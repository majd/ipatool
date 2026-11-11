package assets

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Verified bundle cache", func() {
	It("shares one successful load across concurrent callers", func() {
		var calls atomic.Int32
		entered, release := make(chan struct{}), make(chan struct{})
		data := []byte("verified bundle")
		loader := bundleLoader{gate: make(chan struct{}, 1), resolve: func(context.Context) (Bundle, error) {
			calls.Add(1)
			close(entered)
			<-release

			return Bundle{CoreFP: data}, nil
		}}
		var workers sync.WaitGroup
		results := make(chan Bundle, 8)
		failures := make(chan error, 8)
		for range 8 {
			workers.Go(func() {
				bundle, err := loader.load(context.Background())
				results <- bundle
				failures <- err
			})
		}
		Eventually(entered).Should(BeClosed())
		close(release)
		workers.Wait()
		Expect(calls.Load()).To(Equal(int32(1)))
		for range 8 {
			Expect(<-failures).NotTo(HaveOccurred())
			Expect(&(<-results).CoreFP[0]).To(BeIdenticalTo(&data[0]))
		}
	})

	It("lets a waiting caller cancel without interrupting the load", func() {
		entered, release := make(chan struct{}), make(chan struct{})
		loader := bundleLoader{gate: make(chan struct{}, 1), resolve: func(context.Context) (Bundle, error) {
			close(entered)
			<-release

			return Bundle{CoreFP: []byte("ready")}, nil
		}}
		done := make(chan error, 1)
		go func() { _, err := loader.load(context.Background()); done <- err }()
		Eventually(entered).Should(BeClosed())
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := loader.load(ctx)
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
		close(release)
		Eventually(done).Should(Receive(BeNil()))
		bundle, err := loader.load(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(string(bundle.CoreFP)).To(Equal("ready"))
	})

	It("retries failed loads", func() {
		calls := 0
		failed := errors.New("temporary failure")
		loader := bundleLoader{gate: make(chan struct{}, 1), resolve: func(context.Context) (Bundle, error) {
			calls++
			if calls == 1 {
				return Bundle{}, failed
			}

			return Bundle{CoreFP: []byte("ready")}, nil
		}}
		_, err := loader.load(context.Background())
		Expect(err).To(MatchError(failed))
		bundle, err := loader.load(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(string(bundle.CoreFP)).To(Equal("ready"))
		Expect(calls).To(Equal(2))
	})

	It("does not publish a load canceled before completion", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		loader := bundleLoader{gate: make(chan struct{}, 1), resolve: func(context.Context) (Bundle, error) {
			calls++
			cancel()

			return Bundle{CoreFP: []byte("ready")}, nil
		}}
		_, err := loader.load(ctx)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(loader.installed).To(BeNil())
		_, err = loader.load(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(2))
	})

	It("rejects nil and canceled contexts even after a successful load", func() {
		calls := 0
		loader := bundleLoader{gate: make(chan struct{}, 1), resolve: func(context.Context) (Bundle, error) {
			calls++

			return Bundle{}, nil
		}}
		_, err := loader.load(nil) //nolint:staticcheck // Verify invalid caller input.
		Expect(err).To(MatchError(ContainSubstring("context is nil")))
		_, err = loader.load(context.Background())
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = loader.load(ctx)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(calls).To(Equal(1))
	})
})
