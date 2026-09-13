package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"golang.org/x/term"
)

func readPrompt(prompt string, masked bool) (string, error) {
	if masked {
		state, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return "", fmt.Errorf("failed to configure terminal: %w", err)
		}
		defer term.Restore(int(os.Stdin.Fd()), state) //nolint:errcheck
	}

	// Prompts belong on stderr so structured output on stdout stays valid.
	if _, err := io.WriteString(os.Stderr, prompt); err != nil {
		return "", fmt.Errorf("failed to write prompt: %w", err)
	}

	if masked {
		defer fmt.Fprint(os.Stderr, "\r\n")

		return readMaskedInput(os.Stdin, os.Stderr)
	}

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func readMaskedInput(input io.Reader, output io.Writer) (string, error) {
	reader := bufio.NewReader(input)

	var (
		password []rune
		escape   int
	)

	for {
		key, _, err := reader.ReadRune()
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		// Ignore terminal escape sequences (for example, arrow keys).
		if escape != 0 {
			if escape == 1 && (key == '[' || key == 'O') {
				escape = 2
			} else if escape == 1 || (key >= 0x40 && key <= 0x7e) {
				escape = 0
			}

			continue
		}

		var echo string

		switch key {
		case '\r', '\n':
			return string(password), nil
		case 3:
			return "", errors.New("input interrupted")
		case 4:
			return "", io.EOF
		case 27:
			escape = 1
		case '\b', 127:
			if len(password) > 0 {
				password = password[:len(password)-1]
				echo = "\b \b"
			}
		case 21:
			echo = strings.Repeat("\b \b", len(password))
			password = password[:0]
		default:
			if unicode.IsPrint(key) {
				password = append(password, key)
				echo = "*"
			}
		}

		if _, err := io.WriteString(output, echo); err != nil {
			return "", fmt.Errorf("failed to echo input: %w", err)
		}
	}
}
