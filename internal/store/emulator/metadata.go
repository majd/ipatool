package emulator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// Keep one immutable template, bounded independently of the number of requests.
// The content digest prevents stale metadata reuse when callers change profiles.
var cachedMetadata = metadataCache{gate: make(chan struct{}, 1)}

type metadataCache struct {
	gate     chan struct{}
	digest   [sha256.Size]byte
	prepared *runtimeMetadata
}

type runtimeMetadata struct {
	profile           Profile
	classes           map[string]*Class
	classNames        map[uint64]string
	methods           map[string]uint64
	calls             map[uint64]Call
	selectors         map[uint64]string
	selectorAddresses map[string]uint64
	objects           map[uint64]Object
	entries           map[uint64]string
}

func (c *metadataCache) load(ctx context.Context, raw []byte) (*runtimeMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load Store metadata: %w", err)
	}

	digest := sha256.Sum256(raw)
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("load Store metadata: %w", ctx.Err())
	}

	defer func() { <-c.gate }()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load Store metadata: %w", err)
	}

	if c.prepared != nil && c.digest == digest {
		return c.prepared, nil
	}

	prepared, err := prepareMetadata(raw)
	if err != nil {
		return nil, err
	}

	if err = ctx.Err(); err != nil {
		return nil, fmt.Errorf("load Store metadata: %w", err)
	}

	c.prepared, c.digest = prepared, digest

	return prepared, nil
}

func prepareMetadata(raw []byte) (*runtimeMetadata, error) {
	var profile Profile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return nil, fmt.Errorf("decode Store metadata: %w", err)
	}

	if profile.UUID != "5DFC37A5-4689-3F9D-9655-D4774E1D8D08" {
		return nil, errors.New("unsupported Store profile UUID")
	}

	// Pinned dyld lazy-import thunk, verified against its resolved import slot
	// (0x1eaaee178). This is a host diagnostics preference, not Store logic.
	profile.Calls = append(profile.Calls, Call{Address: 0x1a2d0fb94, Symbol: "CRIsAutoSubmitEnabled"})
	m := &runtimeMetadata{profile: profile, classes: map[string]*Class{}, classNames: map[uint64]string{}, methods: map[string]uint64{}, calls: map[uint64]Call{}, selectors: map[uint64]string{}, selectorAddresses: map[string]uint64{}, objects: map[uint64]Object{}, entries: map[uint64]string{}}

	for i := range m.profile.Classes {
		class := &m.profile.Classes[i]
		m.classes[class.Name] = class
		m.classNames[class.Address] = class.Name

		if class.MetaAddress != 0 {
			m.classNames[class.MetaAddress] = class.Name
		}

		for _, method := range class.Methods {
			key := methodKey(class.Name, method.Selector, method.ClassMethod)
			m.methods[key] = method.Address
			m.entries[method.Address] = key
		}
	}

	for key, value := range profile.Strings {
		address, err := strconv.ParseUint(key, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("decode Store string address: %w", err)
		}

		m.objects[address] = Object{Class: "NSString", Value: value}
	}

	for key, value := range profile.Constants {
		address, err := strconv.ParseUint(key, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("decode Store constant address: %w", err)
		}

		var data any

		switch value.Kind {
		case "NSString":
			data = value.Text
		case "NSNumber":
			data = value.Number
			if value.IsBoolean {
				data = value.Boolean
			}
		case "NSDictionary":
			data = value.Entries
		case "NSArray", "NSSet":
			data = value.Items
		}

		m.objects[address] = Object{Class: value.Kind, Value: data}
	}

	for _, call := range profile.Calls {
		m.calls[call.Address] = call
		if call.Selector != "" {
			m.selectors[call.SelectorAddress] = call.Selector
			m.selectorAddresses[call.Selector] = call.SelectorAddress
		}
	}

	// These JSON tables have been converted to address-indexed templates above.
	// Retain only that representation for the lifetime of the process.
	m.profile.Strings = nil
	m.profile.Constants = nil

	return m, nil
}

func (m *runtimeMetadata) newRuntime() *Runtime {
	// Class definitions, call sites and entry names are immutable. The maps used
	// for dynamic classes, methods and selectors belong to each individual VM.
	r := &Runtime{profile: m.profile, classes: maps.Clone(m.classes), classNames: maps.Clone(m.classNames), methods: maps.Clone(m.methods), calls: m.calls, selectors: maps.Clone(m.selectors), selectorAddresses: maps.Clone(m.selectorAddresses), entries: m.entries, objects: make(map[uint64]*Object, len(m.objects)), pages: map[uint64]bool{}}

	for address, object := range m.objects {
		instance := object
		switch value := object.Value.(type) {
		case map[string]uint64:
			instance.Value = maps.Clone(value)
		case []uint64:
			instance.Value = slices.Clone(value)
		}

		r.objects[address] = &instance
	}

	return r
}
