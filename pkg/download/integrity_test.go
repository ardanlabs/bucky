package download

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParsePinnedVersion(t *testing.T) {
	digest := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		version string
		wantTag string
		wantErr error
	}{
		{name: "plain", version: "v1.9.3", wantTag: "v1.9.3"},
		{name: "pinned", version: "v1.9.3@sha256:" + digest, wantTag: "v1.9.3"},
		{name: "unknown algorithm", version: "v1.9.3@sha512:" + digest, wantErr: ErrInvalidDigest},
		{name: "short digest", version: "v1.9.3@sha256:abcd", wantErr: ErrInvalidDigest},
		{name: "latest pin", version: "latest@sha256:" + digest, wantErr: ErrInvalidVersion},
		{name: "empty pin", version: "@sha256:" + digest, wantErr: ErrInvalidVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag, _, err := ParsePinnedVersion(tt.version)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParsePinnedVersion() error = %v, want %v", err, tt.wantErr)
			}
			if tag != tt.wantTag {
				t.Errorf("tag = %q, want %q", tag, tt.wantTag)
			}
		})
	}
}

func TestVerifyInstallOffline(t *testing.T) {
	dest := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dest, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("good.so", "good")
	write("changed.so", "changed")
	write("extra.so", "extra")
	sum := sha256.Sum256([]byte("good"))
	record := InstallRecord{
		Tag: "v1.9.3", Arch: "amd64", OS: "linux", Processor: "cpu",
		Asset: InstallAsset{Files: map[string]string{
			"good.so":    hex.EncodeToString(sum[:]),
			"changed.so": strings.Repeat("0", 64),
			"missing.so": strings.Repeat("0", 64),
		}},
	}
	if err := WriteInstallRecord(dest, record); err != nil {
		t.Fatal(err)
	}

	report, err := VerifyInstall(context.Background(), dest, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Verified != 1 || report.Changed != 1 || report.Missing != 1 || report.Unexpected != 1 {
		t.Errorf("counts = %d/%d/%d/%d, want 1/1/1/1", report.Verified, report.Changed, report.Missing, report.Unexpected)
	}
	if report.OK() {
		t.Error("OK = true, want false")
	}
	if report.ManifestAuthenticated || report.Source != "install-record" {
		t.Errorf("offline trust = %t/%q, want false/install-record", report.ManifestAuthenticated, report.Source)
	}
}

func TestVerifyInstallDetectsChangedPathTypes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires additional privileges on Windows")
	}

	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "target.so"), []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.so", filepath.Join(dest, "should-be-file.so")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "should-be-link.so"), []byte("regular"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := InstallRecord{
		Tag: "v1.9.3", Arch: "amd64", OS: "linux", Processor: "cpu",
		Asset: InstallAsset{
			Files: map[string]string{"should-be-file.so": sumString("target")},
			Links: map[string]string{"should-be-link.so": "target.so"},
		},
	}
	if err := WriteInstallRecord(dest, record); err != nil {
		t.Fatal(err)
	}

	report, err := VerifyInstall(context.Background(), dest, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Changed != 2 || report.OK() {
		t.Errorf("changed/ok = %d/%t, want 2/false", report.Changed, report.OK())
	}
}

func TestGetWithContextPinned(t *testing.T) {
	const (
		tag      = "v1.9.3"
		filename = "whisper-v1.9.3-bin-ubuntu-cpu-x64.tar.gz"
	)

	archive := linuxArchive(t, map[string]string{
		"libwhisper.so.1": "whisper",
		"libggml.so":      "ggml",
	}, nil)
	archiveSum := sumBytes(archive)

	var manifestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/digests/" + tag + ".json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(manifestBody)
		case "/ardanlabs/bucky-builder/releases/download/" + tag + "/" + filename:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manifestBody = manifestJSON(t, tag, filename, archiveSum, map[string]string{
		"libwhisper.so.1": sumString("whisper"),
		"libggml.so":      sumString("ggml"),
	}, nil)
	manifestSum := sumBytes(manifestBody)
	restoreDownloadURLs(t, server.URL)

	dest := t.TempDir()
	version := tag + "@sha256:" + manifestSum
	if err := GetWithContext(context.Background(), "amd64", "linux", "cpu", version, dest, nil); err != nil {
		t.Fatalf("GetWithContext: %v", err)
	}

	record, err := ReadInstallRecord(dest)
	if err != nil {
		t.Fatalf("ReadInstallRecord: %v", err)
	}
	if record.Tag != tag || record.ManifestSHA256 != manifestSum {
		t.Errorf("record tag/pin = %q/%q, want %q/%q", record.Tag, record.ManifestSHA256, tag, manifestSum)
	}
	if record.Asset.SHA256 != archiveSum || len(record.Asset.Files) != 2 || len(record.Asset.Links) != 0 {
		t.Errorf("record asset = %+v", record.Asset)
	}

	report, err := VerifyInstall(context.Background(), dest, version)
	if err != nil {
		t.Fatalf("VerifyInstall: %v", err)
	}
	if !report.OK() || !report.ManifestAuthenticated || report.Verified != 2 {
		t.Errorf("report = %+v", report)
	}
}

