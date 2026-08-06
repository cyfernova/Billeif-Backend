//go:build cgo && voice_libopus && voice_libopus_test

package audio

/*
#cgo pkg-config: opus
#include <opus.h>

static opus_int32 billeif_test_encode_stereo_48k(
    const opus_int16 *pcm,
    unsigned char *packet,
    opus_int32 packet_capacity
) {
    int result = OPUS_OK;
    OpusEncoder *encoder = opus_encoder_create(48000, 2, OPUS_APPLICATION_VOIP, &result);
    if (encoder == NULL || result != OPUS_OK) {
        if (encoder != NULL) {
            opus_encoder_destroy(encoder);
        }
        return result == OPUS_OK ? OPUS_INTERNAL_ERROR : result;
    }
    opus_int32 encoded = opus_encode(encoder, pcm, 960, packet, packet_capacity);
    opus_encoder_destroy(encoder);
    return encoded;
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

const stereo48kSamplesPerFrame = 960 * 2

func encodeStereo48kFixture(pcm []int16, packet []byte) (int, error) {
	if len(pcm) != stereo48kSamplesPerFrame {
		return 0, ErrInvalidPCMFrame
	}
	if len(packet) < MaxOpusPacketBytes {
		return 0, ErrDestinationTooSmall
	}
	encodedBytes := C.billeif_test_encode_stereo_48k(
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		(*C.uchar)(unsafe.Pointer(&packet[0])),
		C.opus_int32(MaxOpusPacketBytes),
	)
	runtime.KeepAlive(pcm)
	runtime.KeepAlive(packet)
	if encodedBytes < 0 {
		return 0, libopusError(ErrLibopusFailure, "encode stereo test fixture", C.int(encodedBytes))
	}
	return int(encodedBytes), nil
}
