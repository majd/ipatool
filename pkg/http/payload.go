package http

import (
	"bytes"
	"fmt"
	"net/url"
	"strconv"

	"howett.net/plist"
)

type Payload interface {
	data() ([]byte, error)
}

type XMLPayload struct {
	Content map[string]interface{}
}

type URLPayload struct {
	Content map[string]interface{}
}

// RawPayload sends Content without applying an encoding. It is used by Apple
// services whose request body is already serialized, such as DAAP/DMAP.
type RawPayload struct {
	Content []byte
}

func (p *XMLPayload) data() ([]byte, error) {
	buffer := new(bytes.Buffer)

	err := plist.NewEncoder(buffer).Encode(p.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to encode plist: %w", err)
	}

	return buffer.Bytes(), nil
}

func (p *URLPayload) data() ([]byte, error) {
	params := url.Values{}

	for key, val := range p.Content {
		switch t := val.(type) {
		case string:
			params.Add(key, val.(string))
		case int:
			params.Add(key, strconv.Itoa(val.(int)))
		default:
			return nil, fmt.Errorf("value type is not supported (%s)", t)
		}
	}

	return []byte(params.Encode()), nil
}

func (p *RawPayload) data() ([]byte, error) {
	return append([]byte(nil), p.Content...), nil
}
