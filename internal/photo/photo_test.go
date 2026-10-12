package photo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// The defect class this file exists to catch: a photo stored WITH the GPS position it was
// taken at (a home address), or a non-image stored because its label said "image/jpeg".
// Both leave every other test green.

const gpsMarker = "GPS-48.8584N-2.2945E-KHONG-DUOC-LUU"

func smallImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 30), uint8(y * 30), 90, 255})
		}
	}
	return img
}

// JPEGWithEXIF returns a decodable JPEG carrying an APP1 "Exif" segment and a COM segment.
func JPEGWithEXIF(t *testing.T) []byte {
	t.Helper()
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, smallImage(), nil); err != nil {
		t.Fatal(err)
	}
	raw := enc.Bytes()
	seg := func(marker byte, payload string) []byte {
		s := []byte{0xff, marker, 0, 0}
		binary.BigEndian.PutUint16(s[2:], uint16(len(payload)+2))
		return append(s, payload...)
	}
	out := append([]byte{}, raw[:2]...)
	out = append(out, seg(0xe1, "Exif\x00\x00"+gpsMarker)...)
	out = append(out, seg(0xfe, "comment "+gpsMarker)...)
	return append(out, raw[2:]...)
}

func pngChunk(typ string, data []byte) []byte {
	c := make([]byte, 8, 12+len(data))
	binary.BigEndian.PutUint32(c[0:4], uint32(len(data)))
	copy(c[4:8], typ)
	c = append(c, data...)
	crc := crc32.ChecksumIEEE(append([]byte(typ), data...))
	return binary.BigEndian.AppendUint32(c, crc)
}

func PNGWithText(t *testing.T) []byte {
	t.Helper()
	var enc bytes.Buffer
	if err := png.Encode(&enc, smallImage()); err != nil {
		t.Fatal(err)
	}
	raw := enc.Bytes()
	// IHDR is always first: 8 signature + 4 len + 4 type + 13 data + 4 crc = 33.
	out := append([]byte{}, raw[:33]...)
	out = append(out, pngChunk("tEXt", []byte("Comment\x00"+gpsMarker))...)
	out = append(out, pngChunk("eXIf", []byte("MM\x00*"+gpsMarker))...)
	return append(out, raw[33:]...)
}

func riffChunk(fourcc string, data []byte) []byte {
	c := make([]byte, 8, 8+len(data)+1)
	copy(c[0:4], fourcc)
	binary.LittleEndian.PutUint32(c[4:8], uint32(len(data)))
	c = append(c, data...)
	if len(data)%2 == 1 {
		c = append(c, 0)
	}
	return c
}

func WebPWithEXIF() []byte {
	vp8x := make([]byte, 10)
	vp8x[0] = 0x08 | 0x04 | 0x10 // EXIF + XMP + alpha
	body := []byte("WEBP")
	body = append(body, riffChunk("VP8X", vp8x)...)
	body = append(body, riffChunk("VP8L", []byte{0x2f, 1, 2, 3, 4})...)
	body = append(body, riffChunk("EXIF", []byte(gpsMarker))...)
	body = append(body, riffChunk("XMP ", []byte("<x>"+gpsMarker+"</x>"))...)
	out := []byte("RIFF\x00\x00\x00\x00")
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(body)))
	return append(out, body...)
}

func TestSniff_TrustsBytesNotLabels(t *testing.T) {
	for _, c := range []struct {
		name string
		b    []byte
		want string
		ok   bool
	}{
		{"jpeg", []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0}, TypeJPEG, true},
		{"png", append(append([]byte{}, pngSignature...), 0, 0, 0, 0), TypePNG, true},
		{"webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 "), TypeWebP, true},
		{"heic", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00"), "", false},
		{"gif", []byte("GIF89a\x01\x00"), "", false},
		{"text named .jpg", []byte("hello, this is not a photo"), "", false},
		{"riff but avi", []byte("RIFF\x10\x00\x00\x00AVI LIST"), "", false},
		{"empty", nil, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Sniff(c.b)
			if got != c.want || ok != c.ok {
				t.Fatalf("Sniff = %q,%v; want %q,%v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestStripMetadata_JPEG_RemovesEXIFKeepsPixels(t *testing.T) {
	in := JPEGWithEXIF(t)
	out, err := StripMetadata(TypeJPEG, in)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if bytes.Contains(out, []byte(gpsMarker)) {
		t.Fatal("EXIF/COM survived the strip")
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("stripped JPEG no longer decodes: %v", err)
	}
	if typ, ok := Sniff(out); !ok || typ != TypeJPEG {
		t.Fatal("stripped JPEG no longer sniffs as JPEG")
	}
}

func TestStripMetadata_PNG_RemovesTextAndEXIF(t *testing.T) {
	out, err := StripMetadata(TypePNG, PNGWithText(t))
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if bytes.Contains(out, []byte(gpsMarker)) {
		t.Fatal("tEXt/eXIf survived the strip")
	}
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("stripped PNG no longer decodes: %v", err)
	}
}

func TestStripMetadata_WebP_RemovesChunksAndFlags(t *testing.T) {
	out, err := StripMetadata(TypeWebP, WebPWithEXIF())
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if bytes.Contains(out, []byte(gpsMarker)) {
		t.Fatal("EXIF/XMP survived the strip")
	}
	if got := int(binary.LittleEndian.Uint32(out[4:8])); got != len(out)-8 {
		t.Fatalf("RIFF size %d, file body %d", got, len(out)-8)
	}
	flags := out[12+8]
	if flags&(0x08|0x04) != 0 || flags&0x10 == 0 {
		t.Fatalf("VP8X flags %08b: EXIF/XMP must be cleared, alpha kept", flags)
	}
	if !bytes.Contains(out, []byte("VP8L")) {
		t.Fatal("image chunk dropped")
	}
}

func TestStripMetadata_TruncatedIsRefused(t *testing.T) {
	jp := JPEGWithEXIF(t)
	pn := PNGWithText(t)
	wp := WebPWithEXIF()
	for _, c := range []struct {
		name, typ string
		b         []byte
	}{
		{"jpeg header only", TypeJPEG, jp[:10]},
		{"jpeg no scan", TypeJPEG, []byte{0xff, 0xd8, 0xff, 0xd9}},
		{"png cut in a chunk", TypePNG, pn[:40]},
		{"png without IEND", TypePNG, pn[:len(pn)-12]},
		{"webp riff size lies", TypeWebP, wp[:len(wp)-6]},
		{"unknown type", "image/gif", jp},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := StripMetadata(c.typ, c.b); !errors.Is(err, ErrUnreadable) {
				t.Fatalf("err = %v, want ErrUnreadable", err)
			}
		})
	}
}
