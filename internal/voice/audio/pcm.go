package audio

import "errors"

var (
	ErrInvalidPCMBytes     = errors.New("audio: PCM16LE input must contain complete samples")
	ErrDestinationTooSmall = errors.New("audio: destination buffer is too small")
)

func PCM16LEToSamples(source []byte, destination []int16) (int, error) {
	if len(source)%2 != 0 {
		return 0, ErrInvalidPCMBytes
	}
	samples := len(source) / 2
	if len(destination) < samples {
		return 0, ErrDestinationTooSmall
	}
	for index := 0; index < samples; index++ {
		offset := index * 2
		destination[index] = int16(uint16(source[offset]) | uint16(source[offset+1])<<8)
	}
	return samples, nil
}

func SamplesToPCM16LE(source []int16, destination []byte) (int, error) {
	bytesRequired := len(source) * 2
	if len(destination) < bytesRequired {
		return 0, ErrDestinationTooSmall
	}
	for index, sample := range source {
		offset := index * 2
		value := uint16(sample)
		destination[offset] = byte(value)
		destination[offset+1] = byte(value >> 8)
	}
	return bytesRequired, nil
}
