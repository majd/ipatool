package appstore

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	apphttp "github.com/majd/ipatool/v2/pkg/http"
	"howett.net/plist"
)

var infoPlistReleaseDateKeys = []string{
	"releaseDate",
	"ReleaseDate",
}

var infoPlistDisplayVersionKeys = []string{
	"CFBundleShortVersionString",
	"bundleShortVersionString",
}

type versionMetadata struct {
	DisplayVersion string
	ReleaseDate    time.Time
}

// rangeReaderReadAhead is the minimum size of a remote read. archive/zip parses the central
// directory through a 4 KiB bufio.Reader, and every request made through the Store client
// runs in the emulator, so each miss reads ahead and later reads are served from memory.
const rangeReaderReadAhead int64 = 1 << 20

type httpRangeReaderAt struct {
	client apphttp.Client[interface{}]
	url    string
	size   int64

	mu          sync.Mutex
	windowStart int64
	window      []byte
}

func newHTTPRangeReaderAt(client apphttp.Client[interface{}], url string) (*httpRangeReaderAt, int64, error) {
	if url == "" {
		return nil, 0, errors.New("url is empty")
	}

	size, err := remoteFileSize(client, url)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read remote file size: %w", err)
	}

	return &httpRangeReaderAt{
		client: client,
		url:    url,
		size:   size,
	}, size, nil
}

func remoteFileSize(client apphttp.Client[interface{}], url string) (int64, error) {
	req, err := client.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", "bytes=0-0")

	res, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}

	if res != nil && res.Body != nil {
		defer res.Body.Close()
	}

	if res.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("expected partial content response, got status %d", res.StatusCode)
	}

	size, err := parseContentRangeSize(res.Header.Get("Content-Range"))
	if err != nil {
		return 0, err
	}

	return size, nil
}

func parseContentRangeSize(header string) (int64, error) {
	if header == "" {
		return 0, errors.New("content range is empty")
	}

	slash := strings.LastIndex(header, "/")
	if slash == -1 || slash == len(header)-1 {
		return 0, fmt.Errorf("invalid content range: %s", header)
	}

	sizeText := header[slash+1:]
	if sizeText == "*" {
		return 0, fmt.Errorf("invalid content range size: %s", sizeText)
	}

	size, err := strconv.ParseInt(sizeText, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse content range size: %w", err)
	}

	return size, nil
}

func (r *httpRangeReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if off < 0 {
		return 0, errors.New("offset is negative")
	}

	if off >= r.size {
		return 0, io.EOF
	}

	end := off + int64(len(p))
	if end > r.size {
		end = r.size
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if off < r.windowStart || end > r.windowStart+int64(len(r.window)) {
		fetchEnd := off + max(int64(len(p)), rangeReaderReadAhead)
		if fetchEnd > r.size {
			fetchEnd = r.size
		}

		data, err := r.fetch(off, fetchEnd-1)
		if err != nil {
			return 0, err
		}

		r.windowStart = off
		r.window = data
	}

	n := copy(p[:end-off], r.window[off-r.windowStart:])

	if off+int64(len(p)) > r.size {
		return n, io.EOF
	}

	return n, nil
}

// fetch downloads bytes start...stop (inclusive) with a single range request.
func (r *httpRangeReaderAt) fetch(start, stop int64) ([]byte, error) {
	req, err := r.client.NewRequest("GET", r.url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, stop))

	res, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if res != nil && res.Body != nil {
		defer res.Body.Close()
	}

	if res.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("expected partial content response, got status %d", res.StatusCode)
	}

	data := make([]byte, stop-start+1)

	_, err = io.ReadFull(res.Body, data)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return data, nil
}

func (t *appstore) readVersionMetadataFromIPA(url string) (versionMetadata, error) {
	reader, size, err := newHTTPRangeReaderAt(t.httpClient, url)
	if err != nil {
		return versionMetadata{}, err
	}

	zipReader, err := zip.NewReader(reader, size)
	if err != nil {
		return versionMetadata{}, fmt.Errorf("failed to open zip reader: %w", err)
	}

	for _, file := range zipReader.File {
		if !isMainAppInfoPlist(file.Name) {
			continue
		}

		metadata, err := readVersionMetadataFromInfoPlist(file)
		if err != nil {
			return versionMetadata{}, err
		}

		return metadata, nil
	}

	return versionMetadata{}, errors.New("could not find Info.plist")
}

func isMainAppInfoPlist(name string) bool {
	parts := strings.Split(name, "/")

	return len(parts) == 3 && parts[0] == "Payload" && strings.HasSuffix(parts[1], ".app") && parts[2] == "Info.plist"
}

func readVersionMetadataFromInfoPlist(file *zip.File) (versionMetadata, error) {
	src, err := file.Open()
	if err != nil {
		return versionMetadata{}, fmt.Errorf("failed to open Info.plist: %w", err)
	}
	defer src.Close()

	data := new(bytes.Buffer)

	_, err = io.Copy(data, src)
	if err != nil {
		return versionMetadata{}, fmt.Errorf("failed to read Info.plist: %w", err)
	}

	metadata := map[string]interface{}{}

	_, err = plist.Unmarshal(data.Bytes(), &metadata)
	if err != nil {
		return versionMetadata{}, fmt.Errorf("failed to unmarshal Info.plist: %w", err)
	}

	displayVersion, err := readDisplayVersionFromMetadata(metadata)
	if err != nil {
		return versionMetadata{}, err
	}

	releaseDate, err := readReleaseDateFromInfoPlist(metadata, file.Modified)
	if err != nil {
		return versionMetadata{}, err
	}

	return versionMetadata{
		DisplayVersion: displayVersion,
		ReleaseDate:    releaseDate,
	}, nil
}

func readDisplayVersionFromMetadata(metadata map[string]interface{}) (string, error) {
	for _, key := range infoPlistDisplayVersionKeys {
		value, ok := metadata[key]
		if !ok {
			continue
		}

		displayVersion := strings.TrimSpace(fmt.Sprintf("%v", value))
		if displayVersion == "" || displayVersion == "<nil>" {
			return "", fmt.Errorf("%s is empty", key)
		}

		return displayVersion, nil
	}

	return "", errors.New("info plist does not contain a display version")
}

func readReleaseDateFromInfoPlist(metadata map[string]interface{}, modified time.Time) (time.Time, error) {
	for _, key := range infoPlistReleaseDateKeys {
		value, ok := metadata[key]
		if !ok {
			continue
		}

		releaseDate, err := parseReleaseDateValue(value)
		if err != nil {
			return time.Time{}, fmt.Errorf("failed to parse release date: %w", err)
		}

		return releaseDate, nil
	}

	if modified.IsZero() {
		return time.Time{}, errors.New("info plist does not contain a release date")
	}

	return modified.UTC(), nil
}

func parseReleaseDateValue(value interface{}) (time.Time, error) {
	switch val := value.(type) {
	case time.Time:
		return val.UTC(), nil
	case string:
		return parseReleaseDateString(val)
	case int:
		return time.Unix(int64(val), 0).UTC(), nil
	case int64:
		return time.Unix(val, 0).UTC(), nil
	case uint64:
		if val > math.MaxInt64 {
			return time.Time{}, fmt.Errorf("timestamp is too large: %d", val)
		}

		return time.Unix(int64(val), 0).UTC(), nil
	case float64:
		return time.Unix(int64(val), 0).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported release date type %T", value)
	}
}

func parseReleaseDateString(value string) (time.Time, error) {
	value = strings.TrimSpace(value)

	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"Monday, January 2, 2006",
		"2006-01-02",
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid release date: %s", value)
}
