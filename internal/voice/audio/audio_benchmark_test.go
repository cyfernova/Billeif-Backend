package audio

import "testing"

func BenchmarkFrameDecoder(b *testing.B) {
	decoder, err := NewFrameDecoder(allocationDecoder{})
	if err != nil {
		b.Fatal(err)
	}
	payload := []byte{0x01}
	b.ReportAllocs()
	b.SetBytes(PCMBytesPerFrame)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := decoder.Decode(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFrameEncoder(b *testing.B) {
	encoder, err := NewFrameEncoder(allocationEncoder{})
	if err != nil {
		b.Fatal(err)
	}
	var pcm [SamplesPerFrame]int16
	b.ReportAllocs()
	b.SetBytes(PCMBytesPerFrame)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := encoder.Encode(pcm[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPCM16LEToSamples(b *testing.B) {
	var source [PCMBytesPerFrame]byte
	var destination [SamplesPerFrame]int16
	b.ReportAllocs()
	b.SetBytes(PCMBytesPerFrame)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := PCM16LEToSamples(source[:], destination[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSamplesToPCM16LE(b *testing.B) {
	var source [SamplesPerFrame]int16
	var destination [PCMBytesPerFrame]byte
	b.ReportAllocs()
	b.SetBytes(PCMBytesPerFrame)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := SamplesToPCM16LE(source[:], destination[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSampleRing(b *testing.B) {
	ring, err := NewSampleRing(SamplesPerFrame)
	if err != nil {
		b.Fatal(err)
	}
	var source [SamplesPerFrame]int16
	var destination [SamplesPerFrame]int16
	b.ReportAllocs()
	b.SetBytes(PCMBytesPerFrame)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		ring.WriteLatest(source[:])
		ring.CopyLatest(destination[:])
		ring.Reset()
	}
}

func BenchmarkGenerationPCMQueue(b *testing.B) {
	queues := NewGenerationQueues(nil)
	if err := queues.Activate(1); err != nil {
		b.Fatal(err)
	}
	var frame [SamplesPerFrame]int16
	b.ReportAllocs()
	b.SetBytes(PCMBytesPerFrame)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := queues.PushPCM(1, frame[:]); err != nil {
			b.Fatal(err)
		}
		if err := queues.PopPCMFrame(1, frame[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGenerationOpusQueue(b *testing.B) {
	queues := NewGenerationQueues(nil)
	if err := queues.Activate(1); err != nil {
		b.Fatal(err)
	}
	payload := []byte{0x01, 0x02, 0x03}
	var destination [MaxOpusPacketBytes]byte
	b.ReportAllocs()
	b.SetBytes(PCMBytesPerFrame)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := queues.PushOpus(1, payload); err != nil {
			b.Fatal(err)
		}
		if _, err := queues.PopOpus(1, destination[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRTPClock(b *testing.B) {
	clock := NewRTPClock(1)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		timestamp, err := clock.Prepare()
		if err != nil {
			b.Fatal(err)
		}
		if err := clock.Commit(timestamp); err != nil {
			b.Fatal(err)
		}
	}
}
