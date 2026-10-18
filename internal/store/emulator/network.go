package emulator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
	"howett.net/plist"
)

func (r *Runtime) hostBlock(fn func()) uint64 {
	function := r.alloc(16)
	if err := r.MemWrite(function, []byte{0xc0, 0x03, 0x5f, 0xd6}); err != nil {
		panic(err)
	}

	r.callbacks[function] = fn
	descriptor := r.alloc(16)
	r.write64(descriptor+8, 32)
	block := r.alloc(32)
	r.write64(block+8, 1<<28)
	r.write64(block+16, function)
	r.write64(block+24, descriptor)

	return block
}
func (r *Runtime) importValue(value any) uint64 {
	switch v := value.(type) {
	case nil:
		return 0
	case string:
		return r.New("NSString", v)
	case []byte:
		return r.New("NSData", v)
	case time.Time:
		return r.New("NSDate", v)
	case bool:
		return r.New("NSNumber", v)
	case int:
		return r.New("NSNumber", int64(v))
	case int64:
		return r.New("NSNumber", v)
	case uint64:
		return r.New("NSNumber", int64(v))
	case float64:
		if math.Trunc(v) == v && v >= math.MinInt64 && v < math.MaxInt64 {
			return r.New("NSNumber", int64(v))
		}

		return r.New("NSNumber", v)
	case []any:
		result := make([]uint64, len(v))
		for i, x := range v {
			result[i] = r.importValue(x)
		}

		return r.New("NSArray", result)
	case map[string]any:
		result := map[string]uint64{}
		for k, x := range v {
			result[k] = r.importValue(x)
		}

		return r.New("NSDictionary", result)
	default:
		panic(fmt.Sprintf("unsupported Foundation import %T", v))
	}
}
func (r *Runtime) networkMessage(receiver uint64, class, selector string, meta bool) bool {
	object := r.objects[receiver]

	if !meta && class == "NSURLSessionConfiguration" && (selector == "_allowsTCPFastOpen" || selector == "_allowsTLSSessionTickets" || selector == "URLCache") {
		// These optional CFNetwork facilities are absent from the transport.
		r.set(0)

		return true
	}

	if !meta && class == "NSHTTPURLResponse" && selector == "_CFURLResponse" {
		r.set(receiver)

		return true
	}

	if !meta && class == "NSURLSessionDataTask" && selector == "_timingData" {
		// The portable transport has no CFNetwork timing-data object.
		r.set(0)

		return true
	}

	if !meta && class == "AKAppleIDSession" && selector == "handleResponse:forRequest:shouldRetry:" {
		// Pinned AuthKit _canHandleAnisetteURLResponse: accepts exactly these
		// challenge status codes. All other responses leave shouldRetry false.
		status := r.objects[r.arg(2)].Properties["statusCode"]
		switch status {
		case 431, 433, 434, 441, 442, 443, 444:
			panic("AuthKit anisette challenge is not supported")
		}

		if out := r.arg(4); out != 0 {
			if err := r.MemWrite(out, []byte{0}); err != nil {
				panic(err)
			}
		}

		r.set(0)

		return true
	}

	if meta {
		switch {
		case class == "NSURLComponents" && selector == "componentsWithURL:resolvingAgainstBaseURL:":
			value, err := url.Parse(r.String(r.arg(2)))
			if err != nil {
				r.set(0)
			} else {
				r.set(r.New("NSURLComponents", value))
			}
		case class == "NSURLCache" && selector == "sharedURLCache":
			r.set(0)
		case class == "NSJSONSerialization" && selector == "isValidJSONObject:":
			r.set(boolValue(r.validJSONObject(r.arg(2), true, 0)))
		case class == "NSURLQueryItem" && selector == "queryItemWithName:value:":
			item := r.New(class, nil)
			r.objects[item].Properties = map[string]uint64{"name": r.arg(2), "value": r.arg(3)}
			r.set(item)
		case class == "NSHTTPCookie" && selector == "cookieWithProperties:":
			fields := r.objects[r.arg(2)].Value.(map[string]uint64)
			cookie := &http.Cookie{Name: r.String(fields["Name"]), Value: r.String(fields["Value"]), Domain: r.String(fields["Domain"]), Path: r.String(fields["Path"])}

			if value := fields["Secure"]; value != 0 {
				cookie.Secure = strings.EqualFold(fmt.Sprint(r.objects[value].Value), "TRUE")
			}

			if value := fields["HttpOnly"]; value != 0 {
				cookie.HttpOnly = strings.EqualFold(fmt.Sprint(r.objects[value].Value), "TRUE")
			}

			if value := fields["Expires"]; value != 0 {
				cookie.Expires, _ = r.objects[value].Value.(time.Time)
			}

			r.set(r.New("NSHTTPCookie", cookie))
		case class == "NSHTTPCookie" && selector == "requestHeaderFieldsWithCookies:":
			request := &http.Request{Header: http.Header{}}
			for _, item := range r.objects[r.arg(2)].Value.([]uint64) {
				request.AddCookie(r.objects[item].Value.(*http.Cookie))
			}

			fields := map[string]uint64{}
			if value := request.Header.Get("Cookie"); value != "" {
				fields["Cookie"] = r.New("NSString", value)
			}

			r.set(r.New("NSDictionary", fields))
		case class == "NSHTTPCookie" && selector == "cookiesWithResponseHeaderFields:forURL:":
			h := http.Header{}
			for k, v := range r.objects[r.arg(2)].Value.(map[string]uint64) {
				h.Set(k, r.String(v))
			}

			if raw := r.responseHeaders[r.arg(2)]; raw != nil {
				h = raw.Clone()
			}

			response := &http.Response{Header: h}
			cookies := response.Cookies()
			items := make([]uint64, 0, len(cookies))

			for _, cookie := range cookies {
				origin, _ := url.Parse(r.String(r.arg(3)))
				if cookie.Domain == "" {
					cookie.Domain = origin.Hostname()
				} else if !strings.HasPrefix(cookie.Domain, ".") {
					cookie.Domain = "." + cookie.Domain
				}

				if cookie.Path == "" {
					cookie.Path = "/"
				}

				items = append(items, r.New("NSHTTPCookie", cookie))
			}

			r.set(r.New("NSArray", items))
		case class == "NSURLSession" && selector == "sessionWithConfiguration:delegate:delegateQueue:":
			session := r.New("NSURLSession", nil)
			r.objects[session].Properties = map[string]uint64{"configuration": r.arg(2), "delegate": r.arg(3), "delegateQueue": r.arg(4)}
			r.set(session)
		case (class == "NSURLRequest" || class == "NSMutableURLRequest") && selector == "requestWithURL:":
			req := r.New(class, nil)
			r.objects[req].Properties = map[string]uint64{"URL": r.arg(2), "HTTPMethod": r.New("NSString", "GET"), "allHTTPHeaderFields": r.New("NSMutableDictionary", map[string]uint64{})}
			r.set(req)
		case class == "NSJSONSerialization" && selector == "JSONObjectWithData:options:error:":
			var v any

			err := json.Unmarshal(r.objects[r.arg(2)].Value.([]byte), &v)
			if err != nil {
				r.parserError(r.arg(4))
				r.set(0)

				return true
			}

			r.set(r.importValue(v))
		case class == "NSPropertyListSerialization" && selector == "propertyList:isValidForFormat:":
			v, err := r.Export(r.arg(2))
			if err == nil {
				_, err = plist.Marshal(v, plist.XMLFormat)
			}

			r.set(boolValue(err == nil))
		case class == "NSPropertyListSerialization" && selector == "propertyListWithData:options:format:error:":
			var v any

			_, err := plist.Unmarshal(r.objects[r.arg(2)].Value.([]byte), &v)
			if err != nil {
				r.parserError(r.arg(5))
				r.set(0)

				return true
			}

			r.set(r.importValue(v))
		case class == "NSPropertyListSerialization" && selector == "dataWithPropertyList:format:options:error:":
			v, err := r.Export(r.arg(2))
			if err != nil {
				panic(err)
			}

			format := plist.XMLFormat
			if r.arg(3) == 200 {
				format = plist.BinaryFormat
			}

			data, err := plist.Marshal(v, format)
			if err != nil {
				panic(err)
			}

			r.set(r.New("NSData", data))
		default:
			return false
		}

		return true
	}

	if selector == "initWithIndexesInRange:" && class == "NSIndexSet" {
		object.Value = [2]uint64{r.arg(2), r.arg(3)}
		r.set(receiver)

		return true
	}

	if selector == "containsIndex:" && class == "NSIndexSet" {
		v := object.Value.([2]uint64)
		r.set(boolValue(r.arg(2) >= v[0] && r.arg(2)-v[0] < v[1]))

		return true
	}

	if selector == "initWithURL:" && r.kindOf(class, "NSURLRequest") {
		_ = r.engine.RegWrite(unicorn.RegX3, 0)
		_ = r.engine.RegWrite(40, math.Float64bits(60))
		r.message(receiver, "initWithURL:cachePolicy:timeoutInterval:", false)

		return true
	}

	if selector == "initWithURL:cachePolicy:timeoutInterval:" && r.kindOf(class, "NSURLRequest") {
		object.Properties = map[string]uint64{"URL": r.arg(2), "cachePolicy": r.arg(3), "HTTPMethod": r.New("NSString", "GET"), "allHTTPHeaderFields": r.New("NSMutableDictionary", map[string]uint64{})}
		r.set(receiver)

		return true
	}

	if class == "NSHTTPURLResponse" && selector == "valueForHTTPHeaderField:" {
		fields := r.objects[object.Properties["allHeaderFields"]].Value.(map[string]uint64)
		for k, v := range fields {
			if strings.EqualFold(k, r.String(r.arg(2))) {
				r.set(v)

				return true
			}
		}

		r.set(0)

		return true
	}

	if r.kindOf(class, "NSURLRequest") {
		switch selector {
		case "HTTPBody", "HTTPBodyStream":
			// Bodyless requests have nil bodies. The framework inspects these
			// optional properties when handling redirects, even if no setter ran.
			r.set(object.Properties[selector])

			return true
		case "copy", "copyWithZone:", "mutableCopy", "mutableCopyWithZone:":
			if selector == "copy" || selector == "mutableCopy" {
				zone := selector + "WithZone:"
				if entry := r.lookup(class, zone, false); entry != 0 {
					_ = r.engine.RegWrite(unicorn.RegX2, 0)
					r.jump(entry, receiver, zone)

					return true
				}
			}

			copied := r.New(class, object.Value)

			for key, value := range object.Properties {
				r.objects[copied].Properties[key] = value
			}

			if h := object.Properties["allHTTPHeaderFields"]; h != 0 {
				r.objects[copied].Properties["allHTTPHeaderFields"] = r.foundationCopy(h, true)
			}

			r.set(copied)

			return true
		case "_CFURLRequest":
			r.set(receiver)

			return true
		case "_initWithCFURLRequest:":
			source := r.objects[r.arg(2)]
			object.Properties = map[string]uint64{}

			for k, v := range source.Properties {
				object.Properties[k] = v
			}

			if h := object.Properties["allHTTPHeaderFields"]; h != 0 {
				object.Properties["allHTTPHeaderFields"] = r.foundationCopy(h, true)
			}

			r.set(receiver)

			return true
		case "setValue:forHTTPHeaderField:", "addValue:forHTTPHeaderField:":
			if object.Properties["allHTTPHeaderFields"] == 0 {
				object.Properties["allHTTPHeaderFields"] = r.New("NSMutableDictionary", map[string]uint64{})
			}

			fields := r.objects[object.Properties["allHTTPHeaderFields"]].Value.(map[string]uint64)
			key := http.CanonicalHeaderKey(r.String(r.arg(3)))

			if r.arg(2) == 0 {
				delete(fields, key)
			} else {
				fields[key] = r.arg(2)
			}

			r.set(0)

			return true
		case "valueForHTTPHeaderField:":
			fields := r.objects[object.Properties["allHTTPHeaderFields"]].Value.(map[string]uint64)
			r.set(fields[http.CanonicalHeaderKey(r.String(r.arg(2)))])

			return true
		}
	}

	if class == "NSURLSession" && selector == "dataTaskWithRequest:" {
		task := r.New("NSURLSessionDataTask", nil)
		r.objects[task].Properties = map[string]uint64{"currentRequest": r.arg(2), "originalRequest": r.arg(2), "session": receiver, "taskIdentifier": task, "state": 1}
		r.set(task)

		return true
	}

	if class == "NSURLSessionDataTask" && selector == "resume" {
		if object.Properties["state"] != 1 {
			panic("task resumed twice")
		}

		object.Properties["state"] = 0

		r.pending = append(r.pending, r.hostBlock(func() { r.performHTTP(receiver) }))
		r.set(0)

		return true
	}

	if (class == "NSURLSession" && (selector == "invalidateAndCancel" || selector == "finishTasksAndInvalidate")) || (class == "NSURLSessionDataTask" && selector == "cancel") {
		r.set(0)

		return true
	}

	if class == "NSURLSession" || class == "NSURLSessionDataTask" || class == "NSHTTPURLResponse" || class == "NSURLResponse" || r.kindOf(class, "NSURLRequest") {
		if strings.HasPrefix(selector, "set") && strings.HasSuffix(selector, ":") && strings.Count(selector, ":") == 1 {
			key := selector[3 : len(selector)-1]
			if len(key) < 2 || key[1] < 'A' || key[1] > 'Z' {
				key = strings.ToLower(key[:1]) + key[1:]
			}

			object.Properties[key] = r.arg(2)
			r.set(0)

			return true
		}

		if value, ok := object.Properties[selector]; ok {
			r.set(value)

			return true
		}
	}

	if class == "NSHTTPCookie" {
		if selector == "initWithProperties:" {
			fields := r.objects[r.arg(2)].Value.(map[string]uint64)
			cookie := &http.Cookie{Name: r.String(fields["Name"]), Value: r.String(fields["Value"]), Domain: r.String(fields["Domain"]), Path: r.String(fields["Path"])}

			if p := fields["Secure"]; p != 0 {
				cookie.Secure = strings.EqualFold(fmt.Sprint(r.objects[p].Value), "TRUE")
			}

			if p := fields["HttpOnly"]; p != 0 {
				cookie.HttpOnly = strings.EqualFold(fmt.Sprint(r.objects[p].Value), "TRUE")
			}

			if p := fields["Expires"]; p != 0 {
				cookie.Expires, _ = r.objects[p].Value.(time.Time)
			}

			object.Value = cookie

			r.set(receiver)

			return true
		}

		cookie := object.Value.(*http.Cookie)

		switch selector {
		case "properties":
			fields := map[string]any{"Name": cookie.Name, "Value": cookie.Value, "Domain": cookie.Domain, "Path": cookie.Path, "Created": time.Now().Unix() - 978307200}
			if cookie.Secure {
				fields["Secure"] = "TRUE"
			}

			if cookie.HttpOnly {
				fields["HttpOnly"] = "TRUE"
			}

			if cookie.Expires.IsZero() {
				fields["Discard"] = "TRUE"
			} else {
				fields["Expires"] = cookie.Expires
			}

			r.set(r.importValue(fields))

			return true
		case "name":
			r.set(r.New("NSString", cookie.Name))

			return true
		case "domain":
			r.set(r.New("NSString", cookie.Domain))

			return true
		case "path":
			r.set(r.New("NSString", cookie.Path))

			return true
		case "value":
			r.set(r.New("NSString", cookie.Value))

			return true
		case "expiresDate":
			if cookie.Expires.IsZero() {
				r.set(0)
			} else {
				r.set(r.New("NSDate", cookie.Expires))
			}

			return true
		case "isSessionOnly":
			r.set(boolValue(cookie.Expires.IsZero()))

			return true
		case "version":
			r.set(0)

			return true
		case "isSecure":
			r.set(boolValue(cookie.Secure))

			return true
		case "isHTTPOnly":
			r.set(boolValue(cookie.HttpOnly))

			return true
		}
	}

	if class == "NSURLQueryItem" && (selector == "name" || selector == "value") {
		r.set(object.Properties[selector])

		return true
	}

	if class == "NSURLComponents" {
		if selector == "initWithURL:resolvingAgainstBaseURL:" {
			u, err := url.Parse(r.String(r.arg(2)))
			if err != nil {
				panic(err)
			}

			object.Value = u

			r.set(receiver)

			return true
		}

		u := object.Value.(*url.URL)

		switch selector {
		case "query", "percentEncodedQuery":
			if u.RawQuery == "" && !u.ForceQuery {
				r.set(0)
			} else {
				value := u.RawQuery

				if selector == "query" {
					var err error

					value, err = url.PathUnescape(value)
					if err != nil {
						panic("invalid URL query encoding")
					}
				}

				r.set(r.New("NSString", value))
			}

			return true
		case "setFragment:":
			u.Fragment = r.String(r.arg(2))
			u.RawFragment = ""

			r.set(0)

			return true
		case "URL":
			r.set(r.New("NSURL", u.String()))

			return true
		case "queryItems", "percentEncodedQueryItems":
			items := []uint64{}

			if u.RawQuery != "" {
				for _, part := range strings.Split(u.RawQuery, "&") {
					key, value, hasValue := strings.Cut(part, "=")
					if selector == "queryItems" {
						key, _ = url.QueryUnescape(strings.ReplaceAll(key, "+", "%2B"))
						value, _ = url.QueryUnescape(strings.ReplaceAll(value, "+", "%2B"))
					}

					item := r.New("NSURLQueryItem", nil)

					r.objects[item].Properties["name"] = r.New("NSString", key)
					if hasValue {
						r.objects[item].Properties["value"] = r.New("NSString", value)
					}

					items = append(items, item)
				}
			}

			r.set(r.New("NSArray", items))

			return true
		case "setQueryItems:", "setPercentEncodedQueryItems:":
			parts := []string{}

			if r.arg(2) != 0 {
				for _, item := range r.objects[r.arg(2)].Value.([]uint64) {
					p := r.objects[item].Properties
					part := strings.ReplaceAll(url.QueryEscape(r.String(p["name"])), "+", "%20")

					if p["value"] != 0 {
						part += "=" + strings.ReplaceAll(url.QueryEscape(r.String(p["value"])), "+", "%20")
					}

					if selector == "setPercentEncodedQueryItems:" {
						part = r.String(p["name"])
						if p["value"] != 0 {
							part += "=" + r.String(p["value"])
						}
					}

					parts = append(parts, part)
				}
			}

			u.RawQuery = strings.Join(parts, "&")

			r.set(0)

			return true
		}
	}

	if class == "NSURL" {
		u, err := url.Parse(object.Value.(string))
		if err != nil {
			panic(err)
		}

		switch selector {
		case "URLByAppendingPathComponent:", "URLByAppendingPathComponent:isDirectory:":
			value, err := url.JoinPath(u.String(), r.String(r.arg(2)))
			if err != nil {
				panic(err)
			}

			r.set(r.New("NSURL", value))

			return true
		case "fileSystemRepresentation":
			value := append([]byte(u.Path), 0)
			p := r.alloc(uint64(len(value)))
			_ = r.MemWrite(p, value)
			r.set(p)

			return true

		case "isFileURL":
			r.set(boolValue(u.Scheme == "file"))

			return true
		case "fragment":
			if u.Fragment == "" {
				r.set(0)
			} else {
				r.set(r.New("NSString", u.Fragment))
			}

			return true
		case "host":
			r.set(r.New("NSString", u.Hostname()))

			return true
		case "scheme":
			r.set(r.New("NSString", u.Scheme))

			return true
		case "query":
			r.set(r.New("NSString", u.RawQuery))

			return true
		}
	}

	if class == "NSData" || class == "NSMutableData" {
		switch selector {
		case "bytes":
			data, _ := object.Value.([]byte)
			p := r.alloc(uint64(len(data)))
			_ = r.MemWrite(p, data)
			r.set(p)

			return true
		case "appendBytes:length:":
			size := r.arg(3)
			if size > 32<<20 {
				panic("data append exceeds budget")
			}

			other, err := r.engine.MemRead(r.arg(2), size)
			if err != nil {
				panic(err)
			}

			data, _ := object.Value.([]byte)
			object.Value = append(data, other...)

			r.set(0)

			return true
		case "appendData:":
			other := r.objects[r.arg(2)].Value.([]byte)
			data, _ := object.Value.([]byte)
			object.Value = append(data, other...)

			r.set(0)

			return true
		}
	}

	return false
}

