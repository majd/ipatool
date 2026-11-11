package emulator

import (
	"errors"
	"fmt"
)

type profileMemory interface{ MemWrite(uint64, []byte) error }

// Batch consecutive guest pages whose source bytes are also consecutive. Gaps
// remain separate writes, so strict profile validation covers the same pages.
func writeProfilePages(memory profileMemory, pages []Page, data []byte) error {
	for first := 0; first < len(pages); {
		page := pages[first]
		if page.Offset > uint64(len(data)) || profilePageSize > uint64(len(data))-page.Offset {
			return errors.New("profile page exceeds data")
		}

		last := first + 1
		for last < len(pages) {
			next, previous := pages[last], pages[last-1]
			if previous.Address > ^uint64(0)-profilePageSize || next.Address != previous.Address+profilePageSize || next.Offset != previous.Offset+profilePageSize {
				break
			}

			if next.Offset > uint64(len(data)) || profilePageSize > uint64(len(data))-next.Offset {
				return errors.New("profile page exceeds data")
			}

			last++
		}

		end := page.Offset + uint64(last-first)*profilePageSize
		if err := memory.MemWrite(page.Address, data[page.Offset:end]); err != nil {
			return fmt.Errorf("write Store profile pages: %w", err)
		}

		first = last
	}

	return nil
}
