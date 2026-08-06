package runtime

import (
	"context"
	"errors"
	"io"
	"sync"

	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/protocol"
)

var (
	ErrSTTDecoderRequired  = errors.New("voice runtime STT Opus decoder is required")
	ErrSTTActivityRequired = errors.New("voice runtime STT persistent activity is required")
)

// PersistentActivity owns a runtime-rooted context and activity lease for a
// peer or other session-scoped worker whose lifetime outlasts one invocation.
// Close is idempotent, cancels the worker context, and releases /ping busy
// state.
type PersistentActivity struct {
	once   sync.Once
	cancel context.CancelFunc
	lease  *ActivityLease
}

// AcquirePersistentActivity creates an activity context rooted in the runtime,
// not in the current HTTP invocation. Runtime shutdown cancels the context and
// waits for the returned closer to release its activity lease.
func (s *Server) AcquirePersistentActivity() (context.Context, io.Closer, error) {
	if s == nil || s.activities == nil || s.rootContext == nil {
		return nil, nil, ErrShuttingDown
	}
	lease, err := s.activities.acquire()
	if err != nil {
		return nil, nil, err
	}
	activityContext, cancel := context.WithCancel(s.rootContext)
	activity := &PersistentActivity{cancel: cancel, lease: lease}
	if err := activityContext.Err(); err != nil {
		_ = activity.Close()
		return nil, nil, ErrShuttingDown
	}
	return activityContext, activity, nil
}

// Close releases the persistent activity exactly once.
func (activity *PersistentActivity) Close() error {
	if activity == nil {
		return nil
	}
	var closeError error
	activity.once.Do(func() {
		if activity.cancel != nil {
			activity.cancel()
		}
		if activity.lease != nil {
			closeError = activity.lease.Close()
		}
	})
	return closeError
}

// STTSessionBindingConfig binds the final-only STT controller to one peer's
// persistent runtime lifetime and inbound Opus decoder.
type STTSessionBindingConfig struct {
	STT      STTSessionConfig
	Decoder  audio.Decoder
	Activity io.Closer
}

// STTSessionBinding is the production bridge from accepted WebRTC control and
// Opus frames to a final-only STTSession. The nested STT config is the only
// place a final transcript handler can be supplied.
type STTSessionBinding struct {
	mu sync.Mutex

	stt          *STTSession
	decoder      *audio.FrameDecoder
	decoderClose io.Closer
	activity     io.Closer
	pcm          [audio.PCMBytesPerFrame]byte
	closed       bool
	closeOnce    sync.Once
	closeError   error
}

func NewSTTSessionBinding(config STTSessionBindingConfig) (*STTSessionBinding, error) {
	if nilInterface(config.Decoder) {
		return nil, ErrSTTDecoderRequired
	}
	if nilInterface(config.Activity) {
		return nil, ErrSTTActivityRequired
	}
	frameDecoder, err := audio.NewFrameDecoder(config.Decoder)
	if err != nil {
		return nil, err
	}
	sttSession, err := NewSTTSession(config.STT)
	if err != nil {
		frameDecoder.Reset()
		var decoderError error
		if decoderCloser, ok := config.Decoder.(io.Closer); ok {
			decoderError = decoderCloser.Close()
		}
		activityError := config.Activity.Close()
		return nil, errors.Join(err, decoderError, activityError)
	}
	decoderCloser, _ := config.Decoder.(io.Closer)
	return &STTSessionBinding{
		stt:          sttSession,
		decoder:      frameDecoder,
		decoderClose: decoderCloser,
		activity:     config.Activity,
	}, nil
}

// HandleControl is directly compatible with webrtc.PeerConfig.HandleControl.
// All non-speech controls remain available to other session concerns and are
// intentionally ignored by this binding.
func (binding *STTSessionBinding) HandleControl(ctx context.Context, message protocol.ControlMessage) error {
	if ctx == nil {
		return ErrSTTSessionContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if binding == nil {
		return ErrSTTSessionClosed
	}
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.closed || binding.stt == nil {
		return ErrSTTSessionClosed
	}
	switch message.Type {
	case protocol.EventSpeechStarted:
		return binding.stt.SpeechStarted()
	case protocol.EventSpeechEnded:
		return binding.stt.SpeechEnded()
	default:
		return nil
	}
}

// HandleOpus serializes one WebRTC Opus packet through the fixed session
// decoder and PCM16LE buffer before handing it to STT.
func (binding *STTSessionBinding) HandleOpus(payload []byte) error {
	if binding == nil {
		return ErrSTTSessionClosed
	}
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.closed || binding.stt == nil || binding.decoder == nil {
		return ErrSTTSessionClosed
	}
	pcm, err := binding.decoder.Decode(payload)
	if err != nil {
		binding.decoder.Reset()
		clear(binding.pcm[:])
		return err
	}
	defer binding.decoder.Reset()
	defer clear(binding.pcm[:])
	bytesWritten, err := audio.SamplesToPCM16LE(pcm, binding.pcm[:])
	if err != nil {
		return err
	}
	return binding.handlePCM16Locked(binding.pcm[:bytesWritten])
}

// HandlePCM16 accepts an already-decoded frame at the same binding boundary.
// It exists for decoder workers that perform the Opus step before dispatch.
func (binding *STTSessionBinding) HandlePCM16(pcm []byte) error {
	if binding == nil {
		return ErrSTTSessionClosed
	}
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.closed || binding.stt == nil {
		return ErrSTTSessionClosed
	}
	return binding.handlePCM16Locked(pcm)
}

func (binding *STTSessionBinding) handlePCM16Locked(pcm []byte) error {
	err := binding.stt.PushPCM16(pcm)
	if errors.Is(err, ErrSTTSpeechAlreadyEnded) {
		// RTP and DataChannel delivery are independently ordered. A tail audio
		// packet observed after the accepted speech.ended control belongs to the
		// completed utterance and must not fail or contaminate the next pre-roll.
		return nil
	}
	return err
}

// Close retires provider/timer work, scrubs codec buffers, closes the codec,
// and finally releases the runtime activity lease exactly once.
func (binding *STTSessionBinding) Close() error {
	if binding == nil {
		return nil
	}
	binding.closeOnce.Do(func() {
		binding.mu.Lock()
		binding.closed = true
		sttSession := binding.stt
		binding.stt = nil
		decoder := binding.decoder
		binding.decoder = nil
		decoderCloser := binding.decoderClose
		binding.decoderClose = nil
		activity := binding.activity
		binding.activity = nil
		clear(binding.pcm[:])
		if decoder != nil {
			decoder.Reset()
		}
		binding.mu.Unlock()

		var sttError, decoderError, activityError error
		if sttSession != nil {
			sttError = sttSession.Close()
		}
		if decoderCloser != nil {
			decoderError = decoderCloser.Close()
		}
		if activity != nil {
			activityError = activity.Close()
		}
		binding.closeError = errors.Join(sttError, decoderError, activityError)
	})
	return binding.closeError
}

var _ io.Closer = (*STTSessionBinding)(nil)
