package counter

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// generate messages between 64 bytes and 5 MB, weighted toward smaller end

const power = 8
const Min = 64
const Max = 1024

var rng *rand.Rand

func init() {
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
}

func seed2float(seed int64) float64 {
	hash := sha256.Sum256([]byte(fmt.Sprintf("app seed %d", seed)))
	pseudo := binary.BigEndian.Uint32(hash[:4])
	return float64(pseudo) / float64(math.MaxUint32)
}

func WeightedRandom(seed int64) int64 {
	val := seed2float(seed)
	return int64(math.Floor(Min + (Max+1-Min)*(math.Pow(val, power))))
}

func Sha(msg []byte) []byte {
	tmp := sha256.Sum256(msg)
	return tmp[:]
}
