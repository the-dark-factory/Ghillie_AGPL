package voice

// wav.go is the minimum WAV handling the offered-floor breath needs: read
// 16-bit PCM (what the breath bank holds and what the harness writes), scale,
// write 16-bit PCM mono. It refuses anything else rather than guessing — the
// two producers it deals with are known and fixed.

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// wavHeaderSize is the canonical RIFF/fmt/data header length; a WAV no bigger
// than this carries no audio.
const wavHeaderSize = 44

// readWAV reads a 16-bit PCM WAV into samples in [-1, 1]. Multi-channel audio
// is averaged to mono — the bank and the harness both write mono, so hitting
// that path at all is unexpected but not a guess.
func readWAV(path string) (samples []float64, rate int, err error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read wav %s: %w", path, err)
	}
	if len(body) < 12 || string(body[0:4]) != "RIFF" || string(body[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("read wav %s: not a RIFF/WAVE file", path)
	}

	var (
		channels, bits int
		data           []byte
	)
	for off := 12; off+8 <= len(body); {
		id := string(body[off : off+4])
		size := int(binary.LittleEndian.Uint32(body[off+4 : off+8]))
		off += 8
		if off+size > len(body) {
			return nil, 0, fmt.Errorf("read wav %s: chunk %q overruns the file", path, id)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, fmt.Errorf("read wav %s: fmt chunk is %d bytes", path, size)
			}
			format := int(binary.LittleEndian.Uint16(body[off : off+2]))
			channels = int(binary.LittleEndian.Uint16(body[off+2 : off+4]))
			rate = int(binary.LittleEndian.Uint32(body[off+4 : off+8]))
			bits = int(binary.LittleEndian.Uint16(body[off+14 : off+16]))
			if format != 1 || bits != 16 {
				return nil, 0, fmt.Errorf("read wav %s: format %d / %d-bit — this seam handles 16-bit PCM only (the bank and the harness both write it) and refuses to guess at anything else", path, format, bits)
			}
		case "data":
			data = body[off : off+size]
		}
		off += size + size%2 // chunks are word-aligned
	}
	if channels == 0 || data == nil {
		return nil, 0, fmt.Errorf("read wav %s: missing fmt or data chunk", path)
	}

	frames := len(data) / (2 * channels)
	samples = make([]float64, frames)
	for i := 0; i < frames; i++ {
		var acc float64
		for c := 0; c < channels; c++ {
			raw := int16(binary.LittleEndian.Uint16(data[2*(i*channels+c):]))
			acc += float64(raw) / 32768.0
		}
		samples[i] = acc / float64(channels)
	}
	return samples, rate, nil
}

// writeWAV writes samples in [-1, 1] as 16-bit PCM mono, clipping rather than
// wrapping anything that strayed outside the range.
func writeWAV(path string, samples []float64, rate int) error {
	data := make([]byte, wavHeaderSize+2*len(samples))
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(36+2*len(samples)))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(data[22:24], 1) // mono
	binary.LittleEndian.PutUint32(data[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(data[28:32], uint32(rate*2)) // byte rate
	binary.LittleEndian.PutUint16(data[32:34], 2)              // block align
	binary.LittleEndian.PutUint16(data[34:36], 16)             // bits
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(2*len(samples)))
	for i, s := range samples {
		s = math.Max(-1, math.Min(1, s))
		binary.LittleEndian.PutUint16(data[wavHeaderSize+2*i:], uint16(int16(math.Round(s*32767))))
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write wav %s: %w", path, err)
	}
	return nil
}

// rmsOf is the root-mean-square level of samples in [-1, 1].
func rmsOf(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var acc float64
	for _, s := range samples {
		acc += s * s
	}
	return math.Sqrt(acc / float64(len(samples)))
}

// peakOf is the absolute peak of samples in [-1, 1].
func peakOf(samples []float64) float64 {
	var p float64
	for _, s := range samples {
		if a := math.Abs(s); a > p {
			p = a
		}
	}
	return p
}
