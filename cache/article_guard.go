package cache

import (
	"encoding/binary"
	"sync/atomic"

	"github.com/bits-and-blooms/bloom/v3"
	"golang.org/x/sync/singleflight"
)

type ArticleGuard struct {
	bf      *bloom.BloomFilter
	warmed  atomic.Bool
	loader  singleflight.Group
}

func NewArticleGuard(expected uint, falsePositiveRate float64) *ArticleGuard {
	if expected == 0 {
		expected = 1_000_000
	}
	if falsePositiveRate <= 0 || falsePositiveRate >= 1 {
		falsePositiveRate = 0.01
	}
	return &ArticleGuard{bf: bloom.NewWithEstimates(expected, falsePositiveRate)}
}

func (g *ArticleGuard) Warm(ids []uint64) {
	for _, id := range ids {
		g.Add(id)
	}
	g.warmed.Store(true)
}

func (g *ArticleGuard) IsWarmed() bool {
	return g.warmed.Load()
}

func (g *ArticleGuard) Add(id uint64) {
	g.bf.Add(uint64Bytes(id))
}

func (g *ArticleGuard) MightExist(id uint64) bool {
	if !g.IsWarmed() {
		return true
	}
	return g.bf.Test(uint64Bytes(id))
}

func (g *ArticleGuard) DoLoad(key string, fn func() (interface{}, error)) (interface{}, error) {
	v, err, _ := g.loader.Do(key, fn)
	return v, err
}

func uint64Bytes(v uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, v)
	return buf
}
