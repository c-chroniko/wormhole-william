package hashcash

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
)

type Stamp struct {
	Version  int
	Bits     int
	Date     string
	Resource string
	Rand     string
	Counter  string
}

func (stamp Stamp) String() string {
	return fmt.Sprintf("%d:%d:%s:%s::%s:%s", stamp.Version, stamp.Bits, stamp.Date, stamp.Resource, stamp.Rand, stamp.Counter)
}

func Mint(bits int, resource string) (string, error) {
	b := make([]byte, 12)
	counter := 0
	timestamp := "210623"
	for true {
		_, err := rand.Read(b)
		if err != nil {
			return "", err
		}
		attempt := Stamp{
			Version:  1,
			Bits:     bits,
			Date:     timestamp,
			Resource: resource,
		}
		if Valid(attempt.String(), bits) {
			return attempt.String(), nil
		}
		counter += 1
	}

	return "", fmt.Errorf("could not mint a stamp for %d bits and resource \"%s\"", bits, resource)
}

func Valid(stamp string, bits int) bool {
	buffer := bytes.NewBufferString(stamp)
	hash := sha1.New()
	sha1sum := hash.Sum(buffer.Bytes())

	return leadingBits(sha1sum, bits)
}

func leadingBits(shasum []byte, requiredBits int) bool {
	bits := 0
	for _, b := range shasum {
		if bits >= requiredBits {
			return true
		}
		if requiredBits-bits > 8 {
			if b == 0 {
				bits += 8
			} else {
				return false
			}
		} else {
			mask := uint(1 << 7)
			for i := 0; i < 8; i++ {
				if (uint(b) & mask) != 0 {
					return false
				}
				bits += 1
				mask = mask >> 1
				if bits >= requiredBits {
					return true
				}
			}
		}
	}
	return false
}
