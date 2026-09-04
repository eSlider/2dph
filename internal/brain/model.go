//go:build cgo && system_ladybug

// StaticModel wraps the potion-multilingual-128m embedding model.
//
// Mirrors model2vec.StaticModel: tokenizer (daulet Unigram) + safetensors matrix.
// Embed(text) applies the same preprocessing: median_token_length pre-truncation,
// add_special_tokens=false, drop unk (id=1), truncate to 512, mean pool, L2 normalize +1e-32.
package brain

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/chewxy/math32"
	"github.com/daulet/tokenizers"
)

type StaticModel struct {
	tok       *tokenizers.Tokenizer
	mat       []float32 // row-major: vocab_size x 128
	medianLen int
	vocabSize int
	dim       int
}

// LoadModel loads potion-multilingual-128M (same preprocess as search).
func LoadModel() (*StaticModel, error) {
	return loadModel()
}

func loadModel() (*StaticModel, error) {
	dir, err := modelDir()
	if err != nil {
		return nil, err
	}

	tok, err := tokenizers.FromFile(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, fmt.Errorf("tokenizer: %w", err)
	}

	mat, vocabSize, dim, err := loadMatrix(filepath.Join(dir, "model.safetensors"))
	if err != nil {
		return nil, fmt.Errorf("safetensors: %w", err)
	}

	median := medianTokenLength(filepath.Join(dir, "tokenizer.json"))

	return &StaticModel{
		tok:       tok,
		mat:       mat,
		vocabSize: vocabSize,
		dim:       dim,
		medianLen: median,
	}, nil
}

func (m *StaticModel) Close() error {
	if m.tok != nil {
		m.tok.Close()
		m.tok = nil
	}
	return nil
}

func (m *StaticModel) Embed(text string) ([]float64, error) {
	const maxLen = 512

	if m.medianLen > 0 {
		maxChars := maxLen * m.medianLen
		runes := []rune(text)
		if len(runes) > maxChars {
			text = string(runes[:maxChars])
		}
	}

	ids, _, err := m.tok.EncodeErr(text, false)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}

	filtered := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if id != 1 {
			filtered = append(filtered, id)
		}
		if len(filtered) >= maxLen {
			break
		}
	}
	if len(filtered) == 0 {
		return make([]float64, m.dim), nil
	}

	acc := make([]float32, m.dim)
	for _, id := range filtered {
		if int(id) >= m.vocabSize {
			continue
		}
		off := int(id) * m.dim
		for d := 0; d < m.dim; d++ {
			acc[d] += m.mat[off+d]
		}
	}
	inv := 1.0 / float32(len(filtered))
	for d := 0; d < m.dim; d++ {
		acc[d] *= inv
	}

	var norm float32
	for d := 0; d < m.dim; d++ {
		norm += acc[d] * acc[d]
	}
	norm = math32.Sqrt(norm) + 1e-32
	for d := 0; d < m.dim; d++ {
		acc[d] /= norm
	}

	out := make([]float64, m.dim)
	for d := 0; d < m.dim; d++ {
		out[d] = float64(acc[d])
	}
	return out, nil
}

func medianTokenLength(tokenizerPath string) int {
	data, err := os.ReadFile(tokenizerPath)
	if err != nil {
		return 0
	}
	var parsed struct {
		Model struct {
			Vocab [][]json.RawMessage `json:"vocab"`
		} `json:"model"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0
	}
	vocab := parsed.Model.Vocab
	if len(vocab) == 0 {
		return 0
	}
	lengths := make([]int, 0, len(vocab))
	for _, pair := range vocab {
		if len(pair) < 1 {
			continue
		}
		var tok string
		if err := json.Unmarshal(pair[0], &tok); err != nil {
			continue
		}
		lengths = append(lengths, len([]rune(tok)))
	}
	if len(lengths) == 0 {
		return 0
	}
	sort.Ints(lengths)
	return lengths[len(lengths)/2]
}

func loadMatrix(path string) ([]float32, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()

	var hdrLen uint64
	if err := binaryRead(f, &hdrLen); err != nil {
		return nil, 0, 0, err
	}
	hdrBytes := make([]byte, hdrLen)
	if _, err := io.ReadFull(f, hdrBytes); err != nil {
		return nil, 0, 0, err
	}

	var hdr struct {
		Embeddings struct {
			Dtype  string   `json:"dtype"`
			Shape  []int    `json:"shape"`
			Offset []uint64 `json:"data_offsets"`
		} `json:"embeddings"`
	}
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return nil, 0, 0, err
	}
	if hdr.Embeddings.Dtype != "F32" {
		return nil, 0, 0, fmt.Errorf("unsupported dtype %s", hdr.Embeddings.Dtype)
	}
	if len(hdr.Embeddings.Shape) != 2 {
		return nil, 0, 0, fmt.Errorf("expected 2D shape, got %v", hdr.Embeddings.Shape)
	}
	vocabSize := hdr.Embeddings.Shape[0]
	dim := hdr.Embeddings.Shape[1]
	if len(hdr.Embeddings.Offset) != 2 {
		return nil, 0, 0, fmt.Errorf("bad offsets")
	}
	start := hdr.Embeddings.Offset[0]
	end := hdr.Embeddings.Offset[1]
	size := end - start
	if size != uint64(vocabSize*dim*4) {
		return nil, 0, 0, fmt.Errorf("size mismatch")
	}

	if _, err := f.Seek(int64(8+hdrLen+start), io.SeekStart); err != nil {
		return nil, 0, 0, err
	}

	buf := make([]byte, size)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, 0, 0, err
	}

	mat := make([]float32, vocabSize*dim)
	for i := 0; i < len(mat); i++ {
		off := i * 4
		mat[i] = math.Float32frombits(
			uint32(buf[off]) |
				uint32(buf[off+1])<<8 |
				uint32(buf[off+2])<<16 |
				uint32(buf[off+3])<<24,
		)
	}
	return mat, vocabSize, dim, nil
}

func binaryRead(r io.Reader, v any) error {
	switch p := v.(type) {
	case *uint64:
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return err
		}
		*p = uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
			uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
	}
	return nil
}