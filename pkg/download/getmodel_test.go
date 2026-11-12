package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Configure the second client while the first is waiting for its HEAD response.
// The first must retain its own progress listener even after that configuration.
func TestConcurrentDownloads(t *testing.T) {
	for _, library := range []bool{false, true} {
		t.Run(fmt.Sprintf("library=%t", library), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			firstStarted := make(chan struct{})
			secondStarted := make(chan struct{})
			firstDone := make(chan struct{})
			firstName := "first.bin"
			firstBody := []byte("first model contents")
			if library {
				firstName = "whisper-v1.9.5-bin-ubuntu-cpu-x64.tar.gz"
				firstBody = linuxArchive(t, map[string]string{"libwhisper.so": "first library contents"}, nil)
			}
			manifest := manifestJSON(t, "v1.9.5", firstName, sumBytes(firstBody), map[string]string{"libwhisper.so": sumString("first library contents")}, nil)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/digests/") {
					_, _ = w.Write(manifest)
					return
				}
				first := filepath.Base(r.URL.Path) == firstName
				if r.Method == http.MethodHead {
					wait := firstDone
					if first {
						close(firstStarted)
						wait = secondStarted
					} else {
						close(secondStarted)
					}
					select {
					case <-wait:
					case <-ctx.Done():
						return
					}
				}
				body := []byte("second model has different bytes")
				if first {
					body = firstBody
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				if r.Method != http.MethodHead {
					_, _ = w.Write(body)
				}
			}))
			defer server.Close()
			restoreDownloadURLs(t, server.URL)
			firstDir, secondDir := t.TempDir(), t.TempDir()
			results := make(chan error, 2)
			go func() {
				defer close(firstDone)
				progress := downloadProgressCheck{t, firstName}
				if library {
					results <- GetWithContext(ctx, "amd64", "linux", "cpu", "v1.9.5", firstDir, progress)
					return
				}
				results <- GetModelWithContext(ctx, server.URL+"/"+firstName, firstDir, progress)
			}()
			select {
			case <-firstStarted:
			case err := <-results:
				t.Fatalf("first download stopped before HEAD: %v", err)
			case <-ctx.Done():
				t.Fatal("first download did not start")
			}
			go func() {
				results <- GetModelWithContext(ctx, server.URL+"/second.bin", secondDir, downloadProgressCheck{t, "second.bin"})
			}()
			for range 2 {
				if err := <-results; err != nil {
					t.Errorf("download: %v", err)
				}
			}
			firstFile, firstWant := firstName, string(firstBody)
			if library {
				firstFile, firstWant = "libwhisper.so", "first library contents"
			}
			for path, want := range map[string]string{
				filepath.Join(firstDir, firstFile):     firstWant,
				filepath.Join(secondDir, "second.bin"): "second model has different bytes",
			} {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != want {
					t.Errorf("ReadFile(%s): got %q, %v; want %q", path, got, err, want)
				}
			}
		})
	}
}

type downloadProgressCheck struct {
	t    *testing.T
	want string
}

func (p downloadProgressCheck) TrackProgress(src string, _, _ int64, stream io.ReadCloser) io.ReadCloser {
	if src != p.want {
		p.t.Errorf("progress source: got %q, want %q", src, p.want)
	}
	return stream
}

func TestDefaultProgressTrackerIndependentStreams(t *testing.T) {
	tracker := DefaultProgressTracker()
	streams := []struct {
		stream io.ReadCloser
		want   string
	}{
		{tracker.TrackProgress("first", 0, 5, io.NopCloser(strings.NewReader("first"))), "first"},
		{tracker.TrackProgress("second", 0, 6, io.NopCloser(strings.NewReader("second"))), "second"},
	}
	for _, test := range streams {
		got, err := io.ReadAll(test.stream)
		if err != nil || string(got) != test.want {
			t.Errorf("ReadAll: got %q, %v; want %q", got, err, test.want)
		}
		if err := test.stream.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}
}

// TestGetModelSuccess drives getModel against an in-process server and
// verifies the bytes land on disk intact.
//
// getModel is called in production from cmd/model.go with `dest` set to
// the parent directory (e.g. ~/models), and go-getter writes the URL's
// basename into that directory under ClientModeAny. The test mirrors
// that contract: pass a temp dir as dest, then read the basename back.
func TestGetModelSuccess(t *testing.T) {
	payload := []byte("hello whisper")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "13")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	if err := getModel(context.Background(), srv.URL+"/ggml-fake.bin", destDir, nil); err != nil {
		t.Fatalf("getModel: %v", err)
	}

	target := filepath.Join(destDir, "ggml-fake.bin")
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !reflect.DeepEqual(got, payload) {
		t.Errorf("payload: got %q, want %q", got, payload)
	}
}

// TestGetModelContextCanceled drives getModel against a server that blocks
// forever, cancels the context mid-flight, and verifies the call aborts.
// A bug that severs ctx propagation through go-getter would block this
// test until the test framework's deadline expires; that is the
// regression we want to fast-fail.
func TestGetModelContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	destDir := t.TempDir()

	done := make(chan error, 1)
	go func() { done <- getModel(ctx, srv.URL+"/slow.bin", destDir, nil) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("getModel: expected error after ctx cancel, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("getModel: did not return within 5s after ctx cancel")
	}
}

// TestGetModelHTTPError verifies an upstream 404 surfaces as an error
// rather than silently writing an HTML body to the target path.
func TestGetModelHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	if err := getModel(context.Background(), srv.URL+"/missing.bin", destDir, nil); err == nil {
		t.Fatal("getModel: expected error on 404, got nil")
	}
}
