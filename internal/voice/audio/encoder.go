package audio

import "errors"

var (
	ErrNilEncoder          = errors.New("audio: nil encoder")
	ErrInvalidPCMFrame     = errors.New("audio: PCM frame is not 20 milliseconds at 16 kHz mono")
	ErrInvalidEncodedBytes = errors.New("audio: codec returned an invalid Opus packet size")
)

// Encoder converts one 20 millisecond PCM frame into caller-owned Opus storage.
type Encoder interface {
	Encode(pcm []int16, dst []byte) (bytes int, err error)
}

// FrameEncoder owns the reusable Opus packet buffer for one session's encoder
// worker. The returned packet remains valid only until the next Encode or Reset
// call.
type FrameEncoder struct {
	encoder Encoder
	packet  [MaxOpusPacketBytes]byte
}

func NewFrameEncoder(encoder Encoder) (*FrameEncoder, error) {
	if encoder == nil {
		return nil, ErrNilEncoder
	}
	return &FrameEncoder{encoder: encoder}, nil
}

func (encoder *FrameEncoder) Encode(pcm []int16) ([]byte, error) {
	if len(pcm) != SamplesPerFrame {
		return nil, ErrInvalidPCMFrame
	}

	clear(encoder.packet[:])
	encodedBytes, err := encoder.encoder.Encode(pcm, encoder.packet[:])
	if err != nil {
		clear(encoder.packet[:])
		return nil, err
	}
	if encodedBytes <= 0 || encodedBytes > len(encoder.packet) {
		clear(encoder.packet[:])
		return nil, ErrInvalidEncodedBytes
	}
	return encoder.packet[:encodedBytes], nil
}

func (encoder *FrameEncoder) Reset() {
	clear(encoder.packet[:])
}
