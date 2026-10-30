package screenshot

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"

	"github.com/ericpauley/go-quantize/quantize"
)

// Quantize rewrites src as a 256-colour palette PNG with Floyd-Steinberg
// dithering (pngquant replacement). Returns the new size. Keeps the original
// if the result is not smaller.
func Quantize(src, dst string) (int64, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return 0, err
	}
	orig, _ := os.Stat(src)

	q := quantize.MedianCutQuantizer{AddTransparent: false}
	pal := q.Quantize(make(color.Palette, 0, 256), img)
	if len(pal) == 0 {
		return 0, errors.New("empty palette")
	}
	out := image.NewPaletted(img.Bounds(), pal)
	draw.FloydSteinberg.Draw(out, img.Bounds(), img, image.Point{})

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, out); err != nil {
		return 0, err
	}
	if orig != nil && int64(buf.Len()) >= orig.Size() && src == dst {
		return orig.Size(), nil
	}
	if err := os.WriteFile(dst+".tmp", buf.Bytes(), 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(dst+".tmp", dst); err != nil {
		return 0, err
	}
	return int64(buf.Len()), nil
}

// Uniquify appends a random tEXt chunk so image hosts that dedupe by hash
// (picgo.net) treat every upload as a new image. Replaces the ImageMagick
// "-set comment" step from bd.sh.
func Uniquify(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(b) < 8 || !bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return errors.New("not a PNG")
	}
	// Find IEND (last 12 bytes) and insert before it.
	iend := bytes.LastIndex(b, []byte("IEND"))
	if iend < 4 {
		return errors.New("IEND not found")
	}
	start := iend - 4
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	data := append([]byte("seedkit\x00"), []byte(hex.EncodeToString(nonce))...)
	chunk := make([]byte, 0, 12+len(data))
	chunk = binary.BigEndian.AppendUint32(chunk, uint32(len(data)))
	chunk = append(chunk, []byte("tEXt")...)
	chunk = append(chunk, data...)
	crc := crc32.ChecksumIEEE(chunk[4:])
	chunk = binary.BigEndian.AppendUint32(chunk, crc)

	out := make([]byte, 0, len(b)+len(chunk))
	out = append(out, b[:start]...)
	out = append(out, chunk...)
	out = append(out, b[start:]...)
	return os.WriteFile(path, out, 0o644)
}

var _ = io.EOF
