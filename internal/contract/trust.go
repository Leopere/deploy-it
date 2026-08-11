package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type repositoryIdentity struct {
	Root      string
	RemoteURL string
}

type trustRecord struct {
	Root             string `json:"root"`
	RemoteURL        string `json:"remote_url"`
	ManifestSHA256   string `json:"manifest_sha256"`
	ExecutableSHA256 string `json:"executable_sha256"`
}

type trustStore struct {
	Version      int                    `json:"version"`
	Repositories map[string]trustRecord `json:"repositories"`
}

func saveTrust(identity repositoryIdentity, manifest []byte, executable string) error {
	store, err := loadTrustStore()
	if err != nil {
		return err
	}
	record, err := newTrustRecord(identity, manifest, executable)
	if err != nil {
		return err
	}
	store.Repositories[trustKey(identity)] = record
	return writeTrustStore(store)
}

func verifyTrust(identity repositoryIdentity, manifest []byte, executable string) error {
	store, err := loadTrustStore()
	if err != nil {
		return err
	}
	want, err := newTrustRecord(identity, manifest, executable)
	if err != nil {
		return err
	}
	got, ok := store.Repositories[trustKey(identity)]
	if !ok || got != want {
		return errors.New("deployment contract is not locally trusted; review it and run deploy-it trust explicitly")
	}
	return nil
}

func newTrustRecord(identity repositoryIdentity, manifest []byte, executable string) (trustRecord, error) {
	executableData, err := os.ReadFile(executable)
	if err != nil {
		return trustRecord{}, err
	}
	manifestHash := sha256.Sum256(manifest)
	executableHash := sha256.Sum256(executableData)
	return trustRecord{
		Root:             identity.Root,
		RemoteURL:        identity.RemoteURL,
		ManifestSHA256:   hex.EncodeToString(manifestHash[:]),
		ExecutableSHA256: hex.EncodeToString(executableHash[:]),
	}, nil
}

func trustKey(identity repositoryIdentity) string {
	sum := sha256.Sum256([]byte(identity.Root + "\x00" + identity.RemoteURL))
	return hex.EncodeToString(sum[:])
}

func loadTrustStore() (trustStore, error) {
	path, err := trustStorePath()
	if err != nil {
		return trustStore{}, err
	}
	store := trustStore{Version: 1, Repositories: map[string]trustRecord{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return trustStore{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&store); err != nil {
		return trustStore{}, fmt.Errorf("invalid deploy-it trust store: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return trustStore{}, errors.New("invalid deploy-it trust store: multiple JSON values")
	}
	if store.Version != 1 || store.Repositories == nil {
		return trustStore{}, errors.New("invalid deploy-it trust store version")
	}
	return store, nil
}

func writeTrustStore(store trustStore) error {
	path, err := trustStorePath()
	if err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("deploy-it trust store must not be a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".deploy-it-trust-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func trustStorePath() (string, error) {
	if path := os.Getenv("DEPLOY_IT_TRUST_FILE"); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "deploy-it", "trust.json"), nil
}
