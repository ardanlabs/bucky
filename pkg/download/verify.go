package download

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var (
	// ErrNoInstallRecord means the library directory has no installation metadata.
	ErrNoInstallRecord = errors.New("no install record")
	// ErrNoFileDigests means no extracted-file metadata is available for verification.
	ErrNoFileDigests = errors.New("the digests of this release do not cover files")
	// ErrRecordMismatch means the record does not describe the requested release asset.
	ErrRecordMismatch = errors.New("the install record does not agree")
)

// FileState describes the integrity state of an installed path.
type FileState int

const (
	// FileVerified means the path agrees with expected metadata.
	FileVerified FileState = iota
	// FileChanged means the path exists but differs from expected metadata.
	FileChanged
	// FileMissing means an expected path does not exist.
	FileMissing
	// FileUnexpected means a path is not part of the selected asset.
	FileUnexpected
)

// String returns the state name.
func (s FileState) String() string {
	switch s {
	case FileVerified:
		return "verified"
	case FileChanged:
		return "changed"
	case FileMissing:
		return "missing"
	case FileUnexpected:
		return "unexpected"
	default:
		return "unknown"
	}
}

// MarshalJSON writes a state as its name.
func (s FileState) MarshalJSON() ([]byte, error) { return []byte(`"` + s.String() + `"`), nil }

// FileReport describes the verification result for one path.
type FileReport struct {
	Name  string    `json:"name"`
	State FileState `json:"state"`
}

// VerifyReport describes an installed bundle's integrity.
type VerifyReport struct {
	Tag                   string       `json:"tag"`
	LibPath               string       `json:"lib_path"`
	ManifestAuthenticated bool         `json:"manifest_authenticated"`
	Source                string       `json:"source"`
	Files                 []FileReport `json:"files"`
	Verified              int          `json:"verified"`
	Changed               int          `json:"changed"`
	Missing               int          `json:"missing"`
	Unexpected            int          `json:"unexpected"`
}

// OK reports whether the directory contains exactly the expected paths and
// every path is unchanged.
func (r *VerifyReport) OK() bool {
	return r.Changed == 0 && r.Missing == 0 && r.Unexpected == 0
}

// VerifyInstall checks libPath against publisher metadata or its local install record.
// An empty version performs an offline check and does not claim external authenticity.
// A pinned version refetches and authenticates the raw publisher manifest.
func VerifyInstall(ctx context.Context, libPath, version string) (*VerifyReport, error) {
	tag, pin, err := ParsePinnedVersion(version)
	if err != nil {
		return nil, err
	}
	record, err := ReadInstallRecord(libPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w in %s", ErrNoInstallRecord, libPath)
		}
		return nil, err
	}

	wantFiles, wantLinks := record.Asset.Files, record.Asset.Links
	report := &VerifyReport{Tag: record.Tag, LibPath: libPath, Source: "install-record"}
	if tag != "" {
		arch, err := ParseArch(record.Arch)
		if err != nil {
			return nil, ErrUnknownArch
		}
		operatingSystem, err := ParseOS(record.OS)
		if err != nil {
			return nil, ErrUnknownOS
		}
		processor, err := ParseProcessor(record.Processor)
		if err != nil {
			return nil, ErrUnknownProcessor
		}
		location, filename, err := getDownloadLocationAndFilename(arch, operatingSystem, processor, tag)
		if err != nil {
			return nil, err
		}
		assetURL := location + "/" + filename
		m, _, err := fetchManifest(ctx, tag, pin)
		if err != nil {
			return nil, err
		}
		entry, ok := m.assetFor(assetURL)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrRecordMismatch, assetURL)
		}
		wantFiles, wantLinks = entry.Files, entry.Links
		report.Tag = tag
		report.Source = "publisher-manifest"
		report.ManifestAuthenticated = pin != ""
	}
	if len(wantFiles) == 0 && len(wantLinks) == 0 {
		return nil, ErrNoFileDigests
	}

	seen := make(map[string]bool)
	err = filepath.WalkDir(libPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(libPath, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if name == InstallRecordName {
			return nil
		}
		seen[name] = true
		if entry.Type()&fs.ModeSymlink != 0 {
			want, ok := wantLinks[name]
			if !ok {
				if _, shouldBeFile := wantFiles[name]; shouldBeFile {
					report.add(name, FileChanged)
				} else {
					report.add(name, FileUnexpected)
				}
				return nil
			}
			got, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if got != want {
				report.add(name, FileChanged)
			} else {
				report.add(name, FileVerified)
			}
			return nil
		}
		want, ok := wantFiles[name]
		if !ok {
			if _, shouldBeLink := wantLinks[name]; shouldBeLink {
				report.add(name, FileChanged)
			} else {
				report.add(name, FileUnexpected)
			}
			return nil
		}
		got, err := hashFile(path)
		if err != nil {
			return err
		}
		if strings.EqualFold(got, want) {
			report.add(name, FileVerified)
		} else {
			report.add(name, FileChanged)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for name := range wantFiles {
		if !seen[name] {
			report.add(name, FileMissing)
		}
	}
	for name := range wantLinks {
		if !seen[name] {
			report.add(name, FileMissing)
		}
	}
	slices.SortFunc(report.Files, func(a, b FileReport) int { return strings.Compare(a.Name, b.Name) })
	return report, nil
}

func (r *VerifyReport) add(name string, state FileState) {
	r.Files = append(r.Files, FileReport{Name: name, State: state})
	switch state {
	case FileVerified:
		r.Verified++
	case FileChanged:
		r.Changed++
	case FileMissing:
		r.Missing++
	case FileUnexpected:
		r.Unexpected++
	}
}

func hashFile(path string) (string, error) {
	return fileSHA256(path)
}