func (r *Runtime) validJSONObject(address uint64, top bool, depth int) bool {
	if depth > 64 {
		return false
	}

	object := r.objects[address]
	if object == nil {
		return false
	}

	switch value := object.Value.(type) {
	case map[string]uint64:
		for _, child := range value {
			if !r.validJSONObject(child, false, depth+1) {
				return false
			}
		}

		return true
	case []uint64:
		if !strings.Contains(object.Class, "Array") {
			return false
		}

		for _, child := range value {
			if !r.validJSONObject(child, false, depth+1) {
				return false
			}
		}

		return true
	case string, bool, int64:
		return !top
	case float64:
		return !top && !math.IsInf(value, 0) && !math.IsNaN(value)
	default:
		return !top && object.Class == "NSNull"
	}
}
func (r *Runtime) performHTTP(task uint64) {
	t := r.objects[task]
	req := r.objects[t.Properties["currentRequest"]]
	endpoint := r.String(req.Properties["URL"])

	u, err := url.Parse(endpoint)
	if err != nil || !allowedURL(u, r.authRequest || !r.streamResponse) {
		panic("unsupported transport URL")
	}

	var body []byte
	if p := req.Properties["HTTPBody"]; p != 0 {
		body = r.objects[p].Value.([]byte)
	}

	request, err := http.NewRequestWithContext(r.ctx, r.String(req.Properties["HTTPMethod"]), endpoint, bytes.NewReader(body))
	if err != nil {
		panic("cannot construct transport request")
	}

	if p := req.Properties["allHTTPHeaderFields"]; p != 0 {
		for k, v := range r.objects[p].Value.(map[string]uint64) {
			request.Header.Set(k, r.String(v))
		}
	}

	var response *http.Response
	response, err = r.transport(request)

	if err != nil {
		r.transportError = err

		panic("Store HTTP transport failed")
	}

	redirect := response.StatusCode == http.StatusMovedPermanently || response.StatusCode == http.StatusFound || response.StatusCode == http.StatusSeeOther || response.StatusCode == http.StatusTemporaryRedirect || response.StatusCode == http.StatusPermanentRedirect

	var data []byte

	if r.streamResponse && !redirect {
		r.lastResponse = response
	} else {
		data, err = io.ReadAll(io.LimitReader(response.Body, (32<<20)+1))
		_ = response.Body.Close()

		if err != nil {
			r.transportError = err

			panic("Store HTTP response read failed")
		}

		if len(data) > 32<<20 {
			panic("Store API response exceeds size limit")
		}

		copied := *response
		copied.Header = response.Header.Clone()
		copied.Body = io.NopCloser(bytes.NewReader(data))
		r.lastResponse = &copied
	}

	headers := map[string]uint64{}
	for k, v := range response.Header {
		headers[k] = r.New("NSString", strings.Join(v, ", "))
	}

	headerObject := r.New("NSDictionary", headers)

	if r.responseHeaders == nil {
		r.responseHeaders = map[uint64]http.Header{}
	}

	r.responseHeaders[headerObject] = response.Header.Clone()
	res := r.New("NSHTTPURLResponse", nil)
	r.objects[res].Properties = map[string]uint64{"statusCode": uint64(response.StatusCode), "allHeaderFields": headerObject, "URL": req.Properties["URL"], "MIMEType": r.New("NSString", strings.Split(response.Header.Get("Content-Type"), ";")[0]), "expectedContentLength": uint64(response.ContentLength)}
	t.Properties["response"] = res
	session := t.Properties["session"]
	delegate := r.objects[session].Properties["delegate"]
	completion := r.hostBlock(func() {
		if r.arg(1) != 1 {
			panic("framework rejected response")
		}

		r.pending = append(r.pending, r.hostBlock(func() {
			r.delegateCall(delegate, "URLSession:dataTask:didReceiveData:", []uint64{session, task, r.New("NSData", data)}, func() {
				r.pending = append(r.pending, r.hostBlock(func() {
					t.Properties["state"] = 3

					r.delegateCall(delegate, "URLSession:task:didCompleteWithError:", []uint64{session, task, 0}, func() { r.set(0) })
				}))
				r.set(0)
			})
		}))
		r.set(0)
	})
	deliver := func() {
		r.delegateCall(delegate, "URLSession:dataTask:didReceiveResponse:completionHandler:", []uint64{session, task, res, completion}, func() { r.set(0) })
	}

	if redirect && !r.noRedirects {
		destination, e := response.Location()
		if e != nil {
			deliver()

			return
		}

		if t.Properties["redirectCount"] >= 4 {
			panic("redirect limit exceeded")
		}

		if !allowedURL(destination, r.authRequest || !r.streamResponse) || (r.authRequest && (response.StatusCode == http.StatusSeeOther || destination.Port() != "" && destination.Port() != "443" || destination.RawPath != "" || !strings.HasSuffix(destination.Hostname(), "buy.itunes.apple.com") || strings.TrimRight(destination.Path, "/") != "/WebObjects/MZFinance.woa/wa/authenticate")) {
			panic("invalid Store redirect destination")
		}

		proposed := r.New("NSMutableURLRequest", nil)

		p := r.objects[proposed]
		for k, v := range req.Properties {
			p.Properties[k] = v
		}

		p.Properties["URL"] = r.New("NSURL", destination.String())
		fields := map[string]uint64{}

		if h := req.Properties["allHTTPHeaderFields"]; h != 0 {
			for k, v := range r.objects[h].Value.(map[string]uint64) {
				fields[k] = v
			}
		}

		if destination.Hostname() != u.Hostname() {
			for _, k := range []string{"Authorization", "Cookie", "X-Token", "X-Dsid", "Icloud-Dsid"} {
				delete(fields, k)
			}
		}

		if response.StatusCode == http.StatusSeeOther || ((response.StatusCode == http.StatusMovedPermanently || response.StatusCode == http.StatusFound) && r.String(req.Properties["HTTPMethod"]) == "POST") {
			p.Properties["HTTPMethod"] = r.New("NSString", "GET")
			p.Properties["HTTPBody"] = 0

			delete(fields, "Content-Length")
			delete(fields, "Content-Type")
		}

		p.Properties["allHTTPHeaderFields"] = r.New("NSMutableDictionary", fields)
		redirectCompletion := r.hostBlock(func() {
			next := r.arg(1)
			if next == 0 {
				deliver()

				return
			}

			if r.objects[next] == nil {
				panic("invalid redirected request")
			}

			t.Properties["currentRequest"] = next
			t.Properties["redirectCount"]++

			r.queuePending(r.hostBlock(func() { r.performHTTP(task) }), 0)
			r.set(0)
		})
		r.delegateCall(delegate, "URLSession:task:willPerformHTTPRedirection:newRequest:completionHandler:", []uint64{session, task, res, proposed, redirectCompletion}, func() { r.set(0) })

		return
	}

	deliver()
}
func (r *Runtime) delegateCall(receiver uint64, selector string, args []uint64, after func()) {
	obj := r.objects[receiver]
	if obj == nil {
		panic("missing transport delegate")
	}

	entry := r.lookup(obj.Class, selector, false)
	if entry == 0 {
		forward := r.lookup(obj.Class, "forwardingTargetForSelector:", false)
		if forward == 0 {
			panic("missing transport delegate method " + selector)
		}

		r.callbackFunction(forward, []uint64{receiver, r.selector("forwardingTargetForSelector:"), r.selector(selector)}, func() {
			target := r.arg(0)
			if target == 0 || target == receiver {
				panic("transport delegate cannot forward " + selector)
			}

			r.delegateCall(target, selector, args, after)
		})

		return
	}

	r.callbackFunction(entry, append([]uint64{receiver, r.selector(selector)}, args...), after)
}

func (r *Runtime) parserError(out uint64) {
	if out == 0 {
		return
	}

	errorObject := r.New("NSError", map[string]uint64{"domain": r.New("NSString", "NSCocoaErrorDomain"), "code": r.New("NSNumber", int64(3840)), "userInfo": r.New("NSDictionary", map[string]uint64{})})
	r.write64(out, errorObject)
}
