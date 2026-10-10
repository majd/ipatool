//nolint:wsl // Binary format parsing is clearer when related bounds checks remain adjacent.
package assets

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/hpke"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/go-compressions/lzfse"
)

const (
	aeaMagic                    = "AEA1"
	aeaSymmetricProfile         = 1
	aeaMainKeyInfo              = "AEA_AMK"
	aeaRootKeyInfo              = "AEA_RHEK"
	aeaClusterKeyInfo           = "AEA_CK"
	aeaClusterHeaderKeyInfo     = "AEA_CHEK"
	aeaSegmentKeyInfo           = "AEA_SK"
	aeaLZFSECompression         = 'e'
	aeaSHA256Checksum           = 2
	aeaDecodedSegmentCacheCount = 64
	maxAEAAuthDataSize          = 1 << 20
	maxFCSKeySize               = 64 << 10
)

type aeaHeader struct {
	Magic                    [4]byte
	ProfileAndScryptStrength uint32
	AuthDataLength           uint32
}

type aeaMAC [sha256.Size]byte

type encryptedAEARootHeader struct {
	MAC        aeaMAC
	Data       [48]byte
	ClusterMAC aeaMAC
}

type aeaRootHeader struct {
	FileSize           uint64
	EncryptedSize      uint64
	SegmentSize        uint32
	SegmentsPerCluster uint32
	Compression        uint8
	Checksum           uint8
	_                  [22]byte
}

type aeaHeaderKey struct {
	MAC aeaMAC
	Key [32]byte
	IV  [aes.BlockSize]byte
}

type aeaSegmentHeader struct {
	DecompressedSize uint32
	CompressedSize   uint32
	Checksum         [sha256.Size]byte
}

type aeaSegment struct {
	inputOffset      int64
	outputOffset     int64
	compressedSize   uint32
	decompressedSize uint32
	checksum         [sha256.Size]byte
	mac              aeaMAC
	cluster          uint32
	index            uint32
}

type aeaReader struct {
	source      io.ReaderAt
	size        int64
	segmentSize uint32
	mainKey     []byte
	segments    []aeaSegment
	cache       map[int][]byte
	cacheOrder  []int
	cacheMu     sync.Mutex
}

type fcsResponse struct {
	EncRequest string `json:"enc-request"`
	WrappedKey string `json:"wrapped-key"`
}

