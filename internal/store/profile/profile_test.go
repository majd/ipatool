package profile

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStoreProfile(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "Store Profile") }

func fixtureArtifact(data []byte) artifact {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	Expect(err).NotTo(HaveOccurred())
	defer encoder.Close()

	return artifact{Name: "fixture", Size: len(data), SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Encoded: base64.StdEncoding.EncodeToString(encoder.EncodeAll(data, nil))}
}

var _ = Describe("Compiled Store profile", func() {
	It("loads and validates the complete committed profile without setup", func() {
		data, err := Load(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(validateData(data)).To(Succeed())
		Expect(data.Metadata).To(HaveLen(generatedMetadata.Size))
		Expect(data.Pages).To(HaveLen(generatedPages.Size))
		Expect(data.Plugin).To(HaveLen(generatedPlugin.Size))
	})

	It("honors cancellation before decoding", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Load(ctx)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	})

	It("rejects a nil context", func() {
		_, err := Load(nil) //nolint:staticcheck // Verify rejection of invalid caller input.
		Expect(err).To(MatchError(ContainSubstring("context is nil")))
	})
})

var _ = Describe("Compiled artifact integrity", func() {
	It("decodes binary data and rejects a mismatched digest", func() {
		original := []byte{0, 1, 2, 0xff, '\n', '`'}
		item := fixtureArtifact(original)
		actual, err := decodeArtifact(context.Background(), item, 1<<20)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(original))
		item.SHA256 = "invalid"
		_, err = decodeArtifact(context.Background(), item, 1<<20)
		Expect(err).To(MatchError(ContainSubstring("integrity verification")))
	})

	DescribeTable("rejects incorrect decoded sizes", func(delta int) {
		item := fixtureArtifact([]byte("encoded data"))
		item.Size += delta
		_, err := decodeArtifact(context.Background(), item, 1<<20)
		Expect(err).To(MatchError(ContainSubstring("decoded size")))
	}, Entry("too short", -1), Entry("too long", 1))

	It("rejects an artifact above its allowed limit before decoding", func() {
		_, err := decodeArtifact(context.Background(), artifact{Name: "fixture", Size: 100}, 99)
		Expect(err).To(MatchError(ContainSubstring("size")))
	})

	It("rejects corrupt compressed data", func() {
		item := fixtureArtifact([]byte("encoded data"))
		item.Encoded = base64.StdEncoding.EncodeToString([]byte("invalid frame"))
		_, err := decodeArtifact(context.Background(), item, 1<<20)
		Expect(err).To(HaveOccurred())
	})

	It("propagates cancellation through the decoder", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := decodeArtifact(ctx, fixtureArtifact([]byte("encoded data")), 1<<20)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	})
})

var _ = Describe("Profile loading concurrency", func() {
	It("shares one successful decode and lets a waiter cancel independently", func() {
		var calls atomic.Int32
		entered, release := make(chan struct{}), make(chan struct{})
		data := Data{Metadata: []byte("ready")}
		instance := &loader{gate: make(chan struct{}, 1), decode: func(context.Context) (Data, error) {
			calls.Add(1)
			close(entered)
			<-release

			return data, nil
		}}
		finished := make(chan error, 1)
		go func() { _, err := instance.load(context.Background()); finished <- err }()
		Eventually(entered).Should(BeClosed())
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := instance.load(ctx)
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
		close(release)
		Eventually(finished).Should(Receive(BeNil()))
		actual, err := instance.load(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(data))
		Expect(calls.Load()).To(Equal(int32(1)))
	})

	It("does not cache a decode canceled before publication", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		instance := &loader{gate: make(chan struct{}, 1), decode: func(context.Context) (Data, error) {
			calls++
			cancel()

			return Data{Metadata: []byte("ready")}, nil
		}}
		_, err := instance.load(ctx)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(instance.installed).To(BeNil())
		_, err = instance.load(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(2))
	})
})
