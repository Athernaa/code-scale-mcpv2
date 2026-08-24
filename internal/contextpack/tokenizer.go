package contextpack

import (
	"fmt"
	"sync/atomic"

	tiktoken "github.com/tiktoken-go/tokenizer"
)

const (
	TokenizerO200K  = "o200k_base"
	TokenizerCL100K = "cl100k_base"
)

type TokenCounter interface {
	Count(string) int
	Name() string
	Exact() bool
}

type exactCounter struct {
	name  string
	codec tiktoken.Codec
	exact atomic.Bool
}

func NewTokenCounter(name string) (TokenCounter, error) {
	if name == "" {
		name = TokenizerO200K
	}
	var encoding tiktoken.Encoding
	switch name {
	case TokenizerO200K:
		encoding = tiktoken.O200kBase
	case TokenizerCL100K:
		encoding = tiktoken.Cl100kBase
	default:
		return nil, fmt.Errorf("unsupported tokenizer %q", name)
	}
	codec, err := tiktoken.Get(encoding)
	if err != nil {
		return nil, fmt.Errorf("initialize tokenizer %q: %w", name, err)
	}
	counter := &exactCounter{name: name, codec: codec}
	counter.exact.Store(true)
	return counter, nil
}

func (c *exactCounter) Count(text string) int {
	count, err := c.codec.Count(text)
	if err != nil {
		c.exact.Store(false)
		return len([]rune(text))
	}
	return count
}

func (c *exactCounter) Name() string { return c.name }
func (c *exactCounter) Exact() bool  { return c.exact.Load() }
