//go:build !cgo || !voice_libopus

package audio

type LibopusDecoder struct{}

type LibopusEncoder struct{}

func VerifyLibopus() error {
	return ErrLibopusUnavailable
}

func NewLibopusDecoder() (*LibopusDecoder, error) {
	return nil, ErrLibopusUnavailable
}

func (decoder *LibopusDecoder) Decode([]byte, []int16) (int, error) {
	return 0, ErrLibopusUnavailable
}

func (decoder *LibopusDecoder) Close() error {
	return nil
}

func NewLibopusEncoder() (*LibopusEncoder, error) {
	return nil, ErrLibopusUnavailable
}

func (encoder *LibopusEncoder) Encode([]int16, []byte) (int, error) {
	return 0, ErrLibopusUnavailable
}

func (encoder *LibopusEncoder) Close() error {
	return nil
}

var (
	_ Decoder = (*LibopusDecoder)(nil)
	_ Encoder = (*LibopusEncoder)(nil)
)
