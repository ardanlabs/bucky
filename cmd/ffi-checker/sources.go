package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const upstream = "ggml-org/whisper.cpp"

const versionURL = "https://ardanlabs.github.io/bucky-builder/version.json"

var requiredHeaders = []headerSpec{
	{Name: "whisper.h", Path: "include/whisper.h", APIMacros: []string{"WHISPER_API"}},
	{Name: "ggml.h", Path: "ggml/include/ggml.h", APIMacros: []string{"GGML_API"}},
	{Name: "ggml-backend.h", Path: "ggml/include/ggml-backend.h", APIMacros: []string{"GGML_API", "GGML_BACKEND_API"}},
	{Name: "ggml-cpu.h", Path: "ggml/include/ggml-cpu.h", APIMacros: []string{"GGML_BACKEND_API"}},
}

type headerSpec struct {
	Name      string
	Path      string
	APIMacros []string
}

type Header struct {
	Name      string
	Source    string
	APIMacros []string
}

func resolveVersion() (string, error) {
	version, err := fetchVersion(versionURL)
	if err != nil {
		return "", fmt.Errorf("cannot resolve current bucky-builder release: %w", err)
	}
	return version, nil
}

func fetchVersion(url string) (string, error) {
	body, err := httpGet(url)
	if err != nil {
		return "", err
	}
	var response struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}
	if response.TagName == "" {
		return "", fmt.Errorf("response has no tag_name")
	}
	return response.TagName, nil
}

func obtainHeaders(version string) ([]Header, string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	dir := filepath.Join(cache, "bucky-ffi-checker", "headers", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}

	cached := true
	headers := make([]Header, 0, len(requiredHeaders))
	for _, spec := range requiredHeaders {
		path := filepath.Join(dir, spec.Name)
		body, err := os.ReadFile(path)
		if err != nil {
			cached = false
			url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", upstream, version, spec.Path)
			body, err = httpGet(url)
			if err != nil {
				return nil, "", fmt.Errorf("fetch %s: %w", spec.Name, err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				return nil, "", err
			}
		}
		headers = append(headers, Header{Name: spec.Name, Source: string(body), APIMacros: spec.APIMacros})
	}

	source := upstream + "@" + version
	if cached {
		source += ", cached"
	}
	return headers, source, nil
}

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}
