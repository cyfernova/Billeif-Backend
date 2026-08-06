//go:build cgo && voice_libopus

package audio

/*
#cgo pkg-config: opus
#include <opus.h>
*/
import "C"

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

type LibopusDecoder struct {
	mu    sync.Mutex
	state *C.OpusDecoder
}

type LibopusEncoder struct {
	mu    sync.Mutex
	state *C.OpusEncoder
}

func VerifyLibopus() error {
	if version := C.opus_get_version_string(); version == nil || C.GoString(version) == "" {
		return ErrLibopusUnavailable
	}

	decoder, err := NewLibopusDecoder()
	if err != nil {
		return err
	}
	defer decoder.Close()
	encoder, err := NewLibopusEncoder()
	if err != nil {
		return err
	}
	defer encoder.Close()

	var pcm [SamplesPerFrame]int16
	var packet [MaxOpusPacketBytes]byte
	encodedBytes, err := encoder.Encode(pcm[:], packet[:])
	if err != nil {
		return err
	}
	samples, err := decoder.Decode(packet[:encodedBytes], pcm[:])
	if err != nil {
		return err
	}
	if samples != SamplesPerFrame {
		return ErrUnexpectedDecodedSamples
	}
	return nil
}

func NewLibopusDecoder() (*LibopusDecoder, error) {
	var result C.int
	state := C.opus_decoder_create(C.opus_int32(PCMRateHz), 1, &result)
	if state == nil || result != C.OPUS_OK {
		return nil, libopusError(ErrLibopusUnavailable, "create decoder", result)
	}
	decoder := &LibopusDecoder{state: state}
	runtime.SetFinalizer(decoder, finalizeLibopusDecoder)
	return decoder, nil
}

func (decoder *LibopusDecoder) Decode(payload []byte, pcm []int16) (int, error) {
	decoder.mu.Lock()
	defer decoder.mu.Unlock()
	if decoder.state == nil {
		return 0, ErrCodecClosed
	}
	if len(pcm) < SamplesPerFrame {
		return 0, ErrDestinationTooSmall
	}
	if len(payload) == 0 {
		clear(pcm[:SamplesPerFrame])
		return 0, ErrMalformedOpus
	}
	if len(payload) > MaxOpusPacketBytes {
		clear(pcm[:SamplesPerFrame])
		return 0, ErrOpusPacketTooLarge
	}

	clear(pcm[:SamplesPerFrame])
	payloadPointer := (*C.uchar)(unsafe.Pointer(&payload[0]))
	packetSamples := C.opus_packet_get_nb_samples(
		payloadPointer,
		C.opus_int32(len(payload)),
		C.opus_int32(PCMRateHz),
	)
	if packetSamples < 0 {
		return 0, libopusError(ErrMalformedOpus, "inspect packet", C.int(packetSamples))
	}
	if int(packetSamples) != SamplesPerFrame {
		return 0, ErrUnexpectedOpusDuration
	}

	decodedSamples := C.opus_decode(
		decoder.state,
		payloadPointer,
		C.opus_int32(len(payload)),
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(SamplesPerFrame),
		0,
	)
	runtime.KeepAlive(payload)
	runtime.KeepAlive(pcm)
	if decodedSamples < 0 {
		clear(pcm[:SamplesPerFrame])
		return 0, libopusError(ErrMalformedOpus, "decode packet", decodedSamples)
	}
	if int(decodedSamples) != SamplesPerFrame {
		clear(pcm[:SamplesPerFrame])
		return 0, ErrUnexpectedDecodedSamples
	}
	return int(decodedSamples), nil
}

func (decoder *LibopusDecoder) Close() error {
	decoder.mu.Lock()
	defer decoder.mu.Unlock()
	if decoder.state == nil {
		return nil
	}
	C.opus_decoder_destroy(decoder.state)
	decoder.state = nil
	runtime.SetFinalizer(decoder, nil)
	return nil
}

func NewLibopusEncoder() (*LibopusEncoder, error) {
	var result C.int
	state := C.opus_encoder_create(C.opus_int32(PCMRateHz), 1, C.OPUS_APPLICATION_VOIP, &result)
	if state == nil || result != C.OPUS_OK {
		return nil, libopusError(ErrLibopusUnavailable, "create encoder", result)
	}
	encoder := &LibopusEncoder{state: state}
	runtime.SetFinalizer(encoder, finalizeLibopusEncoder)
	return encoder, nil
}

func (encoder *LibopusEncoder) Encode(pcm []int16, destination []byte) (int, error) {
	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	if encoder.state == nil {
		return 0, ErrCodecClosed
	}
	if len(pcm) != SamplesPerFrame {
		return 0, ErrInvalidPCMFrame
	}
	if len(destination) < MaxOpusPacketBytes {
		return 0, ErrDestinationTooSmall
	}

	clear(destination[:MaxOpusPacketBytes])
	encodedBytes := C.opus_encode(
		encoder.state,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(SamplesPerFrame),
		(*C.uchar)(unsafe.Pointer(&destination[0])),
		C.opus_int32(MaxOpusPacketBytes),
	)
	runtime.KeepAlive(pcm)
	runtime.KeepAlive(destination)
	if encodedBytes < 0 {
		clear(destination[:MaxOpusPacketBytes])
		return 0, libopusError(ErrLibopusFailure, "encode frame", C.int(encodedBytes))
	}
	if encodedBytes == 0 || int(encodedBytes) > MaxOpusPacketBytes {
		clear(destination[:MaxOpusPacketBytes])
		return 0, ErrInvalidEncodedBytes
	}
	return int(encodedBytes), nil
}

func (encoder *LibopusEncoder) Close() error {
	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	if encoder.state == nil {
		return nil
	}
	C.opus_encoder_destroy(encoder.state)
	encoder.state = nil
	runtime.SetFinalizer(encoder, nil)
	return nil
}

func finalizeLibopusDecoder(decoder *LibopusDecoder) {
	_ = decoder.Close()
}

func finalizeLibopusEncoder(encoder *LibopusEncoder) {
	_ = encoder.Close()
}

func libopusError(kind error, operation string, code C.int) error {
	detail := C.opus_strerror(code)
	if detail == nil {
		return fmt.Errorf("%w: %s (code %d)", kind, operation, int(code))
	}
	return fmt.Errorf("%w: %s: %s", kind, operation, C.GoString(detail))
}

var (
	_ Decoder = (*LibopusDecoder)(nil)
	_ Encoder = (*LibopusEncoder)(nil)
)
