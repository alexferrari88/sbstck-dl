package lib

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
)

// mediaDuration returns the playing time in seconds of the media file at path,
// read from the file itself. ext selects the parser: MP3, or the MP4 container
// (.mp4, .m4a, .m4v, .mov). ok is false if the duration can't be determined.
func mediaDuration(path, ext string) (seconds float64, ok bool) {
	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer file.Close()

	switch ext {
	case ".mp3":
		seconds, err = mp3Duration(file)
	case ".mp4", ".m4a", ".m4v", ".mov":
		seconds, err = mp4Duration(file)
	default:
		return 0, false
	}
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return seconds, true
}

var errUnknownMediaFormat = errors.New("unrecognized media format")

// mp4Duration reads the duration from the movie header (moov/mvhd) box.
func mp4Duration(r io.ReadSeeker) (float64, error) {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	moovStart, moovEnd, err := findMP4Box(r, 0, end, "moov")
	if err != nil {
		return 0, err
	}
	mvhdStart, _, err := findMP4Box(r, moovStart, moovEnd, "mvhd")
	if err != nil {
		return 0, err
	}

	if _, err := r.Seek(mvhdStart, io.SeekStart); err != nil {
		return 0, err
	}
	var version [4]byte // version and flags
	if _, err := io.ReadFull(r, version[:]); err != nil {
		return 0, err
	}

	var timescale uint32
	var duration uint64
	if version[0] == 1 {
		var v1 struct {
			Created, Modified uint64
			Timescale         uint32
			Duration          uint64
		}
		if err := binary.Read(r, binary.BigEndian, &v1); err != nil {
			return 0, err
		}
		timescale, duration = v1.Timescale, v1.Duration
	} else {
		var v0 struct {
			Created, Modified uint32
			Timescale         uint32
			Duration          uint32
		}
		if err := binary.Read(r, binary.BigEndian, &v0); err != nil {
			return 0, err
		}
		timescale, duration = v0.Timescale, uint64(v0.Duration)
	}

	if timescale == 0 {
		return 0, errUnknownMediaFormat
	}
	return float64(duration) / float64(timescale), nil
}

// findMP4Box scans the boxes between start and end for one of the given type
// and returns the bounds of its payload.
func findMP4Box(r io.ReadSeeker, start, end int64, boxType string) (payloadStart, payloadEnd int64, err error) {
	pos := start
	for pos+8 <= end {
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return 0, 0, err
		}
		var header [8]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return 0, 0, err
		}

		size := int64(binary.BigEndian.Uint32(header[:4]))
		headerLen := int64(8)
		switch size {
		case 0: // box extends to the end
			size = end - pos
		case 1: // 64-bit size follows the type
			var large uint64
			if err := binary.Read(r, binary.BigEndian, &large); err != nil {
				return 0, 0, err
			}
			size = int64(large)
			headerLen = 16
		}
		if size < headerLen || pos+size > end {
			return 0, 0, errUnknownMediaFormat
		}

		if string(header[4:]) == boxType {
			return pos + headerLen, pos + size, nil
		}
		pos += size
	}
	return 0, 0, errUnknownMediaFormat
}

// MPEG audio Layer III tables, indexed by MPEG version: 0 = MPEG 1, 1 = MPEG 2 and 2.5.
var (
	mp3Bitrates = [2][16]int{
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	}
	mp3SamplesPerFrame = [2]int{1152, 576}
)

// mp3Frame holds the fields of an MPEG audio frame header needed to compute duration.
type mp3Frame struct {
	mpeg1      bool
	bitrate    int // bits per second
	sampleRate int
	mono       bool
	length     int // frame length in bytes
}

