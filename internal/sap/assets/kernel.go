package assets

import (
	"archive/zip"
	"bytes"
	"encoding/asn1"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/blacktop/go-macho"
	"github.com/blacktop/go-macho/pkg/fixupchains"
	"github.com/blacktop/go-macho/types"
	"github.com/go-compressions/lzfse"
)

const (
	kernelCacheName           = "kernelcache.release.mac13j"
	kernelCacheCompressedSize = 33043908
	kernelCacheArchiveSize    = 33096787
	kernelCachePayloadSize    = 33096384
	kernelCacheImageSize      = 124354560
	fairPlayKextIdentifier    = "com.apple.driver.FairPlayIOKit"
	maximumKernelCacheSymbols = 1 << 20
)

func extractFairPlayKext(archive *zip.Reader, destination string) error {
	entry, err := pinnedZipEntry(archive, kernelCacheName, kernelCacheCompressedSize, kernelCacheArchiveSize)
	if err != nil {
		return err
	}

	reader, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open pinned kernel collection: %w", err)
	}

	data, readErr := io.ReadAll(io.LimitReader(reader, kernelCacheArchiveSize+1))
	closeErr := reader.Close()

	if readErr != nil || closeErr != nil {
		return fmt.Errorf("read pinned kernel collection: %w", errors.Join(readErr, closeErr))
	}

	if len(data) != kernelCacheArchiveSize {
		return fmt.Errorf("pinned kernel collection has size %d, expected %d", len(data), kernelCacheArchiveSize)
	}

	payload, err := im4pPayload(data)
	if err != nil {
		return fmt.Errorf("decode pinned kernel collection: %w", err)
	}

	if len(payload) != kernelCachePayloadSize {
		return fmt.Errorf("pinned kernel payload has size %d, expected %d", len(payload), kernelCachePayloadSize)
	}

	image, err := lzfse.Decompress(payload)
	if err != nil {
		return fmt.Errorf("decompress pinned kernel collection: %w", err)
	}

	if len(image) != kernelCacheImageSize {
		return fmt.Errorf("pinned kernel image has size %d, expected %d", len(image), kernelCacheImageSize)
	}

	collection, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		return fmt.Errorf("open pinned kernel collection Mach-O: %w", err)
	}

	defer collection.Close()

	if collection.Type != types.MH_FILESET {
		return errors.New("pinned kernel collection is not a fileset")
	}

	var fixups *fixupchains.DyldChainedFixups
	if collection.HasFixups() {
		fixups, err = collection.DyldChainedFixups()
		if err != nil {
			return fmt.Errorf("read pinned kernel fixups: %w", err)
		}
	}

	symbols := make(map[string]macho.Symbol)

	for _, entry := range collection.FileSets() {
		member, openErr := collection.GetFileSetFileByName(entry.EntryID)
		if openErr != nil || member.Symtab == nil {
			continue
		}

		for _, symbol := range member.Symtab.Syms {
			if symbol.Type.IsExternalSym() && !symbol.Type.IsUndefinedSym() && symbol.Value != 0 {
				symbols[symbol.Name] = symbol
				if len(symbols) > maximumKernelCacheSymbols {
					return errors.New("pinned kernel symbol table exceeds the limit")
				}
			}
		}
	}

	member, err := collection.GetFileSetFileByName(fairPlayKextIdentifier)
	if err != nil {
		return fmt.Errorf("find FairPlayIOKit in pinned kernel collection: %w", err)
	}

	var resolved []macho.Symbol

	if member.Symtab != nil {
		for _, symbol := range member.Symtab.Syms {
			if !symbol.Type.IsUndefinedSym() || symbol.Name == "" {
				continue
			}

			if target, ok := symbols[symbol.Name]; ok {
				resolved = append(resolved, macho.Symbol{
					Name:  symbol.Name,
					Type:  types.N_ABS | types.N_EXT,
					Desc:  symbol.Desc,
					Value: target.Value,
				})
			}
		}
	}

	if err := member.Export(destination, fixups, collection.GetBaseAddress(), resolved); err != nil {
		return fmt.Errorf("export FairPlayIOKit from pinned kernel collection: %w", err)
	}

	info, err := os.Stat(destination)
	if err != nil {
		return fmt.Errorf("inspect exported FairPlayIOKit: %w", err)
	}

	if !info.Mode().IsRegular() {
		return errors.New("exported FairPlayIOKit is not a regular file")
	}

	return nil
}

func im4pPayload(data []byte) ([]byte, error) {
	var container asn1.RawValue

	rest, err := asn1.Unmarshal(data, &container)
	if err != nil || len(rest) != 0 || container.Tag != asn1.TagSequence || !container.IsCompound {
		return nil, errors.New("invalid IM4P container")
	}

	contents := container.Bytes
	fields := make([]asn1.RawValue, 0, 4)

	for len(contents) != 0 && len(fields) < 4 {
		var field asn1.RawValue

		contents, err = asn1.Unmarshal(contents, &field)
		if err != nil {
			return nil, fmt.Errorf("decode IM4P field: %w", err)
		}

		fields = append(fields, field)
	}

	if len(fields) != 4 || fields[0].Tag != asn1.TagIA5String || string(fields[0].Bytes) != "IM4P" || fields[1].Tag != asn1.TagIA5String || string(fields[1].Bytes) != "krnl" || fields[3].Tag != asn1.TagOctetString {
		return nil, errors.New("invalid IM4P kernel payload")
	}

	return append([]byte(nil), fields[3].Bytes...), nil
}
