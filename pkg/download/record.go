package download

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// InstallRecordName is the metadata file written beside installed libraries.
const InstallRecordName = "bucky-install.json"

// InstallAsset records the selected archive and its expected extracted contents.
type InstallAsset struct {
	URL    string            `json:"url"`
	SHA256 string            `json:"sha256,omitempty"`
	Files  map[string]string `json:"files,omitempty"`
	Links  map[string]string `json:"links,omitempty"`
}

// InstallRecord describes a completed whisper.cpp library installation.
type InstallRecord struct {
	Version        int          `json:"version"`
	Tag            string       `json:"tag"`
	Arch           string       `json:"arch"`
	OS             string       `json:"os"`
	Processor      string       `json:"processor"`
	Installed      time.Time    `json:"installed"`
	ManifestSHA256 string       `json:"manifest_sha256,omitempty"`
	Asset          InstallAsset `json:"asset"`
}

// WriteInstallRecord writes record into libPath.
func WriteInstallRecord(libPath string, record InstallRecord) error {
	record.Version = 1
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.WriteFile(filepath.Join(libPath, InstallRecordName), body, 0o644); err != nil {
		return fmt.Errorf("writing install record: %w", err)
	}
	return nil
}

// ReadInstallRecord reads the installation metadata in libPath.
func ReadInstallRecord(libPath string) (*InstallRecord, error) {
	body, err := os.ReadFile(filepath.Join(libPath, InstallRecordName))
	if err != nil {
		return nil, err
	}
	var record InstallRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return nil, fmt.Errorf("reading install record: %w", err)
	}
	return &record, nil
}
