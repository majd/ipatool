package http

import (
	"bytes"
	"github.com/pkg/errors"
	"howett.net/plist"
	"net/url"
	"strconv"
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

func (p *XMLPayload) data() ([]byte, error) {
	buffer := new(bytes.Buffer)

	err := plist.NewEncoder(buffer).Encode(p.Content)
	if err != nil {
		return nil, errors.Wrap(err, ErrEncodePayloadXML.Error())
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
			return nil, errors.Errorf("%s: %s", ErrUnsupportedValueType.Error(), t)
		}
	}

	return []byte(params.Encode()), nil
}