// parseMP3Frame parses a Layer III frame header.
func parseMP3Frame(h []byte) (mp3Frame, bool) {
	if len(h) < 4 || h[0] != 0xFF || h[1]&0xE0 != 0xE0 {
		return mp3Frame{}, false
	}
	version := (h[1] >> 3) & 3 // 3 = MPEG 1, 2 = MPEG 2, 0 = MPEG 2.5
	layer := (h[1] >> 1) & 3   // 1 = Layer III
	bitrateIndex := h[2] >> 4
	rateIndex := (h[2] >> 2) & 3
	if version == 1 || layer != 1 || bitrateIndex == 0 || bitrateIndex == 15 || rateIndex == 3 {
		return mp3Frame{}, false
	}

	sampleRate := [3]int{44100, 48000, 32000}[rateIndex]
	table := 1
	switch version {
	case 3:
		table = 0
	case 2:
		sampleRate /= 2
	case 0:
		sampleRate /= 4
	}

	f := mp3Frame{
		mpeg1:      version == 3,
		bitrate:    mp3Bitrates[table][bitrateIndex] * 1000,
		sampleRate: sampleRate,
		mono:       h[3]>>6 == 3,
	}
	padding := int((h[2] >> 1) & 1)
	f.length = mp3SamplesPerFrame[table]/8*f.bitrate/f.sampleRate + padding
	return f, true
}

// samplesPerFrame returns the number of audio samples in each frame.
func (f mp3Frame) samplesPerFrame() int {
	if f.mpeg1 {
		return mp3SamplesPerFrame[0]
	}
	return mp3SamplesPerFrame[1]
}

// mp3Duration computes the duration of an MP3 file. It uses the frame count
// from a Xing/Info or VBRI header when present, and otherwise assumes a
// constant bitrate.
func mp3Duration(r io.ReadSeeker) (float64, error) {
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	// Skip an ID3v2 tag at the start
	var start int64
	var id3 [10]byte
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := io.ReadFull(r, id3[:]); err == nil && string(id3[:3]) == "ID3" {
		tagSize := int64(id3[6]&0x7F)<<21 | int64(id3[7]&0x7F)<<14 | int64(id3[8]&0x7F)<<7 | int64(id3[9]&0x7F)
		start = 10 + tagSize
		if id3[5]&0x10 != 0 { // footer present
			start += 10
		}
	}

	// Find the first frame, confirmed by a second frame right after it
	const searchLimit = 64 << 10
	buf := make([]byte, searchLimit)
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return 0, err
	}
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, err
	}
	buf = buf[:n]

	for i := 0; i+4 <= len(buf); i++ {
		frame, ok := parseMP3Frame(buf[i:])
		if !ok {
			continue
		}
		next := i + frame.length
		if next+4 <= len(buf) {
			if _, ok := parseMP3Frame(buf[next:]); !ok {
				continue
			}
		}

		if frames, ok := mp3FrameCount(buf[i:], frame); ok {
			return float64(frames) * float64(frame.samplesPerFrame()) / float64(frame.sampleRate), nil
		}

		audioBytes := size - (start + int64(i))
		if hasID3v1Tag(r, size) {
			audioBytes -= 128
		}
		return float64(audioBytes) * 8 / float64(frame.bitrate), nil
	}
	return 0, errUnknownMediaFormat
}

// mp3FrameCount reads the total frame count from a Xing/Info or VBRI header
// stored in the first frame.
func mp3FrameCount(frameData []byte, f mp3Frame) (uint32, bool) {
	// Xing/Info follows the side information
	sideInfo := 32
	switch {
	case f.mpeg1 && f.mono:
		sideInfo = 17
	case !f.mpeg1 && !f.mono:
		sideInfo = 17
	case !f.mpeg1 && f.mono:
		sideInfo = 9
	}
	if off := 4 + sideInfo; off+12 <= len(frameData) {
		tag := frameData[off : off+4]
		if bytes.Equal(tag, []byte("Xing")) || bytes.Equal(tag, []byte("Info")) {
			flags := binary.BigEndian.Uint32(frameData[off+4:])
			if flags&1 != 0 {
				return binary.BigEndian.Uint32(frameData[off+8:]), true
			}
			return 0, false
		}
	}

	// VBRI sits at a fixed offset of 32 bytes after the header
	if off := 4 + 32; off+18 <= len(frameData) && bytes.Equal(frameData[off:off+4], []byte("VBRI")) {
		return binary.BigEndian.Uint32(frameData[off+14:]), true
	}
	return 0, false
}

// hasID3v1Tag reports whether the file ends with a 128-byte ID3v1 tag.
func hasID3v1Tag(r io.ReadSeeker, size int64) bool {
	if size < 128 {
		return false
	}
	var tag [3]byte
	if _, err := r.Seek(size-128, io.SeekStart); err != nil {
		return false
	}
	if _, err := io.ReadFull(r, tag[:]); err != nil {
		return false
	}
	return string(tag[:]) == "TAG"
}
