//go:build !windows

package unicorn

func configureEngine(*Engine) error {
	return nil
}
