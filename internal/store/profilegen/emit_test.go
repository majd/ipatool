package profilegen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"

	"github.com/klauspost/compress/zstd"
	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func sourceFixture() *Result {
	return &Result{Profile: &profileformat.Profile{UUID: FrameworkUUID, PluginUUID: PluginUUID, Pages: []profileformat.Page{{Address: 0x180000000}}}, Pages: make([]byte, 4096)}
}

// generatedPayloads reads actual Go declarations, so the test also checks that
// binary payloads are carried by valid source rather than external asset files.
func generatedPayloads(source []byte) map[string][]byte {
	file, err := parser.ParseFile(token.NewFileSet(), "data_generated.go", source, 0)
	Expect(err).NotTo(HaveOccurred())
	Expect(file.Name.Name).To(Equal("profile"))

	decoder, err := zstd.NewReader(nil)
	Expect(err).NotTo(HaveOccurred())

	defer decoder.Close()

	result := map[string][]byte{}

	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.VAR {
			continue
		}

		for _, spec := range group.Specs {
			value := spec.(*ast.ValueSpec)
			for _, element := range value.Values[0].(*ast.CompositeLit).Elts {
				entry := element.(*ast.KeyValueExpr)
				if entry.Key.(*ast.Ident).Name != "Encoded" {
					continue
				}

				encoded, err := strconv.Unquote(entry.Value.(*ast.BasicLit).Value)
				Expect(err).NotTo(HaveOccurred())
				compressed, err := base64.StdEncoding.DecodeString(encoded)
				Expect(err).NotTo(HaveOccurred())
				data, err := decoder.DecodeAll(compressed, nil)
				Expect(err).NotTo(HaveOccurred())

				result[value.Names[0].Name] = data
			}
		}
	}

	return result
}

var _ = Describe("Generated Go profile source", func() {
	It("is byte-identical across map insertion orders and round-trips every artifact", func() {
		first, second := sourceFixture(), sourceFixture()
		first.Profile.Symbols = map[string]uint64{}
		first.Profile.Symbols["a"] = 1
		first.Profile.Symbols["z"] = 2
		second.Profile.Symbols = map[string]uint64{}
		second.Profile.Symbols["z"] = 2
		second.Profile.Symbols["a"] = 1
		plugin := []byte{0, 0xff, '`', '"', '\\', '\n'}
		a, err := first.GoSource(context.Background(), plugin)
		Expect(err).NotTo(HaveOccurred())
		b, err := second.GoSource(context.Background(), plugin)
		Expect(err).NotTo(HaveOccurred())
		Expect(b).To(Equal(a))
		payloads := generatedPayloads(a)
		metadata, err := json.Marshal(first.Profile)
		Expect(err).NotTo(HaveOccurred())
		Expect(payloads).To(Equal(map[string][]byte{"generatedMetadata": metadata, "generatedPages": first.Pages, "generatedPlugin": plugin}))
	})

	It("rejects incomplete output and inconsistent page offsets", func() {
		input := sourceFixture()
		_, err := input.GoSource(context.Background(), nil)
		Expect(err).To(HaveOccurred())
		input.Profile.Pages[0].Offset = 4096
		_, err = input.GoSource(context.Background(), []byte("plugin"))
		Expect(err).To(MatchError(ContainSubstring("page index")))
	})

	It("rejects images from a different binary version", func() {
		input := sourceFixture()
		input.Profile.UUID = "different"
		_, err := input.GoSource(context.Background(), []byte("plugin"))
		Expect(err).To(MatchError(ContainSubstring("pinned Apple images")))
	})

	It("honors cancellation before encoding", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := sourceFixture().GoSource(ctx, []byte("plugin"))
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	})
})
