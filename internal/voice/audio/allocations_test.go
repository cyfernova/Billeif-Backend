package audio

import "testing"

type allocationDecoder struct{}

func (allocationDecoder) Decode(_ []byte, pcm []int16) (int, error) {
	pcm[0] = 1
	return SamplesPerFrame, nil
}

type allocationEncoder struct{}

func (allocationEncoder) Encode(_ []int16, destination []byte) (int, error) {
	destination[0] = 1
	return 1, nil
}

func TestWarmedPureAudioOperationsAllocateNoMemory(t *testing.T) {
	frameDecoder, err := NewFrameDecoder(allocationDecoder{})
	if err != nil {
		t.Fatalf("NewFrameDecoder() error = %v", err)
	}
	frameEncoder, err := NewFrameEncoder(allocationEncoder{})
	if err != nil {
		t.Fatalf("NewFrameEncoder() error = %v", err)
	}
	ring, err := NewSampleRing(SamplesPerFrame)
	if err != nil {
		t.Fatalf("NewSampleRing() error = %v", err)
	}
	queues := NewGenerationQueues(nil)
	if err := queues.Activate(1); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	clock := NewRTPClock(1)

	var pcm [SamplesPerFrame]int16
	var pcmBytes [PCMBytesPerFrame]byte
	var packet [MaxOpusPacketBytes]byte
	encoded := []byte{0x01}

	tests := []struct {
		name string
		run  func()
	}{
		{name: "frame decode", run: func() { _, _ = frameDecoder.Decode(encoded) }},
		{name: "frame encode", run: func() { _, _ = frameEncoder.Encode(pcm[:]) }},
		{name: "PCM bytes to samples", run: func() { _, _ = PCM16LEToSamples(pcmBytes[:], pcm[:]) }},
		{name: "samples to PCM bytes", run: func() { _, _ = SamplesToPCM16LE(pcm[:], pcmBytes[:]) }},
		{name: "ring write copy reset", run: func() {
			ring.WriteLatest(pcm[:])
			ring.CopyLatest(pcm[:])
			ring.Reset()
		}},
		{name: "PCM queue push pop", run: func() {
			_ = queues.PushPCM(1, pcm[:])
			_ = queues.PopPCMFrame(1, pcm[:])
		}},
		{name: "Opus queue push pop", run: func() {
			_ = queues.PushOpus(1, encoded)
			_, _ = queues.PopOpus(1, packet[:])
		}},
		{name: "RTP clock reserve commit", run: func() {
			timestamp, _ := clock.Prepare()
			_ = clock.Commit(timestamp)
		}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if allocations := testing.AllocsPerRun(1_000, test.run); allocations != 0 {
				t.Fatalf("allocations per warmed operation = %.2f, want 0", allocations)
			}
		})
	}
}