func newAEAReader(ctx context.Context, client *http.Client, source io.ReaderAt, sourceSize int64) (*aeaReader, error) {
	var header aeaHeader
	if err := readBinaryAt(source, 0, &header); err != nil {
		return nil, fmt.Errorf("read AEA header: %w", err)
	}

	if string(header.Magic[:]) != aeaMagic {
		return nil, fmt.Errorf("invalid AEA magic %q", header.Magic)
	}

	if header.ProfileAndScryptStrength&0xffffff != aeaSymmetricProfile {
		return nil, fmt.Errorf("unsupported AEA profile %d", header.ProfileAndScryptStrength&0xffffff)
	}

	if header.AuthDataLength > maxAEAAuthDataSize {
		return nil, fmt.Errorf("AEA authentication data is too large: %d", header.AuthDataLength)
	}

	headerSize := int64(binary.Size(header))
	authData := make([]byte, header.AuthDataLength)
	if _, err := source.ReadAt(authData, headerSize); err != nil {
		return nil, fmt.Errorf("read AEA authentication data: %w", err)
	}

	metadata, err := parseAEAMetadata(authData)
	if err != nil {
		return nil, err
	}

	symmetricKey, err := resolveAEASymmetricKey(ctx, client, metadata)
	if err != nil {
		return nil, err
	}

	mainSaltOffset := headerSize + int64(len(authData))
	mainSalt := make([]byte, sha256.Size)
	if _, err := source.ReadAt(mainSalt, mainSaltOffset); err != nil {
		return nil, fmt.Errorf("read AEA main salt: %w", err)
	}

	mainKey, err := deriveAEAKey(symmetricKey, mainSalt, binary.LittleEndian.AppendUint32([]byte(aeaMainKeyInfo), header.ProfileAndScryptStrength), len(symmetricKey))
	if err != nil {
		return nil, fmt.Errorf("derive AEA main key: %w", err)
	}

	rootOffset := mainSaltOffset + int64(len(mainSalt))

	var encryptedRoot encryptedAEARootHeader
	if err := readBinaryAt(source, rootOffset, &encryptedRoot); err != nil {
		return nil, fmt.Errorf("read encrypted AEA root header: %w", err)
	}

	rootKey, err := deriveAEAHeaderKey(mainKey, []byte(aeaRootKeyInfo))
	if err != nil {
		return nil, fmt.Errorf("derive AEA root key: %w", err)
	}

	rootSalt := make([]byte, 0, len(encryptedRoot.ClusterMAC)+len(authData))
	rootSalt = append(rootSalt, encryptedRoot.ClusterMAC[:]...)
	rootSalt = append(rootSalt, authData...)
	computedRootMAC := calculateAEAMAC(rootKey.MAC[:], encryptedRoot.Data[:], rootSalt)
	if !hmac.Equal(encryptedRoot.MAC[:], computedRootMAC[:]) {
		return nil, errors.New("AEA root header failed authentication")
	}

	rootData, err := decryptAEACTR(encryptedRoot.Data[:], rootKey.Key[:], rootKey.IV[:])
	if err != nil {
		return nil, fmt.Errorf("decrypt AEA root header: %w", err)
	}

	var root aeaRootHeader
	if err := binary.Read(bytes.NewReader(rootData), binary.LittleEndian, &root); err != nil {
		return nil, fmt.Errorf("decode AEA root header: %w", err)
	}

	if root.FileSize == 0 || root.FileSize > ^uint64(0)>>1 {
		return nil, fmt.Errorf("invalid AEA output size %d", root.FileSize)
	}

	if root.EncryptedSize != uint64(sourceSize) {
		return nil, fmt.Errorf("AEA encrypted size is %d, expected %d", root.EncryptedSize, sourceSize)
	}

	if root.SegmentSize == 0 || root.SegmentsPerCluster == 0 {
		return nil, errors.New("AEA has an invalid segment layout")
	}

	if root.Compression != aeaLZFSECompression || root.Checksum != aeaSHA256Checksum {
		return nil, fmt.Errorf("unsupported AEA encoding: compression %q, checksum %d", root.Compression, root.Checksum)
	}

	reader := &aeaReader{
		source:      source,
		size:        int64(root.FileSize),
		segmentSize: root.SegmentSize,
		mainKey:     mainKey,
		segments:    make([]aeaSegment, (root.FileSize+uint64(root.SegmentSize)-1)/uint64(root.SegmentSize)),
		cache:       make(map[int][]byte, aeaDecodedSegmentCacheCount),
	}

	clusterOffset := rootOffset + int64(binary.Size(encryptedRoot))
	if err := reader.indexClusters(clusterOffset, encryptedRoot.ClusterMAC, root, sourceSize); err != nil {
		return nil, err
	}

	return reader, nil
}

func parseAEAMetadata(data []byte) (map[string][]byte, error) {
	metadata := make(map[string][]byte)
	reader := bytes.NewReader(data)

	for reader.Len() != 0 {
		var length uint32
		if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
			return nil, fmt.Errorf("read AEA metadata length: %w", err)
		}

		if length < 5 || uint64(length-4) > uint64(reader.Len()) {
			return nil, fmt.Errorf("invalid AEA metadata record length %d", length)
		}

		record := make([]byte, length-4)
		if _, err := io.ReadFull(reader, record); err != nil {
			return nil, fmt.Errorf("read AEA metadata record: %w", err)
		}

		key, value, ok := bytes.Cut(record, []byte{0})
		if !ok || len(key) == 0 {
			return nil, errors.New("invalid AEA metadata record")
		}

		metadata[string(key)] = value
	}

	return metadata, nil
}

