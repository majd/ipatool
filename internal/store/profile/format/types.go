// Package format defines the portable Store runtime profile.
package format

import "strconv"

type Method struct {
	Selector    string
	Address     uint64
	ClassMethod bool
	Types       string
}
type Class struct {
	MetaAddress uint64
	Name        string
	Address     uint64
	Super       string
	Size        uint64
	Ivars       map[string]uint64
	Methods     []Method
}
type Call struct {
	Address          uint64
	Symbol, Selector string
	SelectorAddress  uint64
}
type Page struct{ Address, Offset uint64 }
type Constant struct {
	IsBoolean bool `json:",omitempty"`
	Boolean   bool `json:",omitempty"`
	Kind      string
	Text      string            `json:",omitempty"`
	Number    int64             `json:",omitempty"`
	Entries   map[string]uint64 `json:",omitempty"`
	Items     []uint64          `json:",omitempty"`
}
type DictionaryLayout struct{ Metadata, KeyStride, ValueStride uint64 }
type MetadataPatch struct {
	Address uint64
	Data    []byte
}
type Profile struct {
	Bridges                                                  map[string]uint64
	ObjCMetadata                                             map[string]uint64
	SwiftSingletons                                          map[string]uint64
	MetadataPatches                                          []MetadataPatch
	SwiftForeign                                             map[string]uint64
	PluginUUID                                               string
	SwiftDictionaries                                        map[string]DictionaryLayout
	SwiftGlobals, SwiftTypes, SwiftWitnesses, SwiftAccessors map[string]uint64
	Symbols                                                  map[string]uint64
	Features                                                 map[string]bool
	Constants                                                map[string]Constant
	UUID                                                     string
	Classes                                                  []Class
	Calls                                                    []Call
	Strings                                                  map[string]string
	Pages                                                    []Page
}

// WitnessKey distinguishes conformances instantiated for different generic types.
func WitnessKey(descriptor, metadata uint64) string {
	return strconv.FormatUint(descriptor, 10) + ":" + strconv.FormatUint(metadata, 10)
}
