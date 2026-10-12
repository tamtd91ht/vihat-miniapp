// Package photo holds the policy and the byte-level checks for photos attached to a
// "Tư vấn và báo giá" request (owner decision 12/10/2026, migrations/0005).
//
// It knows nothing about HTTP or SQL. Two jobs:
//
//  1. Sniff — the image type the BYTES say. The file name and the Content-Type a phone
//     sends are labels the client chose; a policy that trusts them accepts whatever the
//     client calls a JPEG.
//  2. StripMetadata — drop EXIF / XMP / text chunks before storing. A phone photo's EXIF
//     carries the GPS position where it was taken: that is a home address the specialist
//     never needed for a quote. The pixels are copied byte for byte, never re-encoded, so
//     the quality the citizen chose is the quality stored.
//
// COST OF STRIPPING, stated once: the EXIF orientation tag goes too, so a photo the phone
// saved sideways-with-a-rotate-flag is shown sideways. Accepted — a rotated photo is a
// nuisance; a stored GPS fix is personal data kept for no purpose.
package photo

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Policy (owner decision 12/10/2026, defaults chosen by the coordinating session). The SAME
// numbers are CHECK constraints in migrations/0005 — change both or the database refuses.
const (
	// MaxPerRequest — photos one request may carry.
	MaxPerRequest = 3
	// MaxBytes — one photo, as uploaded (5 MB).
	MaxBytes = 5 << 20
)

const (
	TypeJPEG = "image/jpeg"
	TypePNG  = "image/png"
	TypeWebP = "image/webp"
)

// ErrUnreadable — the bytes start like an image of the sniffed type but its structure
// cannot be walked (truncated, corrupt, or a polyglot). Refused rather than stored as is:
// metadata that cannot be found cannot be removed.
var ErrUnreadable = errors.New("photo: image structure unreadable")

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// Sniff returns the type the first bytes say, or false for anything else (HEIC, GIF,
// video, text). The same three magic numbers the Mini App checks before it sends.
func Sniff(b []byte) (string, bool) {
	switch {
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return TypeJPEG, true
	case bytes.HasPrefix(b, pngSignature):
		return TypePNG, true
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return TypeWebP, true
	}
	return "", false
}

// StripMetadata returns a copy of b without its metadata. contentType must be what Sniff
// returned for b.
func StripMetadata(contentType string, b []byte) ([]byte, error) {
	switch contentType {
	case TypeJPEG:
		return stripJPEG(b)
	case TypePNG:
		return stripPNG(b)
	case TypeWebP:
		return stripWebP(b)
	}
	return nil, ErrUnreadable
}

// stripJPEG walks the marker segments up to Start Of Scan and drops APP1 (EXIF, XMP),
// APP13 (Photoshop / IPTC) and COM. Everything from SOS on is entropy-coded image data and
// is copied verbatim. APP0 (JFIF), APP2 (ICC colour profile) and APP14 (Adobe) are kept:
// without them colours or decoding change.
func stripJPEG(b []byte) ([]byte, error) {
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		return nil, ErrUnreadable
	}
	out := make([]byte, 0, len(b))
	out = append(out, 0xff, 0xd8)
	i := 2
	for {
		if i+4 > len(b) || b[i] != 0xff {
			return nil, ErrUnreadable
		}
		marker := b[i+1]
		if marker == 0xff { // fill byte before a marker
			i++
			continue
		}
		if marker == 0xd9 { // EOI before any scan: no image at all
			return nil, ErrUnreadable
		}
		// Standalone markers (RSTn, TEM) carry no length and cannot appear before SOS in a
		// valid file.
		if (marker >= 0xd0 && marker <= 0xd7) || marker == 0x01 {
			return nil, ErrUnreadable
		}
		n := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if n < 2 || i+2+n > len(b) {
			return nil, ErrUnreadable
		}
		seg := b[i : i+2+n]
		if marker == 0xda { // SOS: the rest is image data
			out = append(out, b[i:]...)
			return out, nil
		}
		if marker != 0xe1 && marker != 0xed && marker != 0xfe {
			out = append(out, seg...)
		}
		i += 2 + n
	}
}

// pngDropped — ancillary chunks that carry metadata, never pixels: EXIF, the three text
// chunk kinds (where XMP also lives), and the last-modified time.
var pngDropped = map[string]bool{"eXIf": true, "tEXt": true, "zTXt": true, "iTXt": true, "tIME": true}

// stripPNG walks length-type-data-crc chunks up to IEND and drops pngDropped. Kept chunks
// are copied with their original CRC (their bytes do not change).
func stripPNG(b []byte) ([]byte, error) {
	if !bytes.HasPrefix(b, pngSignature) {
		return nil, ErrUnreadable
	}
	out := make([]byte, 0, len(b))
	out = append(out, pngSignature...)
	i := len(pngSignature)
	for {
		if i+12 > len(b) {
			return nil, ErrUnreadable
		}
		n := binary.BigEndian.Uint32(b[i : i+4])
		if n > uint32(len(b)) || i+12+int(n) > len(b) {
			return nil, ErrUnreadable
		}
		typ := string(b[i+4 : i+8])
		end := i + 12 + int(n)
		if !pngDropped[typ] {
			out = append(out, b[i:end]...)
		}
		if typ == "IEND" {
			return out, nil
		}
		i = end
	}
}

// stripWebP walks the RIFF chunks and drops "EXIF" and "XMP ", clearing the matching flag
// bits in VP8X so a decoder is not told to look for a chunk that is gone. The RIFF size is
// rewritten for the shorter file.
func stripWebP(b []byte) ([]byte, error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return nil, ErrUnreadable
	}
	riff := int(binary.LittleEndian.Uint32(b[4:8]))
	if riff < 4 || 8+riff > len(b) {
		return nil, ErrUnreadable
	}
	body := b[12 : 8+riff]

	out := make([]byte, 12, len(b))
	copy(out, b[:12])
	vp8x := -1
	for i := 0; i < len(body); {
		if i+8 > len(body) {
			return nil, ErrUnreadable
		}
		fourcc := string(body[i : i+4])
		n := int(binary.LittleEndian.Uint32(body[i+4 : i+8]))
		padded := n + n%2
		if n < 0 || i+8+padded > len(body) {
			// A final odd-sized chunk without its pad byte is common enough to accept.
			if i+8+n == len(body) {
				padded = n
			} else {
				return nil, ErrUnreadable
			}
		}
		chunk := body[i : i+8+padded]
		switch fourcc {
		case "EXIF", "XMP ":
			// dropped
		case "VP8X":
			if n < 1 {
				return nil, ErrUnreadable
			}
			vp8x = len(out)
			out = append(out, chunk...)
		default:
			out = append(out, chunk...)
		}
		i += 8 + padded
	}
	if vp8x >= 0 {
		// Flags byte: bit 3 = EXIF present, bit 2 = XMP present (WebP container spec).
		out[vp8x+8] &^= 0x08 | 0x04
	}
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out, nil
}