// resolveAEASymmetricKey downloads the Apple-published FCS P-256 private key
// identified by the AEA metadata. That key is the HPKE recipient key for this
// image's wrapped symmetric key; the unwrapped key is then used to derive the
// AES and HMAC keys that decrypt and authenticate the AEA headers and segments.
// It is restore-image key material, not a user credential.
func resolveAEASymmetricKey(ctx context.Context, client *http.Client, metadata map[string][]byte) ([]byte, error) {
	keyURLData, ok := metadata["com.apple.wkms.fcs-key-url"]
	if !ok {
		return nil, errors.New("AEA metadata does not contain an FCS key URL")
	}

	keyURL, err := url.Parse(string(keyURLData))
	if err != nil || keyURL.Scheme != "https" || keyURL.Hostname() != "wkms-public.apple.com" {
		return nil, fmt.Errorf("invalid Apple FCS key URL %q", keyURLData)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, keyURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Apple FCS key request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download Apple FCS key: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download Apple FCS key: server returned %s", response.Status)
	}

	pemData, err := io.ReadAll(io.LimitReader(response.Body, maxFCSKeySize+1))
	if err != nil {
		return nil, fmt.Errorf("read Apple FCS key: %w", err)
	}

	if len(pemData) > maxFCSKeySize {
		return nil, errors.New("apple FCS key response is too large")
	}

	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("apple FCS key response is not PEM encoded")
	}

	privateKeyValue, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Apple FCS private key: %w", err)
	}

	privateKey, ok := privateKeyValue.(*ecdsa.PrivateKey)
	if !ok || privateKey.Curve != elliptic.P256() {
		return nil, errors.New("apple FCS key is not a P-256 private key")
	}

	responseData, ok := metadata["com.apple.wkms.fcs-response"]
	if !ok {
		return nil, errors.New("AEA metadata does not contain an FCS response")
	}

	var wrapped fcsResponse
	if err := json.Unmarshal(responseData, &wrapped); err != nil {
		return nil, fmt.Errorf("parse AEA FCS response: %w", err)
	}

	encapsulation, err := base64.StdEncoding.DecodeString(wrapped.EncRequest)
	if err != nil {
		return nil, fmt.Errorf("decode AEA FCS encapsulation: %w", err)
	}

	wrappedKey, err := base64.StdEncoding.DecodeString(wrapped.WrappedKey)
	if err != nil {
		return nil, fmt.Errorf("decode AEA wrapped key: %w", err)
	}

	kem := hpke.DHKEM(ecdh.P256())
	ecdhPrivateKey, err := privateKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("convert AEA FCS private key: %w", err)
	}
	hpkePrivateKey, err := kem.NewPrivateKey(ecdhPrivateKey.Bytes())
	if err != nil {
		return nil, fmt.Errorf("load AEA FCS private key: %w", err)
	}

	recipient, err := hpke.NewRecipient(encapsulation, hpkePrivateKey, hpke.HKDFSHA256(), hpke.AES256GCM(), nil)
	if err != nil {
		return nil, fmt.Errorf("initialize AEA FCS recipient: %w", err)
	}

	symmetricKey, err := recipient.Open(nil, wrappedKey)
	if err != nil {
		return nil, fmt.Errorf("unwrap AEA symmetric key: %w", err)
	}

	return symmetricKey, nil
}

