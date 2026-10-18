// Package profile loads the precomputed Store profile compiled into the program.
package profile

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Data contains immutable metadata, initialized guest pages, and the plugin image.
type Data struct{ Metadata, Pages, Plugin []byte }

type artifact struct {
	Name, SHA256, Encoded string
	Size                  int
}

type loader struct {
	gate      chan struct{}
	installed *Data
	decode    func(context.Context) (Data, error)
}

var defaultLoader = loader{gate: make(chan struct{}, 1), decode: decodeGenerated}

// Load decodes the committed profile once per process. It performs no file access,
// downloads, or metadata generation.
func Load(ctx context.Context) (Data, error) { return defaultLoader.load(ctx) }

func (l *loader) load(ctx context.Context) (Data, error) {
	if ctx == nil {
		return Data{}, errors.New("store profile context is nil")
	}

	select {
	case l.gate <- struct{}{}:
	case <-ctx.Done():
		return Data{}, fmt.Errorf("load Store profile: %w", ctx.Err())
	}

	defer func() { <-l.gate }()

	if err := ctx.Err(); err != nil {
		return Data{}, fmt.Errorf("load Store profile: %w", err)
	}

	if l.installed != nil {
		return *l.installed, nil
	}

	data, err := l.decode(ctx)
	if err != nil {
		return Data{}, err
	}

	if err := ctx.Err(); err != nil {
		return Data{}, fmt.Errorf("load Store profile: %w", err)
	}

	l.installed = &data

	return data, nil
}

func decodeGenerated(ctx context.Context) (Data, error) {
	metadata, err := decodeArtifact(ctx, generatedMetadata, 128<<20)
	if err != nil {
		return Data{}, err
	}

	pages, err := decodeArtifact(ctx, generatedPages, 256<<20)
	if err != nil {
		return Data{}, err
	}

	plugin, err := decodeArtifact(ctx, generatedPlugin, 64<<20)
	if err != nil {
		return Data{}, err
	}

	result := Data{Metadata: metadata, Pages: pages, Plugin: plugin}
	if err = validateData(result); err != nil {
		return Data{}, err
	}

	return result, nil
}

// decodeArtifact bounds decompression by the committed size and checks the raw
// digest in addition to the compressed frame's checksum.
func decodeArtifact(ctx context.Context, item artifact, limit int) ([]byte, error) {
	if item.Size <= 0 || item.Size > limit {
		return nil, fmt.Errorf("invalid compiled %s size", item.Name)
	}

	input := base64.NewDecoder(base64.StdEncoding, strings.NewReader(item.Encoded))

	decoder, err := zstd.NewReader(contextReader{ctx: ctx, reader: input}, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(limit)))
	if err != nil {
		return nil, fmt.Errorf("open compiled %s: %w", item.Name, err)
	}

	defer decoder.Close()

	data := make([]byte, item.Size+1)

	n, err := io.ReadFull(decoder, data)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode compiled %s: %w", item.Name, err)
	}

	if n != item.Size {
		return nil, fmt.Errorf("compiled %s has an invalid decoded size", item.Name)
	}

	data = data[:n:n]
	if fmt.Sprintf("%x", sha256.Sum256(data)) != item.SHA256 {
		return nil, fmt.Errorf("compiled %s failed integrity verification", item.Name)
	}

	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, fmt.Errorf("read compiled Store profile: %w", err)
	}

	n, err := r.reader.Read(data)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}

	if err != nil {
		return n, fmt.Errorf("read compiled Store profile: %w", err)
	}

	return n, nil
}

func validateData(data Data) error {
	var metadata struct {
		UUID, PluginUUID string
		Pages            []struct{ Address, Offset uint64 }
	}

	if err := json.Unmarshal(data.Metadata, &metadata); err != nil {
		return fmt.Errorf("decode Store metadata: %w", err)
	}

	if metadata.UUID != generatedFrameworkUUID || metadata.PluginUUID != generatedPluginUUID {
		return errors.New("compiled Store profile uses unexpected Apple images")
	}

	if len(metadata.Pages) == 0 || len(metadata.Pages) != len(data.Pages)/4096 || len(data.Pages)%4096 != 0 || len(data.Plugin) == 0 {
		return errors.New("incomplete compiled Store profile")
	}

	for i, page := range metadata.Pages {
		if page.Address%4096 != 0 || page.Offset != uint64(i)*4096 {
			return errors.New("invalid Store page index")
		}
	}

	return nil
}
