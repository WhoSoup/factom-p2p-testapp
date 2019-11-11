package counter

import (
	"bytes"
	"encoding/binary"
)

const (
	MsgNoMessage uint32 = iota
	MsgIncrease
)

const Delim = 'ß'

func NewIncrease(instanceid string, count uint64) []byte {
	buf := new(IntBuffer) // initial size of 64 bytes
	buf.WriteUint32(MsgIncrease)
	buf.WriteUint64(count)
	buf.WriteString(instanceid)
	buf.WriteByte(Delim)

	size := rng.Intn(4096)
	junk := make([]byte, size)
	rng.Read(junk)
	buf.Write(junk)

	return buf.Bytes()
}

type IntBuffer struct {
	bytes.Buffer
}

func (i *IntBuffer) WriteUint64(n uint64) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, n)
	i.Write(buf)
}

func (i *IntBuffer) ReadUint64() (uint64, error) {
	buf := make([]byte, 8)
	_, err := i.Read(buf)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(buf), nil
}

func (i *IntBuffer) WriteUint32(n uint32) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, n)
	i.Write(buf)
}

func (i *IntBuffer) ReadUint32() (uint32, error) {
	buf := make([]byte, 4)
	_, err := i.Read(buf)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(buf), nil
}