func (r *aeaReader) indexClusters(offset int64, clusterMAC aeaMAC, root aeaRootHeader, sourceSize int64) error {
	const segmentHeaderSize = 8 + sha256.Size

	headerSize := int64(segmentHeaderSize*root.SegmentsPerCluster + sha256.Size + sha256.Size*root.SegmentsPerCluster)
	outputSize := uint64(0)
	clusterIndex := uint32(0)

	for outputSize < root.FileSize {
		if offset < 0 || headerSize > sourceSize-offset {
			return errors.New("AEA cluster header extends beyond the encrypted image")
		}

		headerData := make([]byte, headerSize)
		if _, err := r.source.ReadAt(headerData, offset); err != nil {
			return fmt.Errorf("read AEA cluster %d header: %w", clusterIndex, err)
		}

		clusterKey, err := deriveAEAKey(r.mainKey, nil, binary.LittleEndian.AppendUint32([]byte(aeaClusterKeyInfo), clusterIndex), len(r.mainKey))
		if err != nil {
			return fmt.Errorf("derive AEA cluster %d key: %w", clusterIndex, err)
		}

		clusterHeaderKey, err := deriveAEAHeaderKey(clusterKey, []byte(aeaClusterHeaderKeyInfo))
		if err != nil {
			return fmt.Errorf("derive AEA cluster %d header key: %w", clusterIndex, err)
		}

		encryptedHeadersSize := int(segmentHeaderSize * root.SegmentsPerCluster)
		encryptedHeaders := headerData[:encryptedHeadersSize]
		var nextClusterMAC aeaMAC
		copy(nextClusterMAC[:], headerData[encryptedHeadersSize:encryptedHeadersSize+sha256.Size])
		segmentMACData := headerData[encryptedHeadersSize+sha256.Size:]

		hmacSalt := make([]byte, 0, len(nextClusterMAC)+len(segmentMACData))
		hmacSalt = append(hmacSalt, nextClusterMAC[:]...)
		hmacSalt = append(hmacSalt, segmentMACData...)
		computedMAC := calculateAEAMAC(clusterHeaderKey.MAC[:], encryptedHeaders, hmacSalt)
		if !hmac.Equal(clusterMAC[:], computedMAC[:]) {
			return fmt.Errorf("AEA cluster %d failed authentication", clusterIndex)
		}

		decryptedHeaders, err := decryptAEACTR(encryptedHeaders, clusterHeaderKey.Key[:], clusterHeaderKey.IV[:])
		if err != nil {
			return fmt.Errorf("decrypt AEA cluster %d headers: %w", clusterIndex, err)
		}

		dataOffset := offset + headerSize
		for segmentIndex := range root.SegmentsPerCluster {
			start := int(segmentIndex) * segmentHeaderSize
			var segmentHeader aeaSegmentHeader
			if err := binary.Read(bytes.NewReader(decryptedHeaders[start:start+segmentHeaderSize]), binary.LittleEndian, &segmentHeader); err != nil {
				return fmt.Errorf("decode AEA cluster %d segment %d header: %w", clusterIndex, segmentIndex, err)
			}

			if segmentHeader.DecompressedSize == 0 {
				if segmentHeader.CompressedSize != 0 {
					return fmt.Errorf("AEA cluster %d segment %d has compressed data without output", clusterIndex, segmentIndex)
				}

				continue
			}

			if segmentHeader.DecompressedSize > root.SegmentSize || segmentHeader.CompressedSize == 0 {
				return fmt.Errorf("AEA cluster %d segment %d has invalid sizes", clusterIndex, segmentIndex)
			}

			if int64(segmentHeader.CompressedSize) > sourceSize-dataOffset {
				return fmt.Errorf("AEA cluster %d segment %d extends beyond the encrypted image", clusterIndex, segmentIndex)
			}

			var segmentMAC aeaMAC
			copy(segmentMAC[:], segmentMACData[int(segmentIndex)*sha256.Size:(int(segmentIndex)+1)*sha256.Size])

			globalIndex := uint64(clusterIndex)*uint64(root.SegmentsPerCluster) + uint64(segmentIndex)
			if globalIndex >= uint64(len(r.segments)) {
				return fmt.Errorf("AEA cluster %d segment %d is outside the declared output", clusterIndex, segmentIndex)
			}

			outputOffset := int64(globalIndex * uint64(root.SegmentSize))
			if uint64(outputOffset)+uint64(segmentHeader.DecompressedSize) > root.FileSize {
				return fmt.Errorf("AEA cluster %d segment %d extends beyond the declared output", clusterIndex, segmentIndex)
			}

			r.segments[globalIndex] = aeaSegment{
				inputOffset:      dataOffset,
				outputOffset:     outputOffset,
				compressedSize:   segmentHeader.CompressedSize,
				decompressedSize: segmentHeader.DecompressedSize,
				checksum:         segmentHeader.Checksum,
				mac:              segmentMAC,
				cluster:          clusterIndex,
				index:            segmentIndex,
			}

			dataOffset += int64(segmentHeader.CompressedSize)
			outputSize += uint64(segmentHeader.DecompressedSize)
			if outputSize > root.FileSize {
				return errors.New("AEA segment data exceeds the declared output size")
			}
		}

		offset = dataOffset
		clusterMAC = nextClusterMAC
		clusterIndex++
	}

	if outputSize != root.FileSize {
		return fmt.Errorf("AEA segment data has size %d, expected %d", outputSize, root.FileSize)
	}

	return nil
}

func (r *aeaReader) ReadAt(data []byte, offset int64) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	if offset < 0 {
		return 0, errors.New("negative AEA output offset")
	}

	if offset >= r.size {
		return 0, io.EOF
	}

	wanted := len(data)
	if remaining := r.size - offset; int64(wanted) > remaining {
		wanted = int(remaining)
	}

	written := 0
	for written < wanted {
		segmentIndex := int((offset + int64(written)) / int64(r.segmentSize))
		if segmentIndex >= len(r.segments) {
			return written, errors.New("AEA segment index is incomplete")
		}

		segment := r.segments[segmentIndex]
		if segment.decompressedSize == 0 {
			return written, fmt.Errorf("AEA segment %d is missing", segmentIndex)
		}
		decoded, err := r.decodeSegment(segmentIndex, segment)
		if err != nil {
			return written, err
		}

		inside := int(offset + int64(written) - segment.outputOffset)
		if inside < 0 || inside >= len(decoded) {
			return written, errors.New("AEA segment has an invalid output offset")
		}

		count := min(wanted-written, len(decoded)-inside)
		copy(data[written:written+count], decoded[inside:inside+count])
		written += count
	}

	if wanted != len(data) {
		return written, io.EOF
	}

	return written, nil
}

