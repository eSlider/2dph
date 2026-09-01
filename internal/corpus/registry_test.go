package corpus

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eSlider/2dph/internal/contract"
)

type fakeSource struct {
	name  string
	leafs []contract.Leaf
	err   error
}

func (f fakeSource) Name() string { return f.name }
func (f fakeSource) Stream(ctx context.Context, emit func(contract.Leaf) error) error {
	if f.err != nil {
		return f.err
	}
	for _, l := range f.leafs {
		if err := emit(l); err != nil {
			return err
		}
	}
	return nil
}

// TestStreamAllOrderAndCollect — registry прогоняет адаптеры по порядку и
// собирает leafs единым emit.
func TestStreamAllOrderAndCollect(t *testing.T) {
	srcs := []contract.Source{
		fakeSource{name: "mail", leafs: []contract.Leaf{{Source: "mail", ExternalID: "1", Kind: "mail", Text: "a"}}},
		fakeSource{name: "git", leafs: []contract.Leaf{{Source: "git", ExternalID: "2", Kind: "commit", Text: "b"}}},
	}
	var got []string
	err := StreamAll(context.Background(), srcs, func(l contract.Leaf) error {
		got = append(got, l.Source)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != "[mail git]" {
		t.Errorf("order = %v, want [mail git]", got)
	}
}

// TestStreamAllPropagatesSourceError — ошибка адаптера прерывает стрим с
// именем корпуса в сообщении.
func TestStreamAllPropagatesSourceError(t *testing.T) {
	srcs := []contract.Source{
		fakeSource{name: "docs", err: errors.New("boom")},
	}
	err := StreamAll(context.Background(), srcs, func(contract.Leaf) error { return nil })
	if err == nil || !contains(err.Error(), "docs") {
		t.Errorf("err = %v, want corpus docs named", err)
	}
}

// TestStreamAllContextCancel — отмена ctx останавливает стрим.
func TestStreamAllContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := StreamAll(ctx, []contract.Source{fakeSource{name: "x", leafs: []contract.Leaf{{Source: "x", ExternalID: "1", Kind: "a", Text: "t"}}}}, func(contract.Leaf) error {
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && (func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})()))
}
