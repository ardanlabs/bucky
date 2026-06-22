// diarize performs basic speaker diarization by channel separation. Each
// channel of a multi-channel recording is treated as a distinct speaker
// (Speaker A on the left, Speaker B on the right, ...), transcribed
// independently, and the segments are merged back together in chronological
// order. This is the whisper.cpp --diarize style of diarization: it relies on
// speakers being recorded on separate channels, not on voice analysis.
//
// The bundled samples/stereo-speakers.wav has an English speaker on the left
// channel and a Spanish speaker on the right channel.
//
// Usage:
//
//	BUCKY_LIB=./lib BUCKY_TEST_MODEL=$HOME/models/ggml-tiny.bin \
//	    go run ./examples/diarize samples/stereo-speakers.wav
package main

import (
	"fmt"
	"log"
	"os"
	"slices"

	"github.com/ardanlabs/bucky/pkg/audio"
	"github.com/ardanlabs/bucky/pkg/whisper"
)

// segment is one transcribed span tagged with the channel it came from.
type segment struct {
	speaker int
	t0      int64 // milliseconds
	t1      int64 // milliseconds
	text    string
}

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s <audio-file>", os.Args[0])
	}
	audioPath := os.Args[1]

	libPath := os.Getenv("BUCKY_LIB")
	if libPath == "" {
		log.Fatal("BUCKY_LIB must point to the directory containing libwhisper")
	}
	modelPath := os.Getenv("BUCKY_TEST_MODEL")
	if modelPath == "" {
		log.Fatal("BUCKY_TEST_MODEL must point to a GGML whisper model")
	}

	if err := whisper.Load(libPath); err != nil {
		log.Fatalf("whisper.Load: %v", err)
	}
	if err := whisper.Init(libPath); err != nil {
		log.Fatalf("whisper.Init: %v", err)
	}

	ctx, err := whisper.InitFromFileWithParams(modelPath, whisper.ContextDefaultParams())
	if err != nil {
		log.Fatalf("InitFromFileWithParams: %v", err)
	}
	defer whisper.Free(ctx)

	channels, err := decodeChannels(audioPath)
	if err != nil {
		log.Fatalf("decode: %v", err)
	}
	fmt.Printf("decoded %d channel(s)\n\n", len(channels))

	var segs []segment
	for speaker, samples := range channels {
		chSegs, err := transcribe(ctx, speaker, samples)
		if err != nil {
			log.Fatalf("transcribe channel %d: %v", speaker, err)
		}
		segs = append(segs, chSegs...)
	}

	// Merge channels into a single chronological transcript.
	slices.SortStableFunc(segs, func(a, b segment) int {
		return int(a.t0 - b.t0)
	})

	for _, s := range segs {
		fmt.Printf("[%s -> %s] %s:%s\n",
			formatMs(s.t0), formatMs(s.t1), speakerLabel(s.speaker), s.text)
	}
}

// decodeChannels decodes the audio file into one 16 kHz mono stream per
// channel using the new audio.SplitChannels primitive.
func decodeChannels(path string) ([][]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	samples, rate, channels, err := audio.DecodeRaw(f)
	if err != nil {
		return nil, fmt.Errorf("audio.DecodeRaw: %w", err)
	}

	split := audio.SplitChannels(samples, channels)
	for i, ch := range split {
		split[i] = audio.ResampleLinear(ch, rate, audio.TargetSampleRate)
	}
	return split, nil
}

// transcribe runs whisper over a single channel and returns its segments
// tagged with the speaker index.
func transcribe(ctx whisper.Context, speaker int, samples []float32) ([]segment, error) {
	wparams := whisper.FullDefaultParams(whisper.SamplingGreedy)
	wparams.PrintProgress = 0
	wparams.PrintRealtime = 0
	wparams.PrintTimestamps = 0

	if err := whisper.Full(ctx, wparams, samples); err != nil {
		return nil, err
	}

	n := whisper.FullNSegments(ctx)
	segs := make([]segment, 0, n)
	for i := range n {
		segs = append(segs, segment{
			speaker: speaker,
			t0:      whisper.FullGetSegmentT0(ctx, i) * 10,
			t1:      whisper.FullGetSegmentT1(ctx, i) * 10,
			text:    whisper.FullGetSegmentText(ctx, i),
		})
	}
	return segs, nil
}

// speakerLabel maps a channel index to a label (0 -> A, 1 -> B, ...).
func speakerLabel(speaker int) string {
	return fmt.Sprintf("Speaker %c", 'A'+speaker)
}

func formatMs(ms int64) string {
	s := ms / 1000
	mm := s / 60
	ss := s % 60
	return fmt.Sprintf("%02d:%02d.%03d", mm, ss, ms%1000)
}
