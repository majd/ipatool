package assets

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

const storeAgentCacheDirectory = "apple-storeagent-v1"

var storeAgentSpec = fileSpec{
	name:   "storeagent",
	path:   "./System/Library/PrivateFrameworks/CommerceKit.framework/Versions/A/Resources/storeagent",
	size:   2580176,
	digest: mustDigest("70ce036f9dbcbc04db9511ebd08de0dd3cbc35ccc9d44b089c90170cb5453c59"),
}

// LoadStoreAgent loads the pinned StoreAgent image from its separate cache or Apple payload.
func LoadStoreAgent(ctx context.Context) ([]byte, error) {
	directory, err := storeAgentDirectory()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(directory, storeAgentSpec.name)
	if data, err := readStoreAgent(path); err == nil {
		return data, nil
	}

	files, err := downloadFiles(ctx, []fileSpec{storeAgentSpec}, "download Apple StoreAgent asset")
	if err != nil {
		return nil, err
	}

	data := files[storeAgentSpec.name]
	if err := VerifyStoreAgent(data); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create StoreAgent asset cache: %w", err)
	}

	if err := replaceFile(path, data); err != nil {
		return nil, fmt.Errorf("cache Apple StoreAgent asset: %w", err)
	}

	return data, nil
}

func storeAgentDirectory() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find user cache directory: %w", err)
	}

	return filepath.Join(root, "ipatool", "sap", storeAgentCacheDirectory), nil
}

func readStoreAgent(path string) ([]byte, error) {
	data, err := readCacheFile(path, storeAgentSpec.size)
	if err != nil {
		return nil, fmt.Errorf("read cached Apple StoreAgent asset: %w", err)
	}

	if err := VerifyStoreAgent(data); err != nil {
		return nil, err
	}

	return data, nil
}

// VerifyStoreAgent verifies the image profile required by the StoreAgent runtime.
func VerifyStoreAgent(data []byte) error {
	if len(data) != storeAgentSpec.size {
		return fmt.Errorf("apple StoreAgent asset has size %d, expected %d", len(data), storeAgentSpec.size)
	}

	if sha256.Sum256(data) != storeAgentSpec.digest {
		return fmt.Errorf("apple StoreAgent asset failed integrity verification")
	}

	return nil
}