func (r *aeaReader) decodeSegment(cacheIndex int, segment aeaSegment) ([]byte, error) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	if data, ok := r.cache[cacheIndex]; ok {
		return data, nil
	}

	encrypted := make([]byte, segment.compressedSize)
	if _, err := r.source.ReadAt(encrypted, segment.inputOffset); err != nil {
		return nil, fmt.Errorf("read AEA cluster %d segment %d: %w", segment.cluster, segment.index, err)
	}

	clusterKey, err := deriveAEAKey(r.mainKey, nil, binary.LittleEndian.AppendUint32([]byte(aeaClusterKeyInfo), segment.cluster), len(r.mainKey))
	if err != nil {
		return nil, fmt.Errorf("derive AEA cluster %d key: %w", segment.cluster, err)
	}

	segmentKey, err := deriveAEAHeaderKey(clusterKey, binary.LittleEndian.AppendUint32([]byte(aeaSegmentKeyInfo), segment.index))
	if err != nil {
		return nil, fmt.Errorf("derive AEA cluster %d segment %d key: %w", segment.cluster, segment.index, err)
	}

	computedMAC := calculateAEAMAC(segmentKey.MAC[:], encrypted, nil)
	if !hmac.Equal(segment.mac[:], computedMAC[:]) {
		return nil, fmt.Errorf("AEA cluster %d segment %d failed authentication", segment.cluster, segment.index)
	}

	compressed, err := decryptAEACTR(encrypted, segmentKey.Key[:], segmentKey.IV[:])
	if err != nil {
		return nil, fmt.Errorf("decrypt AEA cluster %d segment %d: %w", segment.cluster, segment.index, err)
	}

	var decoded []byte
	if segment.compressedSize == segment.decompressedSize {
		decoded = compressed
	} else {
		decoded, err = lzfse.Decompress(compressed)
		if err != nil {
			return nil, fmt.Errorf("decompress AEA cluster %d segment %d: %w", segment.cluster, segment.index, err)
		}
		if len(decoded) != int(segment.decompressedSize) {
			return nil, fmt.Errorf("decompress AEA cluster %d segment %d: produced %d bytes, expected %d", segment.cluster, segment.index, len(decoded), segment.decompressedSize)
		}
	}

	if sha256.Sum256(decoded) != segment.checksum {
		return nil, fmt.Errorf("AEA cluster %d segment %d failed checksum verification", segment.cluster, segment.index)
	}

	if len(r.cacheOrder) == aeaDecodedSegmentCacheCount {
		delete(r.cache, r.cacheOrder[0])
		copy(r.cacheOrder, r.cacheOrder[1:])
		r.cacheOrder = r.cacheOrder[:len(r.cacheOrder)-1]
	}

	r.cache[cacheIndex] = decoded
	r.cacheOrder = append(r.cacheOrder, cacheIndex)

	return decoded, nil
}

func deriveAEAKey(key, salt, info []byte, size int) ([]byte, error) {
	derived, err := hkdf.Key(sha256.New, key, salt, string(info), size)
	if err != nil {
		return nil, fmt.Errorf("expand AEA key: %w", err)
	}

	return derived, nil
}

func deriveAEAHeaderKey(key, info []byte) (aeaHeaderKey, error) {
	var headerKey aeaHeaderKey
	derived, err := deriveAEAKey(key, nil, info, binary.Size(headerKey))
	if err != nil {
		return aeaHeaderKey{}, fmt.Errorf("derive AEA header key: %w", err)
	}

	if err := binary.Read(bytes.NewReader(derived), binary.LittleEndian, &headerKey); err != nil {
		return aeaHeaderKey{}, fmt.Errorf("decode AEA header key: %w", err)
	}

	return headerKey, nil
}

func calculateAEAMAC(key, data, salt []byte) aeaMAC {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(salt)
	_, _ = mac.Write(data)

	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(salt)))
	_, _ = mac.Write(length[:])

	return aeaMAC(mac.Sum(nil))
}

func decryptAEACTR(data, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize AEA cipher: %w", err)
	}

	decrypted := make([]byte, len(data))
	cipher.NewCTR(block, iv).XORKeyStream(decrypted, data)

	return decrypted, nil
}

func readBinaryAt(source io.ReaderAt, offset int64, value any) error {
	size := binary.Size(value)
	if size < 0 {
		return errors.New("invalid binary value")
	}

	data := make([]byte, size)
	if _, err := source.ReadAt(data, offset); err != nil {
		return fmt.Errorf("read binary value: %w", err)
	}

	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, value); err != nil {
		return fmt.Errorf("decode binary value: %w", err)
	}

	return nil
}
