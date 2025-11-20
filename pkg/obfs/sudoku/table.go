package sudoku

import (
	"crypto/sha256"
	"encoding/binary"
	"log"
	"math/rand"
	"sort"
	"time"
)

// Table holds the encoding and decoding maps
type Table struct {
	EncodeTable [256][][4]byte
	DecodeMap   map[uint32]byte
	PaddingPool []byte
}

// NewTable initializes the obfuscation tables based on the key
func NewTable(key string) *Table {
	start := time.Now()
	t := &Table{
		DecodeMap: make(map[uint32]byte),
	}

	// Initialize padding pool
	t.PaddingPool = make([]byte, 0, 32)
	for i := 0; i < 16; i++ {
		t.PaddingPool = append(t.PaddingPool, byte(0x80+i))
		t.PaddingPool = append(t.PaddingPool, byte(0x10+i))
	}

	allGrids := GenerateAllGrids()

	h := sha256.New()
	h.Write([]byte(key))
	seed := int64(binary.BigEndian.Uint64(h.Sum(nil)[:8]))
	rng := rand.New(rand.NewSource(seed))

	shuffledGrids := make([]Grid, 288)
	copy(shuffledGrids, allGrids)
	rng.Shuffle(len(shuffledGrids), func(i, j int) {
		shuffledGrids[i], shuffledGrids[j] = shuffledGrids[j], shuffledGrids[i]
	})

	var combinations [][]int
	var combine func(int, int, []int)
	combine = func(s, k int, c []int) {
		if k == 0 {
			tmp := make([]int, len(c))
			copy(tmp, c)
			combinations = append(combinations, tmp)
			return
		}
		for i := s; i <= 16-k; i++ {
			c = append(c, i)
			combine(i+1, k-1, c)
			c = c[:len(c)-1]
		}
	}
	combine(0, 4, []int{})

	for byteVal := 0; byteVal < 256; byteVal++ {
		targetGrid := shuffledGrids[byteVal]
		for _, positions := range combinations {
			var currentHints [4]byte
			for i, pos := range positions {
				val := targetGrid[pos]
				hint := byte(((val - 1) << 5) | (uint8(pos) & 0x0F))
				currentHints[i] = hint
			}

			matchCount := 0
			for _, g := range allGrids {
				match := true
				for _, h := range currentHints {
					pos := h & 0x0F
					val := ((h >> 5) & 0x03) + 1
					if g[pos] != val {
						match = false
						break
					}
				}
				if match {
					matchCount++
					if matchCount > 1 {
						break
					}
				}
			}

			if matchCount == 1 {
				t.EncodeTable[byteVal] = append(t.EncodeTable[byteVal], currentHints)
				k := packHintsToKey(currentHints)
				t.DecodeMap[k] = byte(byteVal)
			}
		}
	}
	log.Printf("[Init] Sudoku Tables initialized in %v", time.Since(start))
	return t
}

func packHintsToKey(hints [4]byte) uint32 {
	cleanHints := [4]byte{}
	for i, h := range hints {
		cleanHints[i] = h & 0x6F
	}
	s := cleanHints[:]
	sort.Slice(s, func(i, j int) bool {
		return (s[i] & 0x0F) < (s[j] & 0x0F)
	})
	return binary.BigEndian.Uint32(s)
}
