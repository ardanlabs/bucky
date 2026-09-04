package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrDigestMismatch means downloaded bytes do not match their published digest.
	ErrDigestMismatch = errors.New("digest does not match")
	// ErrDigestMissing means the manifest does not contain the selected asset.
	ErrDigestMissing = errors.New("no digest for asset")
	// ErrInvalidDigest means a version pin is not sha256 followed by 64 hexadecimal characters.
	ErrInvalidDigest = errors.New("invalid digest")
)

var digestsURL = "https://ardanlabs.github.io/bucky-builder/digests/%s.json"

var sha256Pattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// ParsePinnedVersion splits VERSION@sha256:DIGEST into its bare tag and digest.
// An unpinned version is returned unchanged with an empty digest.
func ParsePinnedVersion(version string) (tag string, digest string, err error) {
	tag, rest, found := strings.Cut(version, "@")
	if !found {
		return version, "", nil
	}

	algorithm, value, found := strings.Cut(rest, ":")
	switch {
	case !found:
		return "", "", fmt.Errorf("%w: want sha256:<digest>", ErrInvalidDigest)
	case strings.ToLower(algorithm) != "sha256":
		return "", "", fmt.Errorf("%w: unknown algorithm %q", ErrInvalidDigest, algorithm)
	case !sha256Pattern.MatchString(value):
		return "", "", fmt.Errorf("%w: digest is not 64 hexadecimal characters", ErrInvalidDigest)
	case tag == "" || tag == "latest":
		return "", "", fmt.Errorf("%w: a digest needs an exact version", ErrInvalidVersion)
	}
	if err := VersionIsValid(tag); err != nil {
		return "", "", err
	}

	return tag, strings.ToLower(value), nil
}

type manifest struct {
	Version int                       `json:"version"`
	Tag     string                    `json:"tag"`
	Sources map[string]manifestSource `json:"sources"`
}

type manifestSource struct {
	Tag    string                   `json:"tag"`
	Assets map[string]manifestAsset `json:"assets"`
}

type manifestAsset struct {
	SHA256 string            `json:"sha256"`
	Files  map[string]string `json:"files,omitempty"`
	Links  map[string]string `json:"links,omitempty"`
}

func (m *manifest) assetFor(assetURL string) (manifestAsset, bool) {
	u, err := url.Parse(assetURL)
	if err != nil {
		return manifestAsset{}, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 6 || parts[len(parts)-3] != "download" {
		return manifestAsset{}, false
	}
	repo := strings.Join(parts[:2], "/")
	source, ok := m.Sources[repo]
	if !ok || source.Tag != parts[len(parts)-2] {
		return manifestAsset{}, false
	}
	asset, ok := source.Assets[path.Base(u.Path)]
	return asset, ok
}

const maxManifestSize = 8 << 20

func fetchManifest(ctx context.Context, tag, want string) (*manifest, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(digestsURL, tag), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("received status code %d for the digests of %s", resp.StatusCode, tag)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxManifestSize {
		return nil, "", fmt.Errorf("the digests of %s are too large", tag)
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if want != "" && !strings.EqualFold(got, want) {
		return nil, "", fmt.Errorf("%w for the digests of %s: expected %s, got %s", ErrDigestMismatch, tag, want, got)
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, "", err
	}
	if m.Version != 1 || m.Tag != tag {
		return nil, "", fmt.Errorf("invalid digest manifest for %s", tag)
	}
	return &m, got, nil
}

func verifyFile(filename, want string) error {
	got, err := fileSHA256(filename)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%w for %s: expected %s, got %s", ErrDigestMismatch, filename, want, got)
	}
	return nil
}

func fileSHA256(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
