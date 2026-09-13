package cmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/majd/ipatool/v2/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Login command", func() {
	var store *fakeLoginAppStore

	BeforeEach(func() {
		previousDependencies := dependencies
		DeferCleanup(func() { dependencies = previousDependencies })
		store = &fakeLoginAppStore{}
		dependencies.AppStore = store
		dependencies.Logger = log.NewLogger(log.Args{})
	})

	DescribeTable("requires credentials in non-interactive mode", func(args []string, message string) {
		cmd := loginCmd()
		cmd.SetContext(context.WithValue(context.Background(), interactiveKey, false))
		cmd.SetArgs(args)

		Expect(cmd.Execute()).To(MatchError(message))
		Expect(store.loginCalls).To(BeZero())
	},
		Entry("missing email", []string{"--password", "secret"}, "email is required when not running in interactive mode; use the \"--email\" flag"),
		Entry("missing password", []string{"--email", "user@example.com"}, "password is required when not running in interactive mode; use the \"--password\" flag"),
	)

	DescribeTable("uses supplied credentials", func(interactive bool) {
		cmd := loginCmd()
		cmd.SetContext(context.WithValue(context.Background(), interactiveKey, interactive))
		cmd.SetArgs([]string{"--email", "user@example.com", "--password", "secret"})

		Expect(cmd.Execute()).To(Succeed())
		Expect(store.loginCalls).To(Equal(1))
		Expect(store.input).To(Equal(appstore.LoginInput{Email: "user@example.com", Password: "secret"}))
	},
		Entry("interactive", true),
		Entry("non-interactive", false),
	)

	DescribeTable("prompts for email", func(input, message string) {
		dir := GinkgoT().TempDir()
		inputPath := filepath.Join(dir, "stdin")
		Expect(os.WriteFile(inputPath, []byte(input), 0o600)).To(Succeed())
		stdin, err := os.Open(inputPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(stdin.Close)
		stderr, err := os.Create(filepath.Join(dir, "stderr"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(stderr.Close)

		previousStdin, previousStderr := os.Stdin, os.Stderr
		os.Stdin, os.Stderr = stdin, stderr
		DeferCleanup(func() { os.Stdin, os.Stderr = previousStdin, previousStderr })

		cmd := loginCmd()
		cmd.SetContext(context.WithValue(context.Background(), interactiveKey, true))
		cmd.SetArgs([]string{"--password", "secret"})
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true

		err = cmd.Execute()
		if message == "" {
			Expect(err).NotTo(HaveOccurred())
			Expect(store.loginCalls).To(Equal(1))
			Expect(store.input).To(Equal(appstore.LoginInput{Email: "user@example.com", Password: "secret"}))
		} else {
			Expect(err).To(MatchError(message))
			Expect(store.loginCalls).To(BeZero())
		}

		prompt, err := os.ReadFile(stderr.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(string(prompt)).To(Equal("enter email: "))
	},
		Entry("reads unmasked input without requiring a terminal", "user@example.com\n", ""),
		Entry("reports input errors", "", "failed to read email: failed to read input: EOF"),
	)
})

type fakeLoginAppStore struct {
	appstore.AppStore
	input      appstore.LoginInput
	loginCalls int
}

func (f *fakeLoginAppStore) Login(input appstore.LoginInput) (appstore.LoginOutput, error) {
	f.input = input
	f.loginCalls++

	return appstore.LoginOutput{}, nil
}
