package backend

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/netproxy"
)

type componentFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type componentManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Package       string          `json:"package"`
	Version       string          `json:"version"`
	Platform      string          `json:"platform"`
	Arch          string          `json:"arch"`
	URL           string          `json:"url"`
	Integrity     string          `json:"integrity"`
	Files         []componentFile `json:"files"`
}

type nativeComponent struct {
	mu                  sync.Mutex
	manifestPath, cache string
	client              *http.Client
	recycle             func(string) error
	progress            func(*KernelCopilotRuntime)
	verified            string
	verifiedFiles       map[string]componentFileStamp
}
type componentFileStamp struct {
	Size     int64
	Modified time.Time
}

func nativeHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = netproxy.Proxy
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("cross-request redirects are not allowed")
	}}
}

// prepare acquires only the pinned official platform package. Standard-library
// tar/gzip read archive records; symlinks, traversal and undeclared files fail.
// Old installed components and credentials are never changed in place.
func (c *nativeComponent) prepare(ctx context.Context) (entryPath string, returnErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		if returnErr != nil && c.progress != nil {
			if ctx.Err() != nil {
				c.progress(nil)
			} else {
				c.progress(&KernelCopilotRuntime{Phase: "error", Error: returnErr.Error()})
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(c.manifestPath)
	if err != nil {
		return "", err
	}
	var manifest componentManifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	if manifest.SchemaVersion != 1 || manifest.Platform != "win32" || manifest.Arch != "x64" || manifest.Package != "@github/copilot-sdk-win32-x64" || !strings.HasPrefix(manifest.URL, "https://registry.npmjs.org/@github/copilot-sdk-win32-x64/-/") || !strings.HasPrefix(manifest.Integrity, "sha512-") {
		return "", errors.New("invalid pinned Copilot component")
	}
	if !filepath.IsLocal(manifest.Version) || strings.ContainsAny(manifest.Version, "/\\") || len(manifest.Files) == 0 {
		return "", errors.New("invalid component version or inventory")
	}
	declared := map[string]bool{}
	totalSize := int64(0)
	for _, file := range manifest.Files {
		if !filepath.IsLocal(file.Path) || strings.Contains(file.Path, `\`) || declared[file.Path] || file.Size < 0 || len(file.SHA256) != 64 {
			return "", errors.New("invalid component inventory")
		}
		declared[file.Path] = true
		totalSize += file.Size
	}
	if !declared["prebuilds/win32-x64/copilot-runtime.exe"] || totalSize > 1<<30 {
		return "", errors.New("invalid component entry or size")
	}
	identity := sha256.Sum256([]byte(manifest.Integrity))
	directory := filepath.Join(c.cache, fmt.Sprintf("win32-x64-%s-%x", manifest.Version, identity[:8]))
	entry := filepath.Join(directory, "prebuilds", "win32-x64", "copilot-runtime.exe")
	if c.verified == entry {
		unchanged := len(c.verifiedFiles) == len(manifest.Files)
		for _, file := range manifest.Files {
			info, err := os.Lstat(filepath.Join(directory, filepath.FromSlash(file.Path)))
			stamp := c.verifiedFiles[file.Path]
			if err != nil || !info.Mode().IsRegular() || stamp.Size != info.Size() || !stamp.Modified.Equal(info.ModTime()) {
				unchanged = false
				break
			}
		}
		if unchanged {
			return entry, nil
		}
	}
	verify := func() error {
		stamps := map[string]componentFileStamp{}
		for _, file := range manifest.Files {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !filepath.IsLocal(file.Path) || strings.Contains(file.Path, `\`) {
				return errors.New("invalid component file path")
			}
			path := filepath.Join(directory, filepath.FromSlash(file.Path))
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || info.Size() != file.Size {
				return errors.New("Copilot cached file differs")
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, in)
			in.Close()
			if err != nil {
				return err
			}
			if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
				return errors.New("Copilot cached checksum differs")
			}
			stamps[file.Path] = componentFileStamp{info.Size(), info.ModTime()}
		}
		c.verifiedFiles = stamps
		return nil
	}
	if _, err := os.Stat(directory); err == nil {
		if err := verify(); err == nil {
			c.verified = entry
			return entry, nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := c.recycle(directory); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(c.cache, "_temp"), 0700); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(filepath.Join(c.cache, "_temp"), "download-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err := c.recycle(work); err != nil {
			fmt.Fprintln(os.Stderr, "Copilot download files were retained:", err)
		}
	}()
	status := &KernelCopilotRuntime{Phase: "downloading"}
	notify := func() {
		if c.progress != nil {
			copy := *status
			c.progress(&copy)
		}
	}
	notify()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.URL, nil)
	if err != nil {
		return "", err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("Copilot component download failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Copilot component download: HTTP %d", response.StatusCode)
	}
	status.Total = response.ContentLength
	lastProgress := time.Time{}
	archive := filepath.Join(work, "runtime.tgz")
	out, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	hash := sha512.New()
	buffer := make([]byte, 128<<10)
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			if _, err := out.Write(buffer[:count]); err != nil {
				out.Close()
				return "", err
			}
			hash.Write(buffer[:count])
			status.Received += int64(count)
			if status.Received > totalSize+(1<<20) {
				out.Close()
				return "", errors.New("Copilot download exceeds its registered size")
			}
			if time.Since(lastProgress) >= 200*time.Millisecond {
				notify()
				lastProgress = time.Now()
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				out.Close()
				return "", readErr
			}
			break
		}
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	status.Phase = "verifying"
	notify()
	if "sha512-"+base64.StdEncoding.EncodeToString(hash.Sum(nil)) != manifest.Integrity {
		return "", errors.New("Copilot component integrity check failed")
	}
	in, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer in.Close()
	gzipReader, err := gzip.NewReader(in)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	staging := filepath.Join(work, "package")
	if err := os.Mkdir(staging, 0700); err != nil {
		return "", err
	}
	expected := make(map[string]componentFile)
	for _, file := range manifest.Files {
		expected[file.Path] = file
	}
	seen := make(map[string]bool)
	status.Phase = "extracting"
	notify()
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		name, ok := strings.CutPrefix(header.Name, "package/")
		if !ok || !filepath.IsLocal(name) || strings.Contains(name, `\`) {
			return "", errors.New("unsafe Copilot archive entry")
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		file, ok := expected[name]
		if !ok || seen[name] || header.Typeflag != tar.TypeReg || file.Size != header.Size {
			return "", errors.New("unexpected Copilot archive file")
		}
		path := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", err
		}
		sum := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(output, sum), reader)
		closeErr := output.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return "", err
		}
		if hex.EncodeToString(sum.Sum(nil)) != file.SHA256 {
			return "", errors.New("Copilot archive file checksum differs")
		}
		seen[name] = true
	}
	if len(seen) != len(expected) {
		return "", errors.New("Copilot archive is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(staging, directory); err != nil {
		return "", err
	}
	if err := verify(); err != nil {
		return "", err
	}
	c.verified = entry
	if c.progress != nil {
		c.progress(nil)
	}
	return entry, nil
}
