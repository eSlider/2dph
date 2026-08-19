// bin/brain/model.go - fetch potion-multilingual-128M for the Go brain.
//
//	./bin/brain/model.go                 # into $HF_HOME (default ~/.cache/huggingface)
//	./bin/brain/model.go --to /tmp/m     # explicit target dir
//
// Pure Go (net/http + os), no cgo, no python. Replaces the old
// model2vec download step so the whole model path stays in Go:
// writes tokenizer.json + model.safetensors into the HF cache layout
// that internal/brain/modeldir.go resolves
// ($HF_HOME/hub/models--minishlab--potion-multilingual-128M/snapshots/*).
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	repo       = "minishlab/potion-multilingual-128M"
	baseURL    = "https://huggingface.co/" + repo + "/resolve/main/"
	hubDirName = "models--minishlab--potion-multilingual-128M"
)

var files = []string{"tokenizer.json", "model.safetensors"}

func main() {
	to := flag.String("to", defaultTarget(), "target directory for the model files")
	flag.Parse()

	if err := os.MkdirAll(*to, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "model: mkdir:", err)
		os.Exit(2)
	}

	client := &http.Client{Timeout: 15 * time.Minute}
	for _, name := range files {
		dst := filepath.Join(*to, name)
		if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
			fmt.Printf("model: %s already present (%d bytes)\n", name, st.Size())
			continue
		}
		if err := download(client, baseURL+name, dst); err != nil {
			fmt.Fprintln(os.Stderr, "model:", name, err)
			os.Exit(1)
		}
		fmt.Printf("model: %s -> %s\n", name, dst)
	}
	fmt.Println("model: OK at", *to)
}

func defaultTarget() string {
	hf := os.Getenv("HF_HOME")
	if hf == "" {
		if hd, err := os.UserHomeDir(); err == nil {
			hf = filepath.Join(hd, ".cache", "huggingface")
		}
	}
	if hf != "" {
		return filepath.Join(hf, "hub", hubDirName, "snapshots", "main")
	}
	if root := os.Getenv("KB_ROOT"); root != "" {
		return filepath.Join(root, "models", "potion-multilingual-128m")
	}
	return filepath.Join("models", "potion-multilingual-128m")
}

func download(client *http.Client, url, dst string) error {
	part := dst + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	defer out.Close()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "2dph-brain-model/1 (go)")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(part, dst)
}