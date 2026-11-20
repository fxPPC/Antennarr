// Command runtime verifies and saves the original Wolfi package-installation
// layer. The two release assets remain usable if older Docker Hub images expire.
package main

import (
	"archive/tar"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

//go:embed lock.json
var lockJSON []byte

type layer struct {
	SHA256 string
	Size   int64
}

var client = &http.Client{
	Timeout: 10 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many download redirects")
		}
		if req.URL.Scheme != "https" {
			return errors.New("insecure download redirect")
		}
		if req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	},
}

func get(url, token string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s returned %s", req.URL.Host, resp.Status)
	}
	return resp.Body, nil
}

func registryBlob(l layer) (io.ReadCloser, error) {
	body, err := get("https://auth.docker.io/token?service=registry.docker.io&scope=repository:antennarr/antennarr:pull", "")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var auth struct{ Token string }
	if err = json.NewDecoder(io.LimitReader(body, 64<<10)).Decode(&auth); err != nil {
		return nil, err
	}
	if auth.Token == "" {
		return nil, errors.New("Docker Hub returned no pull token")
	}
	return get("https://registry-1.docker.io/v2/antennarr/antennarr/blobs/sha256:"+l.SHA256, auth.Token)
}

func save(r io.Reader, l layer, out string) error {
	f, err := os.CreateTemp(filepath.Dir(out), ".wolfi-runtime-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = copyVerified(f, r, l); err != nil {
		return err
	}
	if err = f.Chmod(0644); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), out)
}

func copyVerified(w io.Writer, r io.Reader, l layer) error {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(r, l.Size+1))
	if err != nil {
		return err
	}
	if n != l.Size || fmt.Sprintf("%x", h.Sum(nil)) != l.SHA256 {
		return errors.New("runtime layer does not match the locked size and SHA-256")
	}
	return nil
}

func fromCandidate(path string, l layer, out string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return errors.New("the candidate does not contain the locked runtime layer")
		}
		if err != nil {
			return err
		}
		if h.Name == "blobs/sha256/"+l.SHA256 {
			if h.Typeflag != tar.TypeReg || h.Size != l.Size {
				return errors.New("invalid OCI layer entry")
			}
			return save(tr, l, out)
		}
	}
}

func prepare(arch, cache, candidate, out string) error {
	var lock struct {
		Release   string
		Platforms map[string]layer
	}
	if err := json.Unmarshal(lockJSON, &lock); err != nil {
		return err
	}
	l, ok := lock.Platforms[arch]
	if !ok {
		return fmt.Errorf("no runtime snapshot for %q", arch)
	}
	if out == "" {
		return errors.New("an output file is required (-out)")
	}
	if candidate != "" {
		return fromCandidate(candidate, l, out)
	}
	name := "wolfi-runtime-linux-" + arch + ".tar.gz"
	f, err := os.Open(filepath.Join(cache, name))
	if err == nil {
		defer f.Close()
		source, sourceErr := f.Stat()
		target, targetErr := os.Stat(out)
		if sourceErr == nil && targetErr == nil && os.SameFile(source, target) {
			return copyVerified(io.Discard, f, l)
		}
		return save(f, l, out)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Public pulls need a short-lived, read-only registry token. Only the
	// immutable blob digest from the lock is used, never a tag or rolling index.
	r, registryErr := registryBlob(l)
	if registryErr == nil {
		registryErr = save(r, l, out)
		r.Close()
		if registryErr == nil {
			return nil
		}
	}
	r, err = get("https://github.com/fxPPC/Antennarr/releases/download/"+lock.Release+"/"+name, "")
	if err == nil {
		defer r.Close()
		err = save(r, l, out)
	}
	if err != nil {
		return fmt.Errorf("Docker Hub: %w; GitHub release asset: %w", registryErr, err)
	}
	return nil
}

func main() {
	arch := flag.String("arch", runtime.GOARCH, "Linux target architecture")
	cache := flag.String("cache", "packaging/runtime", "directory containing a saved release snapshot")
	candidate := flag.String("candidate", "", "read the layer from this tested OCI archive instead of downloading")
	out := flag.String("out", "", "destination filename")
	flag.Parse()
	if err := prepare(*arch, *cache, *candidate, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