func TestGetWithContextRejectsManifestMismatchBeforeDownload(t *testing.T) {
	const tag = "v1.9.3"
	assetRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/digests/") {
			_, _ = io.WriteString(w, `{"version":1,"tag":"v1.9.3","sources":{}}`)
			return
		}
		assetRequests++
		_, _ = io.WriteString(w, "not an archive")
	}))
	defer server.Close()
	restoreDownloadURLs(t, server.URL)

	dest := filepath.Join(t.TempDir(), "libs")
	err := GetWithContext(context.Background(), "amd64", "linux", "cpu", tag+"@sha256:"+strings.Repeat("0", 64), dest, nil)
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("GetWithContext error = %v, want ErrDigestMismatch", err)
	}
	if assetRequests != 0 {
		t.Errorf("asset requests = %d, want 0", assetRequests)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("destination exists after manifest mismatch: %v", err)
	}
}

func TestGetWithContextRejectsMissingArchiveDigestBeforeDownload(t *testing.T) {
	const (
		tag      = "v1.9.3"
		filename = "whisper-v1.9.3-bin-ubuntu-cpu-x64.tar.gz"
	)
	assetRequests := 0
	var manifestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/digests/") {
			_, _ = w.Write(manifestBody)
			return
		}
		assetRequests++
		_, _ = io.WriteString(w, "not an archive")
	}))
	defer server.Close()
	manifestBody = manifestJSON(t, tag, filename, "", map[string]string{
		"libwhisper.so": sumString("whisper"),
	}, nil)
	restoreDownloadURLs(t, server.URL)

	dest := filepath.Join(t.TempDir(), "libs")
	err := GetWithContext(context.Background(), "amd64", "linux", "cpu", tag+"@sha256:"+sumBytes(manifestBody), dest, nil)
	if !errors.Is(err, ErrDigestMissing) {
		t.Fatalf("GetWithContext error = %v, want ErrDigestMissing", err)
	}
	if assetRequests != 0 {
		t.Errorf("asset requests = %d, want 0", assetRequests)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("destination exists after missing archive digest: %v", err)
	}
}

func TestGetWithContextRejectsArchiveMismatchBeforeExtraction(t *testing.T) {
	const (
		tag      = "v1.9.3"
		filename = "whisper-v1.9.3-bin-ubuntu-cpu-x64.tar.gz"
	)
	archive := linuxArchive(t, map[string]string{"libwhisper.so": "whisper"}, nil)

	var manifestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/digests/") {
			_, _ = w.Write(manifestBody)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	manifestBody = manifestJSON(t, tag, filename, strings.Repeat("0", 64), map[string]string{
		"libwhisper.so": sumString("whisper"),
	}, nil)
	restoreDownloadURLs(t, server.URL)

	dest := filepath.Join(t.TempDir(), "libs")
	err := GetWithContext(context.Background(), "amd64", "linux", "cpu", tag+"@sha256:"+sumBytes(manifestBody), dest, nil)
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("GetWithContext error = %v, want ErrDigestMismatch", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("destination exists after archive mismatch: %v", err)
	}
}

func TestGetWithContextRejectsMissingManifestForPlainVersion(t *testing.T) {
	const tag = "v1.9.3"
	assetRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/digests/") {
			http.NotFound(w, r)
			return
		}
		assetRequests++
		_, _ = io.WriteString(w, "not an archive")
	}))
	defer server.Close()
	restoreDownloadURLs(t, server.URL)

	dest := filepath.Join(t.TempDir(), "libs")
	err := GetWithContext(context.Background(), "amd64", "linux", "cpu", tag, dest, nil)
	if err == nil {
		t.Fatal("GetWithContext error = nil, want missing manifest error")
	}
	if assetRequests != 0 {
		t.Errorf("asset requests = %d, want 0", assetRequests)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("destination exists after missing manifest: %v", err)
	}
}

func restoreDownloadURLs(t *testing.T, serverURL string) {
	t.Helper()
	oldDigestsURL := digestsURL
	oldReleaseURL := buckyBuilderReleaseURL
	digestsURL = serverURL + "/digests/%s.json"
	buckyBuilderReleaseURL = serverURL + "/ardanlabs/bucky-builder/releases/download/%s"
	t.Cleanup(func() {
		digestsURL = oldDigestsURL
		buckyBuilderReleaseURL = oldReleaseURL
	})
}

func manifestJSON(t *testing.T, tag, filename, archiveSHA string, files, links map[string]string) []byte {
	t.Helper()
	body, err := json.Marshal(manifest{
		Version: 1,
		Tag:     tag,
		Sources: map[string]manifestSource{
			BuckyBuilderRepo: {
				Tag: tag,
				Assets: map[string]manifestAsset{
					filename: {SHA256: archiveSHA, Files: files, Links: links},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func linuxArchive(t *testing.T, files, links map[string]string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		header := &tar.Header{Name: "whisper-v1.9.3/" + name, Mode: 0o755, Size: int64(len(body))}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range links {
		header := &tar.Header{Name: "whisper-v1.9.3/" + name, Linkname: target, Typeflag: tar.TypeSymlink, Mode: 0o777}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func sumString(value string) string { return sumBytes([]byte(value)) }

func sumBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
