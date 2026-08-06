package audio

import "errors"

const (
	PCMRateHz          = 16_000
	RTPClockRateHz     = 48_000
	FrameDurationMS    = 20
	SamplesPerFrame    = PCMRateHz * FrameDurationMS / 1_000
	PCMBytesPerFrame   = SamplesPerFrame * 2
	RTPTimePerFrame    = RTPClockRateHz * FrameDurationMS / 1_000
	MaxOpusPacketBytes = 1_276
)

var (
	ErrNilDecoder               = errors.New("audio: nil decoder")
	ErrUnexpectedDecodedSamples = errors.New("audio: decoded frame is not 20 milliseconds at 16 kHz mono")
)

// Decoder converts one Opus packet into caller-owned PCM storage.
type Decoder interface {
	Decode(payload []byte, pcm []int16) (samples int, err error)
}

// FrameDecoder owns the reusable PCM buffer for one session's decoder worker.
// The returned PCM view remains valid only until the next Decode or Reset call.
type FrameDecoder struct {
	decoder Decoder
	pcm     [SamplesPerFrame]int16
}

func NewFrameDecoder(decoder Decoder) (*FrameDecoder, error) {
	if decoder == nil {
		return nil, ErrNilDecoder
	}
	return &FrameDecoder{decoder: decoder}, nil
}

func (decoder *FrameDecoder) Decode(payload []byte) ([]int16, error) {
	clear(decoder.pcm[:])
	samples, err := decoder.decoder.Decode(payload, decoder.pcm[:])
	if err != nil {
		clear(decoder.pcm[:])
		return nil, err
	}
	if samples != SamplesPerFrame {
		clear(decoder.pcm[:])
		return nil, ErrUnexpectedDecodedSamples
	}
	return decoder.pcm[:samples], nil
}

func (decoder *FrameDecoder) Reset() {
	clear(decoder.pcm[:])
}
